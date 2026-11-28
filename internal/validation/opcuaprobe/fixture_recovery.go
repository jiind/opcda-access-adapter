package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jiind/opcda-access-adapter/internal/opcua"
)

func (c *client) validateFixtureRecovery(ctx context.Context, token opcua.ChannelSecurityToken, session opcua.NodeID, signals string) (err error) {
	target := opcua.ItemNodeID("Test/Float")
	before, err := c.readDataValue(token, session, target, opcua.AttributeValue, opcua.TimestampsBoth, 60)
	if err != nil {
		return err
	}
	original, ok := before.Value.Value.(float32)
	if before.Status.IsBad() || !ok || before.Value.Type != opcua.BuiltInFloat || before.SourceTimestamp.IsZero() {
		return fmt.Errorf("recovery target did not provide a usable timestamped Float")
	}

	subscription, err := c.createSubscription(token, session, 1, 0)
	if err != nil {
		return err
	}
	id := subscription.SubscriptionID
	defer func() {
		err = errors.Join(err, c.deleteSubscription(token, session, id))
		if err == nil {
			fmt.Println("OPCUA_REAL_DA_RECONNECT_PASS sameSession=true pendingDiscarded=true outageWriteRefused=true replay=false explicitResubscribe=true valuesLogged=false")
		}
	}()

	const first, second, resumed = 4250, 4251, 4252
	created, err := c.createMonitoredItems(token, session, id, opcua.TimestampsBoth,
		monitoredTarget{node: target, handle: first}, monitoredTarget{node: target, handle: second})
	if err != nil {
		return err
	}
	for _, item := range created {
		if item.StatusCode != opcua.StatusGood || item.MonitoredItemID == 0 {
			return fmt.Errorf("create pre-outage monitor: %s", item.StatusCode.Hex())
		}
	}
	if created[0].MonitoredItemID == created[1].MonitoredItemID {
		return fmt.Errorf("the pre-outage monitors shared an ID")
	}
	initial, more, err := c.recoveryNotification(ctx, token, session, id)
	if err != nil {
		return err
	}
	if !more || initial.Value.Status.IsBad() || initial.Value.Value.Value != original ||
		(initial.ClientHandle != first && initial.ClientHandle != second) {
		return fmt.Errorf("initial Publish did not leave one usable notification pending")
	}
	if err := signalFixtureController(signals, "subscribed"); err != nil {
		return err
	}
	if err := waitFixtureController(ctx, signals, "unavailable"); err != nil {
		return err
	}

	// A device Read also makes the runtime observe a broken COM connection.
	outage, err := c.readDataValue(token, session, target, opcua.AttributeValue, opcua.TimestampsBoth, 61)
	if err != nil {
		return err
	}
	if !outage.Status.IsBad() || !outage.Value.IsNull() || !outage.SourceTimestamp.IsZero() {
		return fmt.Errorf("outage Read carried a usable value or source timestamp")
	}
	remaining := map[uint32]bool{first: true, second: true}
	for len(remaining) > 0 {
		notification, more, err := c.recoveryNotification(ctx, token, session, id)
		if err != nil {
			return err
		}
		value := notification.Value
		if !remaining[notification.ClientHandle] || value.Status != opcua.StatusBadNotConnected ||
			!value.Value.IsNull() || !value.SourceTimestamp.IsZero() || value.ServerTimestamp.IsZero() {
			return fmt.Errorf("outage Publish returned an unexpected handle, status, or timestamp")
		}
		delete(remaining, notification.ClientHandle)
		if more != (len(remaining) != 0) {
			return fmt.Errorf("outage Publish reported an incorrect remaining notification count")
		}
	}
	attempted := float32(17.25)
	if attempted == original {
		attempted = 18.25
	}
	status, err := c.writeVariant(token, session, target, opcua.Variant{Type: opcua.BuiltInFloat, Value: attempted})
	if err != nil {
		return err
	}
	if status != opcua.StatusBadNotConnected {
		return fmt.Errorf("outage Write returned %s", status.Hex())
	}
	if err := signalFixtureController(signals, "invalidated"); err != nil {
		return err
	}
	if err := waitFixtureController(ctx, signals, "available"); err != nil {
		return err
	}

	read, err := c.waitRecoveredRead(ctx, token, session, target)
	if err != nil {
		return err
	}
	recovered, ok := read.Value.Value.(float32)
	if !ok || read.Value.Type != opcua.BuiltInFloat || read.SourceTimestamp.IsZero() || recovered == attempted {
		return fmt.Errorf("recovery Read did not provide live data or the refused Write appeared")
	}
	if err := c.deleteRecoveryMonitor(token, session, id, created[0].MonitoredItemID); err != nil {
		return err
	}
	for range 2 {
		response, err := c.publish(token, session)
		if err != nil {
			return err
		}
		if response.SubscriptionID != id || response.NotificationMessage.HasData || response.MoreNotifications {
			return fmt.Errorf("Publish resumed without explicit monitor creation")
		}
	}

	added, err := c.createMonitoredItems(token, session, id, opcua.TimestampsBoth, monitoredTarget{node: target, handle: resumed})
	if err != nil {
		return err
	}
	newID := added[0].MonitoredItemID
	if added[0].StatusCode != opcua.StatusGood || newID == 0 || newID == created[0].MonitoredItemID || newID == created[1].MonitoredItemID {
		return fmt.Errorf("explicit monitor creation returned %s or an invalid ID", added[0].StatusCode.Hex())
	}
	if _, err := c.awaitMonitoredFloat(ctx, token, session, id, recovered, []uint32{second, resumed}); err != nil {
		return fmt.Errorf("explicit recovery did not publish to the surviving and new monitors: %w", err)
	}
	return nil
}

func (c *client) recoveryNotification(ctx context.Context, token opcua.ChannelSecurityToken, session opcua.NodeID, id uint32) (opcua.MonitoredItemNotification, bool, error) {
	for ctx.Err() == nil {
		response, err := c.publish(token, session)
		if err != nil {
			return opcua.MonitoredItemNotification{}, false, err
		}
		notifications := response.NotificationMessage.Notifications
		if response.SubscriptionID != id || len(notifications) > 1 || (response.MoreNotifications && len(notifications) == 0) {
			return opcua.MonitoredItemNotification{}, false, fmt.Errorf("Publish did not honor the recovery subscription or its notification limit")
		}
		if len(notifications) == 1 {
			return notifications[0], response.MoreNotifications, nil
		}
	}
	return opcua.MonitoredItemNotification{}, false, ctx.Err()
}

func (c *client) waitRecoveredRead(ctx context.Context, token opcua.ChannelSecurityToken, session, target opcua.NodeID) (opcua.DataValue, error) {
	ctx, cancel := context.WithTimeout(ctx, sourceConnectBound)
	defer cancel()
	for {
		value, err := c.readDataValue(token, session, target, opcua.AttributeValue, opcua.TimestampsBoth, 62)
		if err != nil || !value.Status.IsBad() {
			return value, err
		}
		if value.Status != opcua.StatusBadNotConnected || !value.Value.IsNull() || !value.SourceTimestamp.IsZero() {
			return opcua.DataValue{}, fmt.Errorf("Read during recovery returned %s or carried invalid data", value.Status.Hex())
		}
		select {
		case <-ctx.Done():
			return opcua.DataValue{}, fmt.Errorf("source Read did not recover: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (c *client) deleteRecoveryMonitor(token opcua.ChannelSecurityToken, session opcua.NodeID, subscription, monitor uint32) error {
	encoder, err := opcua.NewEncoder(c.limits)
	if err != nil {
		return err
	}
	encoder.WriteDeleteMonitoredItemsRequest(opcua.DeleteMonitoredItemsRequest{
		Header: requestHeader(session, 63), SubscriptionID: subscription, MonitoredItemIDs: []uint32{monitor},
	})
	body, err := encoder.Bytes()
	if err != nil {
		return err
	}
	identifier, decoder, err := c.call(token, 63, body)
	if err != nil {
		return fmt.Errorf("delete the invalidated monitor: %w", err)
	}
	if identifier != opcua.DeleteMonitoredItemsResponseEncodingID {
		return fmt.Errorf("delete monitored items answered with service %d", identifier)
	}
	response, err := decoder.ReadDeleteMonitoredItemsResponse()
	if err != nil {
		return err
	}
	if response.Header.ServiceResult != opcua.StatusGood || len(response.Results) != 1 || response.Results[0] != opcua.StatusGood {
		return fmt.Errorf("delete the invalidated monitor returned %s, %v", response.Header.ServiceResult.Hex(), response.Results)
	}
	return nil
}

func signalFixtureController(directory, name string) error {
	file, err := os.OpenFile(filepath.Join(directory, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("signal fixture controller %s: %w", name, err)
	}
	return file.Close()
}

func waitFixtureController(ctx context.Context, directory, name string) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	path := filepath.Join(directory, name)
	for {
		if info, err := os.Stat(path); err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("fixture controller signal %s is not a file", name)
			}
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("fixture controller did not signal %s: %w", name, ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}
