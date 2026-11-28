package main

import (
	"slices"
	"testing"

	"github.com/jiind/opcda-access-adapter/internal/opcua"
)

func TestFixtureArrayPublishWireProbe(t *testing.T) {
	limits := opcua.DefaultBinaryLimits()
	want := []int32{2, -7, 31}
	encoder, err := opcua.NewEncoder(limits)
	if err != nil {
		t.Fatal(err)
	}
	encoder.WritePublishResponse(opcua.PublishResponse{
		Header:                   opcua.ResponseHeader{AdditionalHeader: opcua.NullExtensionObject()},
		SubscriptionID:           5,
		AvailableSequenceNumbers: []uint32{8},
		NotificationMessage: opcua.NotificationMessage{
			SequenceNumber: 8, HasData: true,
			Notifications: []opcua.MonitoredItemNotification{{
				ClientHandle: 4444,
				Value:        opcua.DataValue{Value: opcua.Variant{Type: opcua.BuiltInInt32, IsArray: true, Value: want}},
			}},
		},
	})
	wire, err := encoder.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := opcua.NewDecoder(wire, limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.ReadNodeID(); err != nil {
		t.Fatal(err)
	}
	actual, err := readFixturePublish(decoder, limits, 5, 4444, len(want))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(actual, want) {
		t.Fatal("published element order changed")
	}
}
