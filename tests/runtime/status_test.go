package runtime_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gdruntime "github.com/davidherring123/godot-mcp/internal/core/project/runtime"
)

func TestStatusReportsSessionFileFreshness(t *testing.T) {
	root := t.TempDir()
	session := "20261023T150405Z-f1e5a1"
	directory := writeRuntimeFixture(t, root, session, runtimeFixture{
		Session:   session,
		Scene:     "res://main.tscn",
		Headless:  true,
		StartedAt: time.Now().Add(-time.Hour).UTC(),
	}, &runtimeExitFixture{
		Reason: "stop",
		Code:   0,
	})

	writeFixtureJSON(t, filepath.Join(directory, "ready.json"), map[string]any{
		"ok":             true,
		"message":        "",
		"scene":          "res://main.tscn",
		"display_server": "headless",
		"headless":       true,
		"window_size":    map[string]int{"width": 0, "height": 0},
		"frames":         2,
		"pid":            0,
	})
	writeFixtureJSON(t, filepath.Join(directory, "heartbeat.json"), map[string]any{
		"frames":    12,
		"uptime_ms": 250,
	})

	now := time.Now()
	setModTime(t, filepath.Join(directory, "ready.json"), now.Add(-2*time.Second))
	setModTime(t, filepath.Join(directory, "heartbeat.json"), now.Add(-time.Second))

	status, err := gdruntime.Status(root, gdruntime.StatusParams{Session: session})
	if err != nil {
		t.Fatalf("checking status: %v", err)
	}

	observed, err := time.Parse(time.RFC3339, status.ObservedAt)
	if err != nil {
		t.Fatalf("observed_at = %q; want RFC 3339: %v", status.ObservedAt, err)
	}

	if delta := now.Sub(observed); delta < -5*time.Second || delta > 5*time.Second {
		t.Fatalf("observed_at = %s; want a current timestamp", status.ObservedAt)
	}

	if status.ReadyAgeMS < 1500 {
		t.Fatalf("ready age = %dms; want at least 1500ms", status.ReadyAgeMS)
	}

	if status.HeartbeatAgeMS < 700 {
		t.Fatalf("heartbeat age = %dms; want at least 700ms", status.HeartbeatAgeMS)
	}

	if status.HeartbeatAgeMS >= status.ReadyAgeMS {
		t.Fatalf(
			"ages = heartbeat %dms, ready %dms; want the heartbeat to be newer",
			status.HeartbeatAgeMS,
			status.ReadyAgeMS,
		)
	}
}

func setModTime(t *testing.T, path string, when time.Time) {
	t.Helper()

	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}
