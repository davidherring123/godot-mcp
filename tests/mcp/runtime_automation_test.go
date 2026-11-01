package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/davidherring123/godot-mcp/internal/core"
	gdruntime "github.com/davidherring123/godot-mcp/internal/core/project/runtime"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPLiveRuntimeAutomationInputWaitLogs(t *testing.T) {
	session := startLiveSession(t)
	mcpSession := connectMCP(t, session.Root(), []core.ActionDefinition{
		core.RuntimeStart,
		core.RuntimeInput,
		core.RuntimeWait,
		core.RuntimeLogs,
		core.RuntimeStop,
	})

	schemas := runtimeAutomationOutputSchemas(t, mcpSession, []string{
		"runtime_start",
		"runtime_input",
		"runtime_wait",
		"runtime_logs",
		"runtime_stop",
	})

	sessionID := runtimeAutomationStart(t, mcpSession, schemas)

	runtimeAutomationPressSpace(t, mcpSession, schemas, sessionID)
	runtimeAutomationWaitPhysics(t, mcpSession, schemas, sessionID, 3)
	runtimeAutomationReleaseSpace(t, mcpSession, schemas, sessionID)
	runtimeAutomationWaitPhysics(t, mcpSession, schemas, sessionID, 2)

	logs := runtimeAutomationLogs(t, mcpSession, schemas, sessionID)
	runtimeAutomationAssertEvidence(t, logs)

	stopped := runtimeAutomationCall(t, mcpSession, schemas["runtime_stop"], "runtime_stop", map[string]any{
		"session":         sessionID,
		"timeout_seconds": 10,
	})

	var stop gdruntime.StopResult
	runtimeAutomationDecode(t, stopped, &stop)

	if !stop.Stopped || stop.State != gdruntime.StateExited {
		t.Fatalf("runtime_stop result = %+v; want the session to exit", stop)
	}
}

func runtimeAutomationStart(
	t *testing.T,
	session *mcpgo.ClientSession,
	schemas map[string]any,
) string {
	t.Helper()

	started := runtimeAutomationCall(t, session, schemas["runtime_start"], "runtime_start", map[string]any{
		"scene":           "res://cases/runtime_automation/main.tscn",
		"headless":        true,
		"timeout_seconds": 30,
	})

	var status gdruntime.StatusResult
	runtimeAutomationDecode(t, started, &status)

	if !status.Active || status.State != gdruntime.StateRunning {
		t.Fatalf("runtime_start status = %+v; want an active running session", status)
	}

	if !status.Headless {
		t.Fatalf("runtime_start status = %+v; want a headless session", status)
	}

	if status.Session == "" {
		t.Fatal("runtime_start returned no session ID")
	}

	t.Cleanup(func() {
		runtimeAutomationStop(t, session, schemas["runtime_stop"], status.Session)
	})

	return status.Session
}

func runtimeAutomationPressSpace(
	t *testing.T,
	session *mcpgo.ClientSession,
	schemas map[string]any,
	sessionID string,
) {
	t.Helper()

	pressed := runtimeAutomationCall(t, session, schemas["runtime_input"], "runtime_input", map[string]any{
		"session": sessionID,
		"events": []any{
			map[string]any{"type": "key_press", "key": "Space"},
		},
		"timeout_seconds": 10,
	})

	var press gdruntime.InputResult
	runtimeAutomationDecode(t, pressed, &press)

	if press.Dispatched != 1 || len(press.Reports) != 1 {
		t.Fatalf("key press result = %+v; want one dispatched report", press)
	}

	report := press.Reports[0]
	if report.Type != "key_press" || report.Delivery != "parse_input_event" {
		t.Fatalf("key press report = %+v; want an engine-delivered key press", report)
	}

	if !report.Callback || !report.Polled {
		t.Fatalf("key press report = %+v; want callback and polling observation", report)
	}

	if !runtimeAutomationContainsKey(press.Held.Keys, 32) {
		t.Fatalf("held keys after key press = %v; want Space (32)", press.Held.Keys)
	}
}

func runtimeAutomationReleaseSpace(
	t *testing.T,
	session *mcpgo.ClientSession,
	schemas map[string]any,
	sessionID string,
) {
	t.Helper()

	released := runtimeAutomationCall(t, session, schemas["runtime_input"], "runtime_input", map[string]any{
		"session": sessionID,
		"events": []any{
			map[string]any{"type": "key_release", "key": "Space"},
		},
		"timeout_seconds": 10,
	})

	var release gdruntime.InputResult
	runtimeAutomationDecode(t, released, &release)

	if release.Dispatched != 1 || len(release.Reports) != 1 {
		t.Fatalf("key release result = %+v; want one dispatched report", release)
	}

	report := release.Reports[0]
	if report.Type != "key_release" || report.Delivery != "parse_input_event" {
		t.Fatalf("key release report = %+v; want an engine-delivered key release", report)
	}

	if !report.Callback || report.Polled {
		t.Fatalf("key release report = %+v; want a callback and an unpressed poll", report)
	}

	if len(release.Held.Keys) != 0 {
		t.Fatalf("held keys after key release = %v; want none", release.Held.Keys)
	}
}

func runtimeAutomationWaitPhysics(
	t *testing.T,
	session *mcpgo.ClientSession,
	schemas map[string]any,
	sessionID string,
	frames int,
) {
	t.Helper()

	waited := runtimeAutomationCall(t, session, schemas["runtime_wait"], "runtime_wait", map[string]any{
		"session":         sessionID,
		"basis":           "physics",
		"frames":          frames,
		"timeout_seconds": 10,
	})

	var wait gdruntime.WaitResult
	runtimeAutomationDecode(t, waited, &wait)

	if wait.Basis != "physics" || wait.RequestedFrames != frames {
		t.Fatalf("physics wait = %+v; want %d requested physics frames", wait, frames)
	}

	if wait.FramesElapsed < frames {
		t.Fatalf("physics wait elapsed frames = %d; want at least %d", wait.FramesElapsed, frames)
	}
}

func runtimeAutomationLogs(
	t *testing.T,
	session *mcpgo.ClientSession,
	schemas map[string]any,
	sessionID string,
) gdruntime.LogsResult {
	t.Helper()

	logged := runtimeAutomationCall(t, session, schemas["runtime_logs"], "runtime_logs", map[string]any{
		"session":   sessionID,
		"source":    "stdout",
		"max_bytes": 65536,
	})

	var logs gdruntime.LogsResult
	runtimeAutomationDecode(t, logged, &logs)

	if logs.Source != "stdout" || logs.Stdout == nil {
		t.Fatalf("runtime_logs result = %+v; want a stdout stream", logs)
	}

	if logs.Stdout.TotalBytes == 0 || logs.Stdout.NextCursor != logs.Stdout.TotalBytes || logs.Stdout.Truncated {
		t.Fatalf("runtime stdout stream = %+v; want the complete log read", logs.Stdout)
	}

	return logs
}

func runtimeAutomationAssertEvidence(t *testing.T, logs gdruntime.LogsResult) {
	t.Helper()

	evidence := []string{
		"runtime_automation: _input saw key_press key=Space",
		"runtime_automation: physics poll observed held=true press_events=1",
		"runtime_automation: _input saw key_release key=Space",
		"runtime_automation: physics poll observed held=false release_events=1",
	}

	previous := -1

	for _, fragment := range evidence {
		position := strings.Index(logs.Stdout.Text, fragment)

		if position < 0 {
			t.Fatalf("runtime stdout is missing %q:\n%s", fragment, logs.Stdout.Text)
		}

		if position <= previous {
			t.Fatalf("runtime stdout evidence is out of order at %q:\n%s", fragment, logs.Stdout.Text)
		}

		previous = position
	}
}

func runtimeAutomationOutputSchemas(
	t *testing.T,
	session *mcpgo.ClientSession,
	names []string,
) map[string]any {
	t.Helper()

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}

	available := make(map[string]*mcpgo.Tool, len(tools.Tools))
	for _, tool := range tools.Tools {
		available[tool.Name] = tool
	}

	schemas := make(map[string]any, len(names))

	for _, name := range names {
		tool := available[name]
		if tool == nil {
			t.Fatalf("tool %q is not exposed", name)
		}

		if tool.OutputSchema == nil {
			t.Fatalf("tool %q exposes no output schema", name)
		}

		schemas[name] = tool.OutputSchema
	}

	return schemas
}

func runtimeAutomationCall(
	t *testing.T,
	session *mcpgo.ClientSession,
	schema any,
	name string,
	arguments map[string]any,
) *mcpgo.CallToolResult {
	t.Helper()

	result, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{
		Name:      name,
		Arguments: arguments,
	})
	if err != nil {
		t.Fatalf("calling %s: %v", name, err)
	}

	if result.IsError {
		t.Fatalf("%s returned an error: %s", name, mcpText(result))
	}

	assertSchemaValid(t, schema, result.StructuredContent)

	return result
}

func runtimeAutomationDecode(t *testing.T, result *mcpgo.CallToolResult, target any) {
	t.Helper()

	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("encoding the structured content: %v", err)
	}

	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decoding the structured content: %v", err)
	}
}

func runtimeAutomationContainsKey(keys []int, key int) bool {
	for _, candidate := range keys {
		if candidate == key {
			return true
		}
	}

	return false
}

func runtimeAutomationStop(t *testing.T, session *mcpgo.ClientSession, schema any, sessionID string) {
	t.Helper()

	result, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{
		Name: "runtime_stop",
		Arguments: map[string]any{
			"session":         sessionID,
			"timeout_seconds": 10,
			"force":           true,
		},
	})
	if err != nil {
		t.Errorf("stopping the runtime during cleanup: %v", err)

		return
	}

	if result.IsError {
		t.Errorf("stopping the runtime during cleanup: %s", mcpText(result))

		return
	}

	assertSchemaValid(t, schema, result.StructuredContent)
}
