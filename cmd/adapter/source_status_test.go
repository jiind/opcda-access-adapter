package main

import (
	"log/slog"
	"slices"
	"testing"

	"github.com/jiind/opcda-access-adapter/internal/opcda"
)

func TestServiceStartupEventsRetryUntilWritten(t *testing.T) {
	events := serviceStartupEvents{listener: "listener started", source: "source connected", sourceLevel: slog.LevelInfo}
	var attempts []string
	events.flush(func(_ slog.Level, message string) bool {
		attempts = append(attempts, message)
		return false
	})
	events.flush(func(_ slog.Level, message string) bool {
		attempts = append(attempts, message)
		return message == "listener started"
	})
	// A new source status supersedes an older status Windows has yet to accept.
	events.source = "source disconnected"
	events.sourceLevel = slog.LevelWarn
	send := func(level slog.Level, message string) bool {
		attempts = append(attempts, message)
		if level != slog.LevelWarn {
			t.Fatal("the pending source warning lost its severity")
		}
		return true
	}
	events.flush(send)
	events.flush(send)
	want := []string{"listener started", "listener started", "source connected", "source disconnected"}
	if !slices.Equal(attempts, want) {
		t.Fatalf("attempts = %q, want %q", attempts, want)
	}
}

func TestSourceStatusReporterSeparatesFailuresAndRecovery(t *testing.T) {
	var reporter sourceStatusReporter
	check := func(status opcda.RuntimeStatus, wantLevel slog.Level, wantMessage string) {
		t.Helper()
		level, message := reporter.update(status)
		if message != wantMessage || (message != "" && level != wantLevel) {
			t.Fatalf("state %s reported (%s, %q), want (%s, %q)", status.State, level, message, wantLevel, wantMessage)
		}
	}

	check(opcda.RuntimeStatus{State: opcda.RuntimeStateConnecting}, slog.LevelInfo, "")
	status := opcda.RuntimeStatus{
		State: opcda.RuntimeStateDisconnected,
		LastSourceError: opcda.SourceDiagnostic{
			Operation: "CoCreateInstance(IOPCServer)", HRESULT: opcda.HRESULT(-2147024891), HRESULTPresent: true,
		},
		LastSourceErrorSet: true,
	}
	check(status, slog.LevelWarn, "OPC DA source disconnected: CoCreateInstance(IOPCServer) (0x80070005)")
	status.State = opcda.RuntimeStateReconnecting
	status.ReconnectCount++
	check(status, slog.LevelWarn, "")
	status.State = opcda.RuntimeStateDisconnected
	check(status, slog.LevelWarn, "")

	status.State = opcda.RuntimeStateConnected
	status.ConnectionGeneration = 1
	check(status, slog.LevelInfo, "OPC DA source connected (generation 1)")
	check(status, slog.LevelInfo, "")
	status.ConnectionGeneration = 2
	check(status, slog.LevelInfo, "OPC DA source connected (generation 2)")
	status.State = opcda.RuntimeStateDegraded
	status.DegradedReason = "DA thread wait failed; process restart is required"
	check(status, slog.LevelWarn, "OPC DA runtime degraded: DA thread wait failed; process restart is required")
}
