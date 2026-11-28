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

	"github.com/jiind/opcda-access-adapter/internal/opcda"
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

const (
	fixtureArrayItemID        = "ValidationArrays/Int32"
	fixtureMatrixItemID       = "ValidationArrays/Matrix"
	fixtureOffsetMatrixItemID = "ValidationArrays/OffsetMatrix"
)

var fixtureMatrixElements = []int32{0, 1, 2, 10, 11, 12}

func writeFixtureArray(ctx context.Context, writer *url.URL, itemID string, shape fixtureArrayShape) error {
	shape.ElementDataType = "VT_I4"
	payload := struct {
		Items []fixtureArrayWrite `json:"items"`
	}{Items: []fixtureArrayWrite{{
		ItemID: itemID, DataType: "VT_I4|VT_ARRAY", ValueEncoding: "array", Value: shape,
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
	if len(reply.Results) != 1 || !reply.Results[0].OK || reply.Results[0].ItemID != itemID {
		return fmt.Errorf("array fixture HTTP Write failed or changed the ItemID")
	}
	return nil
}

func (c *client) validateFixtureArraySubscription(ctx context.Context, token opcua.ChannelSecurityToken, session opcua.NodeID, writerURL string) error {
	writer, err := fixtureArrayWriterURL(writerURL)
	if err != nil {
		return err
	}
	vectorDimensions := []fixtureArrayDimension{{Length: 3}}
	if err := c.validateFixtureArrayChanges(ctx, token, session, writer, fixtureArrayItemID,
		fixtureArrayShape{Dimensions: vectorDimensions, Elements: []int32{2, -7, 31}},
		[]fixtureArrayShape{{Dimensions: vectorDimensions, Elements: []int32{-31, 7, 2}}},
	); err != nil {
		return err
	}
	matrixDimensions := []fixtureArrayDimension{{Length: 2}, {Length: 3}}
	return c.validateFixtureArrayChanges(ctx, token, session, writer, fixtureMatrixItemID,
		fixtureArrayShape{Dimensions: matrixDimensions, Elements: fixtureMatrixElements},
		[]fixtureArrayShape{
			{Dimensions: matrixDimensions, Elements: []int32{30, 31, 32, 40, 41, 42}},
			{Dimensions: []fixtureArrayDimension{{LowerBound: 1, Length: 2}, {LowerBound: -2, Length: 3}},
				Elements: []int32{50, 51, 52, 60, 61, 62}},
		},
	)
}

func (c *client) validateFixtureArrayChanges(ctx context.Context, token opcua.ChannelSecurityToken, session opcua.NodeID,
	writer *url.URL, itemID string, original fixtureArrayShape, changes []fixtureArrayShape) (err error) {
	subscription, err := c.createSubscription(token, session, 0, 0)
	if err != nil {
		return err
	}
	id := subscription.SubscriptionID
	defer func() {
		err = errors.Join(err, c.deleteSubscription(token, session, id))
	}()
	const handle = 4444
	monitored, err := c.createMonitoredItems(token, session, id, opcua.TimestampsNeither,
		monitoredTarget{node: opcua.ItemNodeID(opcda.DAItemID(itemID)), handle: handle},
	)
	if err != nil {
		return err
	}
	if monitored[0].StatusCode != opcua.StatusGood {
		return fmt.Errorf("array MonitoredItem returned %s", monitored[0].StatusCode.Hex())
	}
	if err := c.awaitFixtureArray(ctx, token, session, id, handle, original.sample()); err != nil {
		return fmt.Errorf("initial notification for %s: %w", itemID, err)
	}
	var changeErr error
	for _, changed := range changes {
		changeErr = writeFixtureArray(ctx, writer, itemID, changed)
		if changeErr == nil {
			changeErr = c.awaitFixtureArray(ctx, token, session, id, handle, changed.sample())
		}
		if changeErr != nil {
			changeErr = fmt.Errorf("changed notification for %s: %w", itemID, changeErr)
			break
		}
	}
	restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	restoreErr := writeFixtureArray(restoreCtx, writer, itemID, original)
	cancel()
	if err := errors.Join(changeErr, restoreErr); err != nil {
		return err
	}
	if err := c.awaitFixtureArray(ctx, token, session, id, handle, original.sample()); err != nil {
		return fmt.Errorf("restored notification for %s: %w", itemID, err)
	}
	return nil
}

type fixtureArraySample struct {
	status     opcua.StatusCode
	dimensions []int32
	elements   []int32
}

func (shape fixtureArrayShape) sample() fixtureArraySample {
	sample := fixtureArraySample{elements: shape.Elements}
	for _, dimension := range shape.Dimensions {
		if dimension.LowerBound != 0 {
			return fixtureArraySample{status: opcua.StatusBadNotSupported}
		}
		if len(shape.Dimensions) > 1 {
			sample.dimensions = append(sample.dimensions, int32(dimension.Length))
		}
	}
	return sample
}

func (sample *fixtureArraySample) matches(expected fixtureArraySample) bool {
	return sample != nil && sample.status == expected.status &&
		slices.Equal(sample.dimensions, expected.dimensions) && slices.Equal(sample.elements, expected.elements)
}

func (c *client) awaitFixtureArray(ctx context.Context, token opcua.ChannelSecurityToken, session opcua.NodeID, subscription, handle uint32, expected fixtureArraySample) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		decoder, err := c.publishReply(token, session)
		if err != nil {
			return err
		}
		actual, err := readFixturePublish(decoder, c.limits, subscription, handle)
		if err != nil {
			return err
		}
		if actual.matches(expected) {
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

func readFixturePublish(decoder *opcua.Decoder, limits opcua.BinaryLimits, subscription, handle uint32) (*fixtureArraySample, error) {
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
	sample, err := readFixtureArraySample(value)
	if err != nil {
		return nil, err
	}
	if count, _, err := value.ReadArrayLength(1); err != nil || count != 0 || value.Remaining() != 0 {
		return nil, fmt.Errorf("array notification carried unexpected trailing data")
	}
	return sample, nil
}

func readFixtureArraySample(decoder *opcua.Decoder) (*fixtureArraySample, error) {
	mask, err := decoder.ReadByteValue()
	if err != nil {
		return nil, err
	}
	if mask == 0x02 {
		status, err := decoder.ReadStatusCode()
		if err != nil {
			return nil, err
		}
		if status != opcua.StatusBadNotSupported {
			return nil, fmt.Errorf("UA array returned status %s without a value", status.Hex())
		}
		return &fixtureArraySample{status: status}, nil
	}
	if mask != 0x01 {
		return nil, fmt.Errorf("UA array did not carry a successful value-only DataValue")
	}
	variant, err := decoder.ReadByteValue()
	const int32Array = 0x80 | byte(opcua.BuiltInInt32)
	if err != nil || (variant != int32Array && variant != int32Array|0x40) {
		return nil, fmt.Errorf("UA array did not carry an Int32 array Variant")
	}
	elements, err := readFixtureInt32s(decoder)
	if err != nil {
		return nil, err
	}
	sample := &fixtureArraySample{elements: elements}
	if variant&0x40 != 0 {
		sample.dimensions, err = readFixtureInt32s(decoder)
		if err != nil {
			return nil, err
		}
	}
	return sample, nil
}

func readFixtureInt32s(decoder *opcua.Decoder) ([]int32, error) {
	count, isNull, err := decoder.ReadArrayLength(4)
	if err != nil || isNull || count == 0 {
		return nil, fmt.Errorf("UA array returned an invalid list length")
	}
	entries := make([]int32, count)
	for index := range entries {
		entries[index], err = decoder.ReadInt32()
		if err != nil {
			return nil, err
		}
	}
	return entries, nil
}
