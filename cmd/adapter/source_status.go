package main

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/jiind/opcda-access-adapter/internal/opcda"
)

const sourceStatusInterval = time.Second

type sourceStatusReporter struct {
	lastMessage string
}

func (reporter *sourceStatusReporter) update(status opcda.RuntimeStatus) (slog.Level, string) {
	var message string
	level := slog.LevelWarn
	switch status.State {
	case opcda.RuntimeStateConnected:
		level = slog.LevelInfo
		message = fmt.Sprintf("OPC DA source connected (generation %d)", status.ConnectionGeneration)
	case opcda.RuntimeStateDisconnected, opcda.RuntimeStateReconnecting:
		message = "OPC DA source disconnected"
		if status.LastSourceErrorSet {
			diagnostic := status.LastSourceError
			message += ": " + diagnostic.Operation
			if diagnostic.HRESULTPresent {
				message += " (" + diagnostic.HRESULT.Hex() + ")"
			}
		}
	case opcda.RuntimeStateDegraded:
		message = "OPC DA runtime degraded: " + status.DegradedReason
	default:
		return level, ""
	}
	if message == reporter.lastMessage {
		return level, ""
	}
	reporter.lastMessage = message
	return level, message
}

// serviceStartupEvents holds the startup record and the latest DA connection
// status until Windows accepts them. It carries no process values.
type serviceStartupEvents struct {
	listener    string
	source      string
	sourceLevel slog.Level
}

func (events *serviceStartupEvents) flush(write func(slog.Level, string) bool) {
	if events.listener != "" {
		if !write(slog.LevelInfo, events.listener) {
			return
		}
		events.listener = ""
	}
	if events.source != "" && write(events.sourceLevel, events.source) {
		events.source = ""
	}
}
