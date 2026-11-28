package main

import (
	"context"
	"fmt"
	"math"
	"time"

	opcdav1 "github.com/jiind/opcda-access-adapter/api/opcda/v1"
	"github.com/jiind/opcda-access-adapter/internal/opcda"
	"github.com/jiind/opcda-access-adapter/internal/validation/fixture"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const fixtureFloatItem = "Test/Float"

func validateFixtureRecovery(ctx context.Context, client opcdav1.OPCDAAccessClient, signals string) error {
	before, err := waitConnected(ctx, client, 1)
	if err != nil {
		return err
	}
	if before.Runtime == nil || before.Runtime.SubscriptionCount != 0 {
		return fmt.Errorf("the recovery probe expected zero groups before Subscribe")
	}
	original, err := readFixtureFloat(ctx, client)
	if err != nil {
		return err
	}
	oldCtx, cancelOld := context.WithCancel(ctx)
	defer cancelOld()
	oldStream, oldGroup, err := openFixtureFloatStream(oldCtx, client)
	if err != nil {
		return err
	}
	if oldGroup.ConnectionGeneration != before.Source.ConnectionGeneration {
		return fmt.Errorf("the old Subscribe used the wrong connection generation")
	}
	if err := receiveSubscriptionValue(oldStream, []string{fixtureFloatItem}, &original); err != nil {
		return fmt.Errorf("initial recovery notification: %w", err)
	}
	if err := fixture.Signal(signals, "subscribed"); err != nil {
		return err
	}
	if err := fixture.WaitForSignal(ctx, signals, "unavailable"); err != nil {
		return err
	}

	response, err := client.Read(ctx, fixtureFloatRead())
	if response != nil || status.Code(err) != codes.Unavailable {
		return fmt.Errorf("outage Read returned data or %s", status.Code(err))
	}
	message, err := oldStream.Recv()
	if message != nil || !hasDAError(err, codes.Aborted, string(opcda.CodeSubscriptionInvalidated)) {
		return fmt.Errorf("old Subscribe returned data or ended with %s", status.Code(err))
	}
	attempted := float32(17.25)
	if attempted == original {
		attempted = 18.25
	}
	write, err := client.Write(ctx, fixtureFloatWrite(attempted))
	if write != nil || !hasDAError(err, codes.Unavailable, string(opcda.CodeRuntimeUnavailable)) {
		return fmt.Errorf("outage Write returned data or %s", status.Code(err))
	}
	if err := fixture.Signal(signals, "invalidated"); err != nil {
		return err
	}
	if err := fixture.WaitForSignal(ctx, signals, "available"); err != nil {
		return err
	}

	reconnectCtx, cancelReconnect := context.WithTimeout(ctx, 30*time.Second)
	after, err := waitConnected(reconnectCtx, client, before.Source.ConnectionGeneration+1)
	cancelReconnect()
	if err != nil {
		return err
	}
	if after.Runtime == nil || after.Runtime.SubscriptionCount != 0 || after.Runtime.ReconnectCount <= before.Runtime.ReconnectCount {
		return fmt.Errorf("gRPC recovery restored a group or failed to advance the reconnect count")
	}
	recovered, err := readFixtureFloat(ctx, client)
	if err != nil {
		return err
	}
	if recovered == attempted {
		return fmt.Errorf("the refused Write appeared after reconnect")
	}

	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	newStream, newGroup, err := openFixtureFloatStream(streamCtx, client)
	if err != nil {
		return err
	}
	if newGroup.SubscriptionId == oldGroup.SubscriptionId || newGroup.ConnectionGeneration != after.Source.ConnectionGeneration {
		return fmt.Errorf("explicit resubscribe reused an identity or used the wrong generation")
	}
	if err := receiveSubscriptionValue(newStream, []string{fixtureFloatItem}, &recovered); err != nil {
		return fmt.Errorf("new-generation notification: %w", err)
	}
	cancelStream()
	if err := waitForSubscriptionRelease(ctx, client); err != nil {
		return err
	}
	fmt.Println("GRPC_REAL_DA_RECONNECT_PASS sameClient=true aborted=true replay=false explicitResubscribe=true active=0 valuesLogged=false")
	return nil
}

func openFixtureFloatStream(ctx context.Context, client opcdav1.OPCDAAccessClient) (opcdav1.OPCDAAccess_SubscribeClient, *opcdav1.DASubscriptionCreated, error) {
	stream, err := client.Subscribe(ctx, &opcdav1.DASubscribeRequest{
		Items: []*opcdav1.DASubscribeItem{{ItemId: fixtureFloatItem}}, RequestedUpdateRateMs: 250,
	})
	if err != nil {
		return nil, nil, err
	}
	message, err := stream.Recv()
	if err != nil {
		return nil, nil, err
	}
	group := message.GetCreated()
	if group == nil || group.SubscriptionId == "" || group.ActiveItemCount != 1 ||
		len(group.Items) != 1 || !group.Items[0].Active || group.Items[0].ItemId != fixtureFloatItem {
		return nil, nil, fmt.Errorf("Subscribe did not create the fixture group")
	}
	return stream, group, nil
}

func fixtureFloatRead() *opcdav1.DAReadRequest {
	return &opcdav1.DAReadRequest{Items: []*opcdav1.DAReadItem{{ItemId: fixtureFloatItem}}}
}

func readFixtureFloat(ctx context.Context, client opcdav1.OPCDAAccessClient) (float32, error) {
	response, err := client.Read(ctx, fixtureFloatRead())
	if err != nil {
		return 0, fmt.Errorf("read the fixture Float: %w", err)
	}
	if len(response.Results) != 1 {
		return 0, fmt.Errorf("read the fixture Float returned %d results", len(response.Results))
	}
	row := response.Results[0]
	value, ok := row.GetValue().GetValue().(*opcdav1.DAScalarValue_R4Value)
	if !ok || !row.GetOk() || row.GetItemId() != fixtureFloatItem ||
		row.GetCanonicalDataType().GetRaw() != uint32(opcda.VTR4) {
		return 0, fmt.Errorf("read of the fixture item did not return a usable VT_R4")
	}
	return value.R4Value, nil
}

func fixtureFloatWrite(value float32) *opcdav1.DAWriteRequest {
	return &opcdav1.DAWriteRequest{Items: []*opcdav1.DAWriteItem{{
		ItemId:   fixtureFloatItem,
		DataType: &opcdav1.DAVarType{Raw: uint32(opcda.VTR4), Name: "VT_R4"},
		Value:    &opcdav1.DAScalarValue{Value: &opcdav1.DAScalarValue_R4Value{R4Value: value}},
	}}}
}

func writeFixtureFloat(ctx context.Context, client opcdav1.OPCDAAccessClient, value float32) error {
	response, err := client.Write(ctx, fixtureFloatWrite(value))
	if err != nil {
		return err
	}
	if len(response.Results) != 1 || !response.Results[0].GetOk() {
		return fmt.Errorf("the fixture did not accept the typed Write")
	}
	return nil
}

func restoreFixtureFloat(ctx context.Context, client opcdav1.OPCDAAccessClient, original float32) error {
	if err := writeFixtureFloat(ctx, client, original); err != nil {
		return fmt.Errorf("restore the fixture Float: %w", err)
	}
	restored, err := readFixtureFloat(ctx, client)
	if err != nil {
		return fmt.Errorf("read the restored fixture Float: %w", err)
	}
	if math.Float32bits(restored) != math.Float32bits(original) {
		return fmt.Errorf("the fixture Float did not restore")
	}
	return nil
}
