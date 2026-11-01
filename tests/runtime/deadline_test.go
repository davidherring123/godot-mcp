package runtime_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	gdruntime "github.com/davidherring123/godot-mcp/internal/core/project/runtime"
)

// submittedRequest is the subset of a runtime runner request that the deadline
// and protocol tests inspect.
type submittedRequest struct {
	Op              string `json:"op"`
	Path            string `json:"path"`
	ExpiresAtUnixMS int64  `json:"expires_at_unix_ms"`
	BudgetMS        int64  `json:"budget_ms"`
}

func newRunnerSession(t *testing.T, root string, session string, protocol int) string {
	t.Helper()

	_, pid := startRuntimeHelper(t)
	directory := writeRuntimeFixture(t, root, session, runtimeFixture{
		Session:        session,
		PID:            pid,
		Scene:          "res://main.tscn",
		StartedAt:      time.Now().UTC(),
		RunnerProtocol: protocol,
	}, nil)

	for _, name := range []string{"requests", "responses"} {
		if err := os.MkdirAll(filepath.Join(directory, name), 0755); err != nil {
			t.Fatal(err)
		}
	}

	writeHeartbeatFixture(t, directory, 8)

	return directory
}

func readSubmittedRequests(t *testing.T, directory string) []submittedRequest {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(directory, "requests", "*.json"))
	if err != nil {
		t.Fatal(err)
	}

	sort.Strings(paths)

	requests := make([]submittedRequest, 0, len(paths))

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		var request submittedRequest

		if err := json.Unmarshal(data, &request); err != nil {
			t.Fatalf("decoding request %s: %v", filepath.Base(path), err)
		}

		requests = append(requests, request)
	}

	return requests
}

func assertNoSubmittedRequests(t *testing.T, directory string) {
	t.Helper()

	requests := readSubmittedRequests(t, directory)
	if len(requests) != 0 {
		t.Fatalf("requests = %+v; want none written for a rejected legacy session", requests)
	}
}

// TestLegacyRunnerRejectsDeadlineFeatures covers sessions whose metadata has no
// runner protocol version. Those runners do not enforce submission deadlines,
// so wait, input, and capture waits must fail before a request is submitted.
func TestLegacyRunnerRejectsDeadlineFeatures(t *testing.T) {
	root := t.TempDir()
	session := "20261023T150405Z-1e9ac1"
	directory := newRunnerSession(t, root, session, 0)

	cases := []struct {
		name    string
		feature string
		call    func() error
	}{
		{
			name:    "wait",
			feature: "deadline-aware waits",
			call: func() error {
				_, err := gdruntime.Wait(root, gdruntime.WaitParams{
					Session:        session,
					Frames:         2,
					TimeoutSeconds: 1,
				})
				return err
			},
		},
		{
			name:    "input",
			feature: "deadline-aware input",
			call: func() error {
				_, err := gdruntime.Input(root, gdruntime.InputParams{
					Session:        session,
					Events:         []gdruntime.InputEvent{{Type: "action_press", Action: "jump"}},
					TimeoutSeconds: 1,
				})
				return err
			},
		},
		{
			name:    "capture wait",
			feature: "capture waits",
			call: func() error {
				_, err := gdruntime.Capture(root, gdruntime.CaptureParams{
					Session:        session,
					WaitFrames:     1,
					TimeoutSeconds: 1,
				})
				return err
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.call()

			assertRuntimeError(t, err, "failed_precondition")
			assertErrorContains(t, err, "older godot-mcp runner")
			assertErrorContains(t, err, testCase.feature)
			assertErrorContains(t, err, "restart the runtime")
		})
	}

	assertNoSubmittedRequests(t, directory)
}

// TestLegacyRunnerAllowsPlainCapture keeps ordinary screenshots working for a
// session started by an older runner: only the wait features need the protocol.
func TestLegacyRunnerAllowsPlainCapture(t *testing.T) {
	root := t.TempDir()
	session := "20261023T150405Z-1e9ac2"
	directory := newRunnerSession(t, root, session, 0)

	submitted := time.Now()

	_, err := gdruntime.Capture(root, gdruntime.CaptureParams{
		Session:        session,
		TimeoutSeconds: 0.2,
	})

	// No runner consumes the request, so the host reports its own timeout.
	assertRuntimeError(t, err, "action_failed")
	assertErrorContains(t, err, "timed out after")

	requests := readSubmittedRequests(t, directory)
	if len(requests) != 1 {
		t.Fatalf("capture requests = %+v; want one plain capture request", requests)
	}

	if requests[0].Op != "capture" {
		t.Fatalf("capture request = %+v; want op capture", requests[0])
	}

	if requests[0].ExpiresAtUnixMS <= submitted.UnixMilli() {
		t.Fatalf(
			"capture expiry = %d; want a deadline after submission at %d",
			requests[0].ExpiresAtUnixMS,
			submitted.UnixMilli(),
		)
	}
}

// TestRunnerDeadlinePrecedesHostTimeout pins the shared submission deadline:
// every wait, capture, and input request must expire at the runner strictly
// before the host stops waiting, so the runner's failure can be observed.
func TestRunnerDeadlinePrecedesHostTimeout(t *testing.T) {
	root := t.TempDir()
	session := "20261023T150405Z-1e9ac3"
	directory := newRunnerSession(t, root, session, gdruntime.RunnerProtocolVersion)

	cases := []struct {
		name    string
		timeout time.Duration
		call    func() error
	}{
		{
			name:    "wait",
			timeout: 200 * time.Millisecond,
			call: func() error {
				_, err := gdruntime.Wait(root, gdruntime.WaitParams{
					Session:        session,
					Frames:         2,
					TimeoutSeconds: 0.2,
				})
				return err
			},
		},
		{
			name:    "capture",
			timeout: 200 * time.Millisecond,
			call: func() error {
				_, err := gdruntime.Capture(root, gdruntime.CaptureParams{
					Session:        session,
					TimeoutSeconds: 0.2,
				})
				return err
			},
		},
		{
			name:    "input",
			timeout: 400 * time.Millisecond,
			call: func() error {
				_, err := gdruntime.Input(root, gdruntime.InputParams{
					Session:        session,
					Events:         []gdruntime.InputEvent{{Type: "action_press", Action: "jump"}},
					TimeoutSeconds: 0.4,
				})
				return err
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			submitted := time.Now()
			err := testCase.call()

			assertRuntimeError(t, err, "action_failed")
			assertErrorContains(t, err, "timed out after")

			requests := readSubmittedRequests(t, directory)
			if len(requests) == 0 {
				t.Fatal("no runtime request was submitted")
			}

			request := requests[len(requests)-1]
			expiry := time.UnixMilli(request.ExpiresAtUnixMS)
			hostDeadline := submitted.Add(testCase.timeout)

			if !expiry.After(submitted) {
				t.Fatalf(
					"%s expiry = %s; want a deadline after submission at %s",
					testCase.name,
					expiry,
					submitted,
				)
			}

			if !expiry.Before(hostDeadline) {
				t.Fatalf(
					"%s expiry = %s; want it strictly before the host deadline at %s",
					testCase.name,
					expiry,
					hostDeadline,
				)
			}

			if lead := hostDeadline.Sub(expiry); lead < 50*time.Millisecond {
				t.Fatalf(
					"%s deadline lead = %s; want a reporting margin before the host timeout",
					testCase.name,
					lead,
				)
			}

			if testCase.name != "input" {
				return
			}

			if request.BudgetMS <= 0 {
				t.Fatalf("input budget = %dms; want a positive active budget", request.BudgetMS)
			}

			budget := time.Duration(request.BudgetMS) * time.Millisecond

			if budget > hostDeadline.Sub(submitted) {
				t.Fatalf(
					"input budget = %s; want it inside the %s host timeout",
					budget,
					testCase.timeout,
				)
			}

			if delta := expiry.Sub(submitted) - budget; delta > 5*time.Millisecond || delta < -5*time.Millisecond {
				t.Fatalf(
					"input budget = %s but expiry is %s after submission; want them aligned",
					budget,
					expiry.Sub(submitted),
				)
			}
		})
	}
}

// TestAbandonedWaitDoesNotWedgeQueue is the regression for a host that stops
// waiting before the runner finishes: the runner's own deadline must clear the
// active request, or every later wait, input, and capture stalls behind it.
func TestAbandonedWaitDoesNotWedgeQueue(t *testing.T) {
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

	_, err := gdruntime.Wait(root, gdruntime.WaitParams{
		Session:        start.Session,
		Seconds:        10,
		TimeoutSeconds: 0.6,
	})

	assertRuntimeError(t, err, "action_failed")
	assertErrorContains(t, err, "submission deadline")

	if strings.Contains(err.Error(), "timed out after") {
		t.Fatalf("the abandoned wait hit the host timeout instead of the runner deadline: %v", err)
	}

	resumed := time.Now()

	if _, err := gdruntime.Wait(root, gdruntime.WaitParams{
		Session:        start.Session,
		Frames:         2,
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("the queue stayed wedged after an abandoned wait: %v", err)
	}

	if elapsed := time.Since(resumed); elapsed > 2*time.Second {
		t.Fatalf("wait after an abandoned wait took %s; want a free session queue", elapsed)
	}
}

// TestQueuedInputReportsPartialDelivery covers a queued sequence that is nearly
// expired when the runner dequeues it. The runner must report how many events it
// delivered before its own (earlier) deadline, not lose the race to the host
// timeout. The fixture runs at 10 fps so frame-paced dispatch cannot finish the
// sequence inside the remaining budget; the assertion never assumes 60 fps.
func TestQueuedInputReportsPartialDelivery(t *testing.T) {
	executable := requireGodot(t)

	root := newSlowInputProject(t)
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
			Seconds:        0.5,
			TimeoutSeconds: 10,
		})
		waitDone <- err
	}()

	time.Sleep(100 * time.Millisecond)

	events := make([]gdruntime.InputEvent, 32)

	for index := range events {
		kind := "action_press"

		if index%2 == 1 {
			kind = "action_release"
		}

		events[index] = gdruntime.InputEvent{Type: kind, Action: "jump"}
	}

	_, err := gdruntime.Input(root, gdruntime.InputParams{
		Session:        start.Session,
		Events:         events,
		TimeoutSeconds: 1.2,
	})

	select {
	case waitErr := <-waitDone:
		if waitErr != nil {
			t.Fatalf("background wait failed: %v", waitErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("background wait did not finish")
	}

	assertRuntimeError(t, err, "action_failed")
	assertErrorContains(t, err, "events were delivered before the failure")

	if strings.Contains(err.Error(), "timed out after") {
		t.Fatalf("the host timed out before the runner reported partial delivery: %v", err)
	}

	delivered := deliveredInputEvents(t, err.Error())

	if delivered <= 0 || delivered >= len(events) {
		t.Fatalf(
			"partial input delivered %d of %d events; want some but not all: %v",
			delivered,
			len(events),
			err,
		)
	}
}

var deliveredEventsPattern = regexp.MustCompile(`(\d+) of \d+ events were delivered before the failure`)

func deliveredInputEvents(t *testing.T, message string) int {
	t.Helper()

	matches := deliveredEventsPattern.FindStringSubmatch(message)

	if len(matches) != 2 {
		t.Fatalf("error message does not report a delivered event count: %s", message)
	}

	delivered, err := strconv.Atoi(matches[1])
	if err != nil {
		t.Fatalf("parsing delivered event count %q: %v", matches[1], err)
	}

	return delivered
}

// TestCaptureWithDisabledRenderLoopFreesQueue is the regression for a capture
// that never receives RenderingServer.frame_post_draw. The render barrier must
// be bounded, must not write a late artifact, and must leave later requests
// usable.
func TestCaptureWithDisabledRenderLoopFreesQueue(t *testing.T) {
	if os.Getenv("GODOT_MODE") != "graphical" {
		t.Skip("set GODOT_MODE=graphical to exercise windowed screenshot capture")
	}

	executable := requireGodot(t)

	root := newNoRenderProject(t)
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

	if start.Headless {
		t.Fatal("start reported a headless session; want a windowed runtime")
	}

	_, err = gdruntime.Capture(root, gdruntime.CaptureParams{
		Session:        start.Session,
		TimeoutSeconds: 1,
	})

	assertRuntimeError(t, err, "action_failed")
	assertErrorContains(t, err, "no screenshot was written")

	if strings.Contains(err.Error(), "timed out after") {
		t.Fatalf("capture hit the host timeout instead of the runner's render barrier: %v", err)
	}

	captures, globErr := filepath.Glob(filepath.Join(
		root,
		".godot",
		"godot-mcp",
		"runtime",
		"sessions",
		start.Session,
		"captures",
		"*.png",
	))
	if globErr != nil {
		t.Fatal(globErr)
	}

	if len(captures) != 0 {
		t.Fatalf("an expired capture wrote artifacts: %v", captures)
	}

	if _, err := gdruntime.Wait(root, gdruntime.WaitParams{
		Session:        start.Session,
		Frames:         2,
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("capture wedged the session queue: %v", err)
	}
}

func newSlowInputProject(t *testing.T) string {
	t.Helper()

	root := newInputProject(t)
	script := `extends Node2D

func _ready() -> void:
	Engine.max_fps = 10
`

	if err := os.WriteFile(filepath.Join(root, "main.gd"), []byte(script), 0644); err != nil {
		t.Fatal(err)
	}

	return root
}

func newNoRenderProject(t *testing.T) string {
	t.Helper()

	root := newProject(t, false)
	script := `extends Node2D

func _ready() -> void:
	RenderingServer.render_loop_enabled = false
`

	if err := os.WriteFile(filepath.Join(root, "main.gd"), []byte(script), 0644); err != nil {
		t.Fatal(err)
	}

	return root
}
