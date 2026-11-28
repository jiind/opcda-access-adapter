package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jiind/opcda-access-adapter/internal/app"
	"github.com/jiind/opcda-access-adapter/internal/opcda"
)

// TestGuidedMCPSetupSavesOnly는 프롬프트 출력과 MCP stdio가 섞이지 않게 한다.
func TestGuidedMCPSetupSavesOnly(t *testing.T) {
	// 1. 사용자가 소스·MCP·저장 동작을 각각 명시적으로 선택한다.
	var written app.Config
	started := false
	dependencies := guidedSetupDependencies{
		detect: func(context.Context, opcda.LocalDetectionLimits) ([]opcda.DetectedLocalServer, error) {
			return []opcda.DetectedLocalServer{{CLSID: "{AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA}"}}, nil
		},
		writeConfig: func(_ string, config app.Config) error {
			written = config
			return nil
		},
		runForeground: func(string) error {
			started = true
			return nil
		},
		installAndStart: func(serviceInstallOptions) error {
			started = true
			return nil
		},
	}
	var output, errorOutput bytes.Buffer
	code := runSetup([]string{"--config", "mcp.json"}, strings.NewReader("1\n4\n1\ny\n"), &output, &errorOutput, dependencies)

	// 2. 파일만 생성하고 클라이언트가 실행할 경로를 안내한다.
	if code != 0 || written.Frontend != app.FrontendMCP || written.WriteEnabled || started {
		t.Fatalf("unsafe MCP setup: code=%d started=%t stderr=%s", code, started, errorOutput.String())
	}
	_, launchSettings, found := strings.Cut(output.String(), "Use these launch settings in your MCP client (stdio):\n")
	if !strings.Contains(output.String(), "no network listener") || !found {
		t.Fatal("MCP stdio launch instructions missing")
	}
	var launch struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal([]byte(launchSettings), &launch); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path, err := filepath.Abs("mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	if launch.Command != executable || !slices.Equal(launch.Args, []string{"run", "--config", path}) {
		t.Fatalf("incorrect MCP launch settings: %+v", launch)
	}
}
