package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/jiind/opcda-access-adapter/internal/opcua"
)

func fixtureArrayWriterURL(value string) (*url.URL, error) {
	endpoint, err := url.Parse(value)
	if err != nil || endpoint.Scheme != "http" || endpoint.Hostname() != "127.0.0.1" || endpoint.Port() == "" ||
		endpoint.User != nil || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, fmt.Errorf("array fixture writer must be an IPv4 loopback HTTP endpoint")
	}
	return endpoint.JoinPath("v1", "write"), nil
}

type fixtureArrayWrite struct {
	ItemID        string            `json:"itemId"`
	DataType      string            `json:"dataType"`
	ValueEncoding string            `json:"valueEncoding"`
	Value         fixtureArrayShape `json:"value"`
}

type fixtureArrayShape struct {
	ElementDataType string                  `json:"elementDataType"`
	Dimensions      []fixtureArrayDimension `json:"dimensions"`
	Elements        []int32                 `json:"elements"`
}

type fixtureArrayDimension struct {
	LowerBound int32 `json:"lowerBound"`
	Length     int   `json:"length"`
}

const fixtureArrayItemID = "ValidationArrays/Int32"

func writeFixtureArray(ctx context.Context, writer *url.URL, elements []int32) error {
	payload := struct {
		Items []fixtureArrayWrite `json:"items"`
	}{Items: []fixtureArrayWrite{{
		ItemID: fixtureArrayItemID, DataType: "VT_I4|VT_ARRAY", ValueEncoding: "array",
		Value: fixtureArrayShape{
			ElementDataType: "VT_I4",
			Dimensions:      []fixtureArrayDimension{{Length: len(elements)}},
			Elements:        elements,
		},
	}}}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, writer.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("array fixture HTTP Write: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("array fixture HTTP Write returned status %d", response.StatusCode)
	}
	var reply struct {
		Results []struct {
			OK     bool   `json:"ok"`
			ItemID string `json:"itemId"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&reply); err != nil {
		return fmt.Errorf("invalid HTTP array Write response")
	}
	if len(reply.Results) != 1 || !reply.Results[0].OK || reply.Results[0].ItemID != fixtureArrayItemID {
		return fmt.Errorf("array fixture HTTP Write failed or changed the ItemID")
	}
	return nil
}

func (c *client) validateFixtureArraySubscription(ctx context.Context, token opcua.ChannelSecurityToken, session opcua.NodeID, writerURL string) (err error) {
	writer, err := fixtureArrayWriterURL(writerURL)
	if err != nil {
		return err
	}
	subscription, err := c.createSubscription(token, session, 0, 0)
	if err != nil {
		return err
	}
	id := subscription.SubscriptionID
	defer func() {
		err = errors.Join(err, c.deleteSubscription(token, session, id))
	}()
	const handle = 4444
	monitored, err := c.createMonitoredItems(token, session, id, opcua.ItemNodeID(fixtureArrayItemID), opcua.TimestampsNeither, handle)
	if err != nil {
		return err
	}
	if monitored[0].StatusCode != opcua.StatusGood {
		return fmt.Errorf("array MonitoredItem returned %s", monitored[0].StatusCode.Hex())
	}
	original := []int32{2, -7, 31}
	changed := []int32{-31, 7, 2}
	if err := c.awaitFixtureArray(ctx, token, session, id, handle, original); err != nil {
		return fmt.Errorf("initial array notification: %w", err)
	}
	changeErr := writeFixtureArray(ctx, writer, changed)
	if changeErr == nil {
		changeErr = c.awaitFixtureArray(ctx, token, session, id, handle, changed)
	}
	restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	restoreErr := writeFixtureArray(restoreCtx, writer, original)
	cancel()
	if err := errors.Join(changeErr, restoreErr); err != nil {
		return err
	}
	if err := c.awaitFixtureArray(ctx, token, session, id, handle, original); err != nil {
		return fmt.Errorf("restored array notification: %w", err)
	}
	return nil
}

func (c *client) awaitFixtureArray(ctx context.Context, token opcua.ChannelSecurityToken, session opcua.NodeID, subscription, handle uint32, expected []int32) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		decoder, err := c.publishReply(token, session)
		if err != nil {
			return err
		}
		actual, err := readFixturePublish(decoder, c.limits, subscription, handle, len(expected))
		if err != nil {
			return err
		}
		if slices.Equal(actual, expected) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("the expected array notification did not arrive")
}

func readFixturePublish(decoder *opcua.Decoder, limits opcua.BinaryLimits, subscription, handle uint32, length int) ([]int32, error) {
	header, err := decoder.ReadResponseHeader()
	if err != nil || header.ServiceResult != opcua.StatusGood {
		return nil, fmt.Errorf("array Publish did not return a successful response")
	}
	if id, err := decoder.ReadUInt32(); err != nil || id != subscription {
		return nil, fmt.Errorf("array Publish changed the subscription ID")
	}
	available, _, err := decoder.ReadArrayLength(4)
	if err != nil {
		return nil, err
	}
	for range available {
		if _, err := decoder.ReadUInt32(); err != nil {
			return nil, err
		}
	}
	if _, err := decoder.ReadBoolean(); err != nil {
		return nil, err
	}
	if _, err := decoder.ReadUInt32(); err != nil {
		return nil, err
	}
	if _, err := decoder.ReadDateTime(); err != nil {
		return nil, err
	}
	count, _, err := decoder.ReadArrayLength(3)
	if err != nil || count > 1 {
		return nil, fmt.Errorf("array Publish had unexpected NotificationData")
	}
	if count == 0 {
		return nil, nil
	}
	data, err := decoder.ReadExtensionObject()
	if err != nil || !data.TypeID.Equal(opcua.NumericNodeID(0, opcua.DataChangeNotificationEncodingID)) {
		return nil, fmt.Errorf("array Publish did not carry a data change object")
	}
	value, err := opcua.NewDecoder(data.Body, limits)
	if err != nil {
		return nil, err
	}
	if count, _, err := value.ReadArrayLength(5); err != nil || count != 1 {
		return nil, fmt.Errorf("array Publish did not carry one monitored item")
	}
	if got, err := value.ReadUInt32(); err != nil || got != handle {
		return nil, fmt.Errorf("array Publish changed the client handle")
	}
	if err := readFixtureArrayHeader(value, opcua.BuiltInInt32, length); err != nil {
		return nil, err
	}
	elements := make([]int32, length)
	for index := range elements {
		elements[index], err = value.ReadInt32()
		if err != nil {
			return nil, err
		}
	}
	if count, _, err := value.ReadArrayLength(1); err != nil || count != 0 || value.Remaining() != 0 {
		return nil, fmt.Errorf("array notification carried unexpected trailing data")
	}
	return elements, nil
}
