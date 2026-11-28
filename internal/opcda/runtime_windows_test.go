//go:build windows

package opcda

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestWindowsRuntimeRepeatedStartStopWithoutSource(t *testing.T) {
	for iteration := 0; iteration < 20; iteration++ {
		runtime, err := New(Config{Limits: DefaultLimits()})
		if err != nil {
			t.Fatalf("iteration %d: New() error = %v", iteration, err)
		}
		if state := runtime.Status(context.Background()).State; state != RuntimeStateNotConfigured {
			t.Fatalf("iteration %d: state = %q, want %q", iteration, state, RuntimeStateNotConfigured)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = runtime.Shutdown(ctx)
		cancel()
		if err != nil {
			t.Fatalf("iteration %d: Shutdown() error = %v", iteration, err)
		}
		if state := runtime.Status(context.Background()).State; state != RuntimeStateStopped {
			t.Fatalf("iteration %d: final state = %q, want %q", iteration, state, RuntimeStateStopped)
		}
	}
}

func TestWindowsRuntimeRejectsInvalidFoundationConfig(t *testing.T) {
	tests := []Config{
		{Limits: Limits{}},
		{
			Source: SourceConfig{ProgID: "Vendor.Server", CLSID: "{00000000-0000-0000-0000-000000000000}"},
			Limits: DefaultLimits(),
		},
	}
	for _, config := range tests {
		if _, err := New(config); err == nil {
			t.Fatalf("New(%+v) unexpectedly succeeded", config)
		}
	}
}

func TestIOPCServerIIDMatchesOfficialIDL(t *testing.T) {
	if got, want := iidIOPCServer.String(), "{39C13A4D-011E-11D0-9675-0020AFD8ADB3}"; got != want {
		t.Fatalf("IOPCServer IID = %s, want %s", got, want)
	}
}

func TestConnectionGenerationMonotonicallyInvalidatesHandles(t *testing.T) {
	session := &daThreadSession{}
	session.beginConnectionGeneration(2)
	firstGeneration := session.generation
	if !session.registrations.put(itemRegistration{ItemID: "A", ServerHandle: 10, Generation: firstGeneration}) {
		t.Fatal("failed to register first-generation handle")
	}
	session.disconnect()
	if session.registrations != nil {
		t.Fatal("registration cache survived disconnect")
	}
	session.beginConnectionGeneration(2)
	if session.generation != firstGeneration+1 {
		t.Fatalf("generation = %d, want %d", session.generation, firstGeneration+1)
	}
	if _, ok := session.registrations.get("A"); ok {
		t.Fatal("first-generation handle survived reconnect")
	}
}

type subscribeHandoffTestContext struct {
	context.Context
	checked chan struct{}
	once    sync.Once
}

func (ctx *subscribeHandoffTestContext) Err() error {
	err := ctx.Context.Err()
	ctx.once.Do(func() { close(ctx.checked) })
	return err
}

func TestSubscribeHandoffReleasesGroupWhenCallerLeaves(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checked := &subscribeHandoffTestContext{Context: ctx, checked: make(chan struct{})}
	runtime := &windowsRuntime{}
	session := &daThreadSession{subscriptions: make(map[SubscriptionID]*daSubscription)}
	subscription := &daSubscription{id: "sub-1-1", pending: newPendingUpdates(0)}
	accepted := make(chan *daSubscription)
	finished := make(chan struct{})

	// Stop the DA thread at the status update after its initial context
	// check. The caller leaves before the group can be handed over.
	runtime.statusMu.Lock()
	go func() {
		runtime.handoffSubscription(checked, session, subscription, accepted)
		close(finished)
	}()
	select {
	case <-checked.checked:
	case <-time.After(5 * time.Second):
		runtime.statusMu.Unlock()
		t.Fatal("the subscription handoff did not start")
	}
	cancel()
	runtime.statusMu.Unlock()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the abandoned group blocked the DA thread")
	}
	if len(session.subscriptions) != 0 || runtime.Status(context.Background()).SubscriptionCount != 0 {
		t.Fatal("the abandoned group was left active")
	}
	select {
	case <-subscription.Done():
		if err, ok := AsAdapterError(subscription.Err()); !ok || err.Code != CodeSubscriptionInvalidated {
			t.Fatalf("invalidation error = %v", subscription.Err())
		}
	default:
		t.Fatal("the abandoned group was not invalidated")
	}
}

func TestSubscribeHandoffKeepsGroupAcceptedByCaller(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime := &windowsRuntime{}
	session := &daThreadSession{subscriptions: make(map[SubscriptionID]*daSubscription)}
	subscription := &daSubscription{id: "sub-1-1", pending: newPendingUpdates(0)}
	accepted := make(chan *daSubscription)
	finished := make(chan struct{})
	go func() {
		runtime.handoffSubscription(ctx, session, subscription, accepted)
		close(finished)
	}()
	select {
	case actual := <-accepted:
		if actual != subscription {
			t.Fatal("the caller accepted a different subscription")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the subscription was not delivered")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the DA thread did not finish the subscription handoff")
	}
	if session.subscriptions[subscription.id] != subscription || runtime.Status(context.Background()).SubscriptionCount != 1 {
		t.Fatal("the accepted group was removed when the request ended")
	}
	select {
	case <-subscription.Done():
		t.Fatal("the accepted group was invalidated")
	default:
	}
}

func TestWindowsRuntimeQueueBackpressureAndDegradedFailFast(t *testing.T) {
	wake, err := createWakeEvent()
	if err != nil {
		t.Fatal(err)
	}
	defer wake.close()
	runtime := &windowsRuntime{
		config:   Config{Limits: DefaultLimits()},
		commands: make(chan daThreadCommand, 1),
		wake:     wake,
		status:   RuntimeStatus{State: RuntimeStateConnected},
	}
	command := daThreadCommand{context: context.Background(), name: "test", run: func(*daThreadSession) {}}
	if err := runtime.enqueue(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	err = runtime.enqueue(context.Background(), command)
	adapterErr, ok := AsAdapterError(err)
	if !ok || adapterErr.Code != CodeQueueFull {
		t.Fatalf("second enqueue error = %v, want %s", err, CodeQueueFull)
	}
	runtime.markDegraded("test degradation")
	err = runtime.enqueue(context.Background(), command)
	adapterErr, ok = AsAdapterError(err)
	if !ok || adapterErr.Code != CodeRuntimeUnavailable {
		t.Fatalf("degraded enqueue error = %v, want %s", err, CodeRuntimeUnavailable)
	}
}

func TestCOMCallWatchdogMarksRuntimeDegraded(t *testing.T) {
	runtime := &windowsRuntime{
		config: Config{COMCallWatchdog: 10 * time.Millisecond},
		status: RuntimeStatus{
			State:        RuntimeStateConnected,
			Capabilities: Capabilities{Browse: "supported", Read: true, Write: true},
		},
	}
	finish := runtime.beginCOMWatchdog("test call")
	defer finish()
	deadline := time.Now().Add(time.Second)
	for !runtime.degraded.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	status := runtime.Status(context.Background())
	if status.State != RuntimeStateDegraded || status.Capabilities.Read || status.DegradedReason == "" {
		t.Fatalf("watchdog status = %+v", status)
	}
}

func TestScheduleReconnectExposesDisconnectedBackoffState(t *testing.T) {
	runtime := &windowsRuntime{
		config: Config{ReconnectInitial: time.Second, ReconnectMax: 30 * time.Second},
		status: RuntimeStatus{State: RuntimeStateConnected},
	}
	session := &daThreadSession{jitterState: 1}
	runtime.scheduleReconnect(session)
	status := runtime.Status(context.Background())
	if status.State != RuntimeStateDisconnected || session.reconnectAt.IsZero() || session.reconnectAttempt != 1 {
		t.Fatalf("scheduled reconnect = status %+v, session %+v", status, session)
	}
}

func TestSourceFailureDiagnosticPreservesOperationAndHRESULT(t *testing.T) {
	runtime := &windowsRuntime{status: RuntimeStatus{State: RuntimeStateConnecting}}
	runtime.recordSourceFailure("fallback", &comCallError{
		Operation: "CoCreateInstance(IOPCServer)",
		HRESULT:   HRESULT(-2147024891), // 0x80070005 E_ACCESSDENIED
	})

	status := runtime.Status(context.Background())
	if !status.LastSourceErrorSet {
		t.Fatal("source diagnostic was not recorded")
	}
	if got, want := status.LastSourceError.Operation, "CoCreateInstance(IOPCServer)"; got != want {
		t.Fatalf("operation = %q, want %q", got, want)
	}
	if !status.LastSourceError.HRESULTPresent || status.LastSourceError.HRESULT.Hex() != "0x80070005" {
		t.Fatalf("HRESULT diagnostic = %+v", status.LastSourceError)
	}

	runtime.updateStatus(func(status *RuntimeStatus) {
		status.LastSourceError = SourceDiagnostic{}
		status.LastSourceErrorSet = false
	})
	if runtime.Status(context.Background()).LastSourceErrorSet {
		t.Fatal("source diagnostic survived successful-connection reset")
	}
}
