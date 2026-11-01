package runtime_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	gdruntime "github.com/davidherring123/godot-mcp/internal/core/project/runtime"
)

func TestWaitValidatesBounds(t *testing.T) {
	root := newProject(t, false)

	cases := []struct {
		name     string
		params   gdruntime.WaitParams
		fragment string
	}{
		{
			name:     "missing bounds",
			params:   gdruntime.WaitParams{},
			fragment: "specify frames, seconds, or both",
		},
		{
			name:     "too many frames",
			params:   gdruntime.WaitParams{Frames: 601},
			fragment: "frames must be between 0 and 600",
		},
		{
			name:     "negative frames",
			params:   gdruntime.WaitParams{Frames: -1, Seconds: 0.1},
			fragment: "frames must be between 0 and 600",
		},
		{
			name:     "too many seconds",
			params:   gdruntime.WaitParams{Seconds: 30.5},
			fragment: "seconds must be between 0 and 30",
		},
		{
			name:     "unknown basis",
			params:   gdruntime.WaitParams{Frames: 1, Basis: "render"},
			fragment: `basis must be "idle" or "physics"`,
		},
		{
			name:     "timeout out of range",
			params:   gdruntime.WaitParams{Frames: 1, TimeoutSeconds: 61},
			fragment: "timeout_seconds must be between 0 and 60",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := gdruntime.Wait(root, testCase.params)

			assertRuntimeError(t, err, "invalid_argument")
			assertErrorContains(t, err, testCase.fragment)
		})
	}
}

func TestCaptureValidatesWaitBounds(t *testing.T) {
	root := newProject(t, false)

	cases := []struct {
		name     string
		params   gdruntime.CaptureParams
		fragment string
	}{
		{
			name:     "too many frames",
			params:   gdruntime.CaptureParams{WaitFrames: 601},
			fragment: "frames must be between 0 and 600",
		},
		{
			name:     "too many seconds",
			params:   gdruntime.CaptureParams{WaitSeconds: 31},
			fragment: "seconds must be between 0 and 30",
		},
		{
			name:     "unknown basis",
			params:   gdruntime.CaptureParams{WaitFrames: 1, Basis: "render"},
			fragment: `basis must be "idle" or "physics"`,
		},
		{
			name:     "timeout out of range",
			params:   gdruntime.CaptureParams{TimeoutSeconds: -1},
			fragment: "timeout_seconds must be between 0 and 60",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := gdruntime.Capture(root, testCase.params)

			assertRuntimeError(t, err, "invalid_argument")
			assertErrorContains(t, err, testCase.fragment)
		})
	}
}

func TestHeadlessWaitFramesAndSeconds(t *testing.T) {
	executable := requireGodot(t)

	root := newProject(t, false)
	registerEditor(t, root, executable)
	start := startHeadlessRuntime(t, root)

	t.Cleanup(func() {
		_, _ = gdruntime.Stop(root, gdruntime.StopParams{
			Session: start.Session,
			Force:   true,
		})
	})

	idle, err := gdruntime.Wait(root, gdruntime.WaitParams{
		Session:        start.Session,
		Frames:         5,
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("waiting idle frames: %v", err)
	}

	if idle.Basis != "idle" || idle.FramesElapsed < 5 {
		t.Fatalf("idle wait = %+v; want at least 5 idle frames", idle)
	}

	if idle.Frames < idle.StartFrames+5 || idle.ElapsedMS < 0 {
		t.Fatalf("idle wait = %+v; want a progressed frame counter", idle)
	}

	if idle.RequestedFrames != 5 || idle.RequestedSeconds != 0 {
		t.Fatalf("idle wait = %+v; want the requested frame bound", idle)
	}

	physics, err := gdruntime.Wait(root, gdruntime.WaitParams{
		Session:        start.Session,
		Frames:         3,
		Basis:          "physics",
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("waiting physics frames: %v", err)
	}

	if physics.Basis != "physics" || physics.FramesElapsed < 3 {
		t.Fatalf("physics wait = %+v; want at least 3 physics frames", physics)
	}

	seconds, err := gdruntime.Wait(root, gdruntime.WaitParams{
		Session:        start.Session,
		Seconds:        0.2,
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("waiting seconds: %v", err)
	}

	if seconds.ElapsedMS < 200 {
		t.Fatalf("seconds wait = %+v; want at least 200ms elapsed", seconds)
	}

	combined, err := gdruntime.Wait(root, gdruntime.WaitParams{
		Session:        start.Session,
		Frames:         2,
		Seconds:        0.1,
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("waiting frames and seconds: %v", err)
	}

	if combined.FramesElapsed < 2 || combined.ElapsedMS < 100 {
		t.Fatalf("combined wait = %+v; want both bounds met", combined)
	}
}

func TestWaitTimesOutWithoutRunner(t *testing.T) {
	root := t.TempDir()
	done, pid := startRuntimeHelper(t)
	session := "20261023T150405Z-a1b2c3"
	directory := writeRuntimeFixture(t, root, session, runtimeFixture{
		Session:        session,
		PID:            pid,
		Scene:          "res://main.tscn",
		StartedAt:      time.Now().UTC(),
		RunnerProtocol: gdruntime.RunnerProtocolVersion,
	}, nil)

	for _, name := range []string{"requests", "responses"} {
		if err := os.MkdirAll(filepath.Join(directory, name), 0755); err != nil {
			t.Fatal(err)
		}
	}

	writeHeartbeatFixture(t, directory, 8)

	_, err := gdruntime.Wait(root, gdruntime.WaitParams{
		Session:        session,
		Frames:         1,
		TimeoutSeconds: 0.2,
	})

	assertRuntimeError(t, err, "action_failed")
	assertErrorContains(t, err, "timed out after")

	requested, globErr := filepath.Glob(filepath.Join(directory, "requests", "*.json"))
	if globErr != nil {
		t.Fatal(globErr)
	}

	if len(requested) != 1 {
		t.Fatalf("wait requests = %v; want one request in the session file channel", requested)
	}

	assertRuntimeHelperAlive(t, done)
}

func TestCaptureWaitTimesOutWithoutRunner(t *testing.T) {
	root := t.TempDir()
	done, pid := startRuntimeHelper(t)
	session := "20261023T150405Z-a1b2c4"
	directory := writeRuntimeFixture(t, root, session, runtimeFixture{
		Session:        session,
		PID:            pid,
		Scene:          "res://main.tscn",
		StartedAt:      time.Now().UTC(),
		RunnerProtocol: gdruntime.RunnerProtocolVersion,
	}, nil)

	for _, name := range []string{"requests", "responses"} {
		if err := os.MkdirAll(filepath.Join(directory, name), 0755); err != nil {
			t.Fatal(err)
		}
	}

	writeHeartbeatFixture(t, directory, 8)

	_, err := gdruntime.Capture(root, gdruntime.CaptureParams{
		Session:        session,
		WaitFrames:     1,
		Basis:          "physics",
		TimeoutSeconds: 0.2,
	})

	assertRuntimeError(t, err, "action_failed")
	assertErrorContains(t, err, "timed out after")

	requested, globErr := filepath.Glob(filepath.Join(directory, "requests", "*.json"))
	if globErr != nil {
		t.Fatal(globErr)
	}

	if len(requested) != 1 {
		t.Fatalf("capture requests = %v; want one request in the session file channel", requested)
	}

	data, readErr := os.ReadFile(requested[0])
	if readErr != nil {
		t.Fatal(readErr)
	}

	var request struct {
		Op      string  `json:"op"`
		Path    string  `json:"path"`
		Basis   string  `json:"basis"`
		Frames  int     `json:"frames"`
		Seconds float64 `json:"seconds"`
	}

	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatalf("decoding capture request: %v", err)
	}

	if request.Op != "capture" || request.Basis != "physics" || request.Frames != 1 {
		t.Fatalf("capture request = %+v; want a physics frame wait", request)
	}

	if !strings.HasPrefix(request.Path, "res://.godot/godot-mcp/runtime/") {
		t.Fatalf("capture path = %q; want a runtime res:// path", request.Path)
	}

	assertRuntimeHelperAlive(t, done)
}

func TestStopInterruptsActiveWait(t *testing.T) {
	executable := requireGodot(t)

	root := newProject(t, true)
	registerEditor(t, root, executable)
	start := startHeadlessRuntime(t, root)

	t.Cleanup(func() {
		_, _ = gdruntime.Stop(root, gdruntime.StopParams{
			Session: start.Session,
			Force:   true,
		})
	})

	waitDone := make(chan error, 1)

	go func() {
		_, err := gdruntime.Wait(root, gdruntime.WaitParams{
			Session:        start.Session,
			Seconds:        10,
			TimeoutSeconds: 20,
		})
		waitDone <- err
	}()

	// Give the runner time to accept the request and enter its wait loop.
	time.Sleep(200 * time.Millisecond)

	stop, err := gdruntime.Stop(root, gdruntime.StopParams{
		Session:        start.Session,
		TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatalf("stopping during a wait: %v", err)
	}

	if !stop.Stopped || stop.State != gdruntime.StateExited {
		t.Fatalf("stop = %+v; want the waiting session to stop", stop)
	}

	select {
	case waitErr := <-waitDone:
		assertRuntimeError(t, waitErr, "failed_precondition")
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not finish after the runtime stopped")
	}
}

func TestWindowedCaptureWithWaitProducesImage(t *testing.T) {
	if os.Getenv("GODOT_MODE") != "graphical" {
		t.Skip("set GODOT_MODE=graphical to exercise windowed screenshot capture")
	}

	executable := requireGodot(t)

	root := newProject(t, false)
	registerEditor(t, root, executable)

	start, err := gdruntime.Start(root, gdruntime.StartParams{TimeoutSeconds: 30})
	if err != nil {
		t.Fatalf("starting windowed runtime: %v", err)
	}

	t.Cleanup(func() {
		_, _ = gdruntime.Stop(root, gdruntime.StopParams{
			Session: start.Session,
			Force:   true,
		})
	})

	capture, err := gdruntime.Capture(root, gdruntime.CaptureParams{
		Session:        start.Session,
		WaitFrames:     2,
		WaitSeconds:    0.1,
		TimeoutSeconds: 15,
	})
	if err != nil {
		t.Fatalf("capturing after a bounded wait: %v", err)
	}

	if capture.Width <= 0 || capture.Height <= 0 || capture.Bytes <= 0 {
		t.Fatalf("capture = %+v; want a non-empty image", capture)
	}

	if capture.WaitedFrames < 2 || capture.WaitedMS < 100 {
		t.Fatalf("capture = %+v; want both capture waits reported", capture)
	}

	if capture.Frame < start.Frames+2 {
		t.Fatalf(
			"capture frame = %d, start frames = %d; want the waits to advance frames",
			capture.Frame,
			start.Frames,
		)
	}

	if !strings.HasPrefix(capture.Artifact, "res://.godot/godot-mcp/runtime/") {
		t.Fatalf("artifact = %q; want a runtime res:// path", capture.Artifact)
	}
}

func writeHeartbeatFixture(t *testing.T, directory string, frames int) {
	t.Helper()

	heartbeat := []byte(`{"frames":` + strconv.Itoa(frames) + `,"uptime_ms":100}`)

	if err := os.WriteFile(
		filepath.Join(directory, "heartbeat.json"),
		heartbeat,
		0644,
	); err != nil {
		t.Fatal(err)
	}
}
