package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/jiind/opcda-access-adapter/internal/opcua"
)

const fixtureAnalogItem = "ValidationAnalog/Float"

func (c *client) validateFixtureAnalog(ctx context.Context, token opcua.ChannelSecurityToken, session opcua.NodeID, items []opcua.NodeID) (err error) {
	node := opcua.ItemNodeID(fixtureAnalogItem)
	if !slices.ContainsFunc(items, node.Equal) {
		return fmt.Errorf("Browse did not expose the fixture analog item")
	}
	// Browse before device Read so the next Browse must refresh the property
	// mapping with the DA canonical type learned by Read.
	if _, err := c.browse(token, session, node, 573); err != nil {
		return err
	}
	const original, below, above float32 = 25, 30, 45
	if err := c.checkAnalogValue(token, session, node, original); err != nil {
		return err
	}
	if err := c.validateFixtureEURange(token, session, node); err != nil {
		return err
	}

	filter, err := c.percentDeadbandFilter(10)
	if err != nil {
		return err
	}
	invalidFilter, err := c.percentDeadbandFilter(math.NaN())
	if err != nil {
		return err
	}
	target := monitoredTarget{node: node, handle: 4701, filter: filter}

	subscription, err := c.createSubscription(token, session, 1, 0)
	if err != nil {
		return err
	}
	id := subscription.SubscriptionID
	if subscription.Header.ServiceResult != opcua.StatusGood || id == 0 {
		return fmt.Errorf("CreateSubscription did not create the analog subscription")
	}
	defer func() {
		err = errors.Join(err, c.deleteSubscription(token, session, id))
		if err == nil {
			fmt.Println("OPCUA_REAL_DA_DEADBAND_PASS euRange=true belowSuppressed=true aboveDelivered=true nonAnalogRejected=true nanRejected=true restored=true released=true valuesLogged=false")
		}
	}()

	created, err := c.createMonitoredItems(token, session, id, opcua.TimestampsBoth, target,
		monitoredTarget{node: opcua.ItemNodeID("Test/Float"), handle: target.handle + 1, filter: filter},
		monitoredTarget{node: node, handle: target.handle + 2, filter: invalidFilter})
	if err != nil {
		return err
	}
	if created[0].StatusCode != opcua.StatusGood || created[0].MonitoredItemID == 0 {
		return fmt.Errorf("CreateMonitoredItems with the percent deadband returned %s", created[0].StatusCode.Hex())
	}
	if created[1].StatusCode != opcua.StatusBadDeadbandFilterInvalid || created[1].MonitoredItemID != 0 {
		return fmt.Errorf("non-analog monitor with the percent deadband returned %s", created[1].StatusCode.Hex())
	}
	if created[2].StatusCode != opcua.StatusBadDeadbandFilterInvalid || created[2].MonitoredItemID != 0 {
		return fmt.Errorf("analog monitor with the NaN deadband returned %s", created[2].StatusCode.Hex())
	}
	handles := []uint32{target.handle}
	if _, err := c.awaitMonitoredFloat(ctx, token, session, id, original, handles); err != nil {
		return fmt.Errorf("initial analog notification: %w", err)
	}

	restored := false
	defer func() {
		if !restored {
			err = errors.Join(err, c.writeFloat(token, session, node, original))
		}
	}()
	if err := c.writeFloat(token, session, node, below); err != nil {
		return err
	}
	if err := c.checkAnalogValue(token, session, node, below); err != nil {
		return err
	}
	// Two keep-alives span multiple source sampling intervals after the
	// below-threshold Write while proving the subscription remains active.
	for range 2 {
		response, err := c.publish(token, session)
		if err != nil {
			return err
		}
		if response.SubscriptionID != id || response.NotificationMessage.HasData || response.MoreNotifications {
			return fmt.Errorf("the percent deadband did not hold the below-threshold change")
		}
	}
	if err := c.writeFloat(token, session, node, above); err != nil {
		return err
	}
	if _, err := c.awaitMonitoredFloat(ctx, token, session, id, above, handles); err != nil {
		return fmt.Errorf("above-threshold analog notification: %w", err)
	}
	if err := c.writeFloat(token, session, node, original); err != nil {
		return err
	}
	if err := c.checkAnalogValue(token, session, node, original); err != nil {
		return err
	}
	restored = true
	if _, err := c.awaitMonitoredFloat(ctx, token, session, id, original, handles); err != nil {
		return fmt.Errorf("restored analog notification: %w", err)
	}
	return nil
}

func (c *client) percentDeadbandFilter(percent float64) (opcua.ExtensionObject, error) {
	encoder, err := opcua.NewEncoder(c.limits)
	if err != nil {
		return opcua.ExtensionObject{}, err
	}
	encoder.WriteUInt32(opcua.DataChangeTriggerStatusValue)
	encoder.WriteUInt32(opcua.DeadbandPercent)
	encoder.WriteDouble(percent)
	body, err := encoder.Bytes()
	if err != nil {
		return opcua.ExtensionObject{}, err
	}
	return opcua.ExtensionObject{
		TypeID:   opcua.NumericNodeID(0, opcua.NodeIDDataChangeFilterEncodingDefaultBinary),
		Encoding: opcua.ExtensionObjectByteString,
		Body:     body,
	}, nil
}

func (c *client) checkAnalogValue(token opcua.ChannelSecurityToken, session, node opcua.NodeID, expected float32) error {
	status, value, err := c.readAttributeValue(token, session, node, opcua.AttributeValue, 575)
	if err != nil {
		return err
	}
	actual, ok := value.Value.(float32)
	if status != opcua.StatusGood || value.Type != opcua.BuiltInFloat || value.IsArray || !ok || actual != expected {
		return fmt.Errorf("device Read did not return the expected fixture analog value")
	}
	return nil
}

func (c *client) validateFixtureEURange(token opcua.ChannelSecurityToken, session, item opcua.NodeID) error {
	references, err := c.browse(token, session, item, 576)
	if err != nil {
		return err
	}
	node := opcua.ItemPropertyNodeID(fixtureAnalogItem, "EURange")
	found := slices.ContainsFunc(references, func(reference opcua.ReferenceDescription) bool {
		return reference.IsForward && reference.BrowseName.Name == "EURange" && reference.NodeID.NodeID.Equal(node)
	})
	if !found {
		return fmt.Errorf("Browse did not expose EURange after learning the canonical type")
	}
	status, value, err := c.readAttributeValue(token, session, node, opcua.AttributeValue, 574)
	if err != nil {
		return err
	}
	object, ok := value.Value.(opcua.ExtensionObject)
	if status != opcua.StatusGood || value.Type != opcua.BuiltInExtensionObject || value.IsArray || !ok ||
		!object.TypeID.Equal(opcua.NumericNodeID(0, rangeEncodingID)) || len(object.Body) != rangeBodyByteSize {
		return fmt.Errorf("the fixture analog item did not expose a usable EURange")
	}
	decoder, err := opcua.NewDecoder(object.Body, c.limits)
	if err != nil {
		return err
	}
	low, lowErr := decoder.ReadDouble()
	high, highErr := decoder.ReadDouble()
	if lowErr != nil || highErr != nil || low != 0 || high != 100 {
		return fmt.Errorf("EURange did not preserve the fixture's original limits")
	}
	return nil
}
