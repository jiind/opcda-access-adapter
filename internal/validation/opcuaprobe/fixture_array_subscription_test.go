package main

import (
	"testing"

	"github.com/jiind/opcda-access-adapter/internal/opcua"
)

func TestFixtureArrayPublishWireProbe(t *testing.T) {
	limits := opcua.DefaultBinaryLimits()
	for _, test := range []struct {
		name  string
		value opcua.DataValue
		want  fixtureArraySample
	}{
		{
			name:  "vector",
			value: opcua.DataValue{Value: opcua.Variant{Type: opcua.BuiltInInt32, IsArray: true, Value: []int32{2, -7, 31}}},
			want:  fixtureArraySample{elements: []int32{2, -7, 31}},
		},
		{
			name: "matrix",
			value: opcua.DataValue{Value: opcua.Variant{
				Type: opcua.BuiltInInt32, IsArray: true, Value: fixtureMatrixElements, ArrayDimensions: []int32{2, 3},
			}},
			want: fixtureArraySample{dimensions: []int32{2, 3}, elements: fixtureMatrixElements},
		},
		{
			name:  "nonzero lower bound refusal",
			value: opcua.DataValue{Status: opcua.StatusBadNotSupported},
			want:  fixtureArraySample{status: opcua.StatusBadNotSupported},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
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
						Value:        test.value,
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
			actual, err := readFixturePublish(decoder, limits, 5, 4444)
			if err != nil {
				t.Fatal(err)
			}
			if !actual.matches(test.want) {
				t.Fatal("published array data or status changed")
			}
		})
	}
}
