package runtime_test

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	gdruntime "github.com/davidherring123/godot-mcp/internal/core/project/runtime"
	"github.com/invopop/jsonschema"
)

func inputFloat(value float64) *float64 { return &value }

func inputBool(value bool) *bool { return &value }

func inputInt(value int) *int { return &value }

func TestInputValidatesEvents(t *testing.T) {
	root := newInputProject(t)

	cases := []struct {
		name     string
		params   gdruntime.InputParams
		fragment string
	}{
		{
			name:     "missing events",
			params:   gdruntime.InputParams{},
			fragment: "at least one input event",
		},
		{
			name: "too many events",
			params: gdruntime.InputParams{Events: repeatActionPress(
				33,
			)},
			fragment: "at most 32 input events",
		},
		{
			name: "unknown type",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "eval"},
			}},
			fragment: "unknown type",
		},
		{
			name: "action without name",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "action_press"},
			}},
			fragment: "requires an action",
		},
		{
			name: "action strength out of range",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "action_press", Action: "jump", Strength: inputFloat(1.5)},
			}},
			fragment: "strength must be between 0 and 1",
		},
		{
			name: "action release strength",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "action_release", Action: "jump", Strength: inputFloat(1)},
			}},
			fragment: "does not accept strength",
		},
		{
			name: "key and keycode",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "key_press", Key: "Space", Keycode: 32},
			}},
			fragment: "not both",
		},
		{
			name: "key without key or keycode",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "key_press"},
			}},
			fragment: "requires key or keycode",
		},
		{
			name: "negative keycode",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "key_release", Keycode: -1},
			}},
			fragment: "requires key or keycode",
		},
		{
			name: "invalid mouse button",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "mouse_button", Button: "thumb", Pressed: inputBool(true), X: inputFloat(1), Y: inputFloat(1)},
			}},
			fragment: "button must be",
		},
		{
			name: "mouse button without pressed",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "mouse_button", Button: "left", X: inputFloat(1), Y: inputFloat(1)},
			}},
			fragment: "requires pressed",
		},
		{
			name: "mouse button without coordinates",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "mouse_button", Button: "left", Pressed: inputBool(true)},
			}},
			fragment: "unless virtual_cursor is true",
		},
		{
			name: "mouse button with half a position",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "mouse_button", Button: "left", Pressed: inputBool(true), X: inputFloat(1)},
			}},
			fragment: "both x and y",
		},
		{
			name: "motion without position or delta",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "mouse_motion"},
			}},
			fragment: "requires x and y, or dx and dy",
		},
		{
			name: "motion with half a position",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "mouse_motion", X: inputFloat(1)},
			}},
			fragment: "both x and y, or dx and dy",
		},
		{
			name: "scroll with a non-wheel button",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "mouse_scroll", Button: "left", X: inputFloat(1), Y: inputFloat(1)},
			}},
			fragment: "wheel_up",
		},
		{
			name: "scroll without coordinates",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "mouse_scroll"},
			}},
			fragment: "unless virtual_cursor is true",
		},
		{
			name: "scroll factor out of range",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "mouse_scroll", X: inputFloat(1), Y: inputFloat(1), Factor: inputFloat(0)},
			}},
			fragment: "factor must be greater than 0",
		},
		{
			name: "magnify without factor",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "gesture_magnify"},
			}},
			fragment: "factor greater than 0",
		},
		{
			name: "pan without delta",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "gesture_pan"},
			}},
			fragment: "requires dx, dy, or both",
		},
		{
			name: "coordinate out of range",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "mouse_motion", X: inputFloat(100001), Y: inputFloat(1)},
			}},
			fragment: "within 100000 pixels",
		},
		{
			name: "non-finite coordinate",
			params: gdruntime.InputParams{Events: []gdruntime.InputEvent{
				{Type: "mouse_motion", X: inputFloat(math.NaN()), Y: inputFloat(1)},
			}},
			fragment: "must be finite",
		},
		{
			name: "negative tick frames",
			params: gdruntime.InputParams{
				Events:     []gdruntime.InputEvent{{Type: "action_press", Action: "jump"}},
				TickFrames: -1,
			},
			fragment: "tick_frames must be between 0 and 600",
		},
		{
			name: "too many tick frames",
			params: gdruntime.InputParams{
				Events:     []gdruntime.InputEvent{{Type: "action_press", Action: "jump"}},
				TickFrames: 601,
			},
			fragment: "tick_frames must be between 0 and 600",
		},
		{
			name: "timeout out of range",
			params: gdruntime.InputParams{
				Events:         []gdruntime.InputEvent{{Type: "action_press", Action: "jump"}},
				TimeoutSeconds: 61,
			},
			fragment: "timeout_seconds must be between 0 and 60",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := gdruntime.Input(root, testCase.params)

			assertRuntimeError(t, err, "invalid_argument")
			assertErrorContains(t, err, testCase.fragment)
		})
	}
}

func repeatActionPress(count int) []gdruntime.InputEvent {
	events := make([]gdruntime.InputEvent, count)

	for index := range events {
		events[index] = gdruntime.InputEvent{Type: "action_press", Action: "jump"}
	}

	return events
}

func TestInputTimesOutWithoutRunner(t *testing.T) {
	root := t.TempDir()
	done, pid := startRuntimeHelper(t)
	session := "20261023T150405Z-a1b2c5"
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

	submittedAt := time.Now()

	_, err := gdruntime.Input(root, gdruntime.InputParams{
		Session: session,
		Events: []gdruntime.InputEvent{
			{Type: "action_press", Action: "jump"},
			{Type: "action_release", Action: "jump"},
		},
		TickFrames:     2,
		TimeoutSeconds: 0.2,
	})

	assertRuntimeError(t, err, "action_failed")
	assertErrorContains(t, err, "timed out after")

	requested, globErr := filepath.Glob(filepath.Join(directory, "requests", "*.json"))
	if globErr != nil {
		t.Fatal(globErr)
	}

	if len(requested) != 1 {
		t.Fatalf("input requests = %v; want one request in the session file channel", requested)
	}

	data, readErr := os.ReadFile(requested[0])
	if readErr != nil {
		t.Fatal(readErr)
	}

	var request struct {
		Op              string `json:"op"`
		TickFrames      int    `json:"tick_frames"`
		ExpiresAtUnixMS int64  `json:"expires_at_unix_ms"`
		BudgetMS        int64  `json:"budget_ms"`
		Events          []struct {
			Type string `json:"type"`
		} `json:"events"`
	}

	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatalf("decoding input request: %v", err)
	}

	if request.Op != "input" || request.TickFrames != 2 {
		t.Fatalf("input request = %+v; want an input request with tick frames", request)
	}

	if len(request.Events) != 2 || request.Events[0].Type != "action_press" ||
		request.Events[1].Type != "action_release" {
		t.Fatalf("input request events = %+v; want the ordered sequence", request.Events)
	}

	if request.BudgetMS <= 0 {
		t.Fatalf("input request budget = %d; want a positive active budget", request.BudgetMS)
	}

	// The absolute deadline is measured from submission, not from the moment
	// the runner dequeues the command.
	if request.ExpiresAtUnixMS <= submittedAt.UnixMilli() {
		t.Fatalf(
			"input request expiry = %d; want a deadline after submission at %d",
			request.ExpiresAtUnixMS,
			submittedAt.UnixMilli(),
		)
	}

	assertRuntimeHelperAlive(t, done)
}

func TestQueuedInputExpiresWithoutLateDelivery(t *testing.T) {
	executable := requireGodot(t)

	root := newInputProject(t)
	registerEditor(t, root, executable)
	start := startHeadlessRuntime(t, root)

	t.Cleanup(func() {
		_, _ = gdruntime.Stop(root, gdruntime.StopParams{
			Session: start.Session,
			Force:   true,
		})
	})

	if _, err := gdruntime.Wait(root, gdruntime.WaitParams{
		Session:        start.Session,
		Frames:         2,
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("settling the fixture: %v", err)
	}

	baseline := readInputObservations(t, root)

	// Hold the runtime busy long enough that the queued input expires while it
	// waits. The input timeout is measured from submission, not from dequeue.
	waitDone := make(chan error, 1)

	go func() {
		_, err := gdruntime.Wait(root, gdruntime.WaitParams{
			Session:        start.Session,
			Seconds:        1,
			TimeoutSeconds: 10,
		})
		waitDone <- err
	}()

	time.Sleep(200 * time.Millisecond)

	_, err := gdruntime.Input(root, gdruntime.InputParams{
		Session:       start.Session,
		VirtualCursor: true,
		Events: []gdruntime.InputEvent{
			{Type: "action_press", Action: "jump"},
			{Type: "key_press", Key: "A"},
			{Type: "mouse_motion", X: inputFloat(220), Y: inputFloat(160)},
			{Type: "mouse_button", Button: "left", Pressed: inputBool(true)},
		},
		TimeoutSeconds: 0.15,
	})

	assertRuntimeError(t, err, "action_failed")
	assertErrorContains(t, err, "timed out after")

	select {
	case waitErr := <-waitDone:
		if waitErr != nil {
			t.Fatalf("background wait failed: %v", waitErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("background wait did not finish")
	}

	// Serialize behind the expired request so the runner has reached and
	// rejected it before observations are checked.
	if _, err := gdruntime.Wait(root, gdruntime.WaitParams{
		Session:        start.Session,
		Frames:         5,
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("draining the expired input request: %v", err)
	}

	observations := readInputObservations(t, root)

	assertNoLateInputObservations(t, baseline, observations)

	expiredEvents, expiredDispatched := readExpiredInputResponse(t, root, start.Session)

	if expiredEvents != 4 || expiredDispatched != 0 {
		t.Fatalf(
			"expired input response = %d events/%d dispatched; want 0 of 4 delivered",
			expiredEvents,
			expiredDispatched,
		)
	}
}

func assertNoLateInputObservations(t *testing.T, baseline inputObservations, observations inputObservations) {
	t.Helper()

	if observations.KeyEvents != baseline.KeyEvents ||
		observations.MouseButtonEvents != baseline.MouseButtonEvents ||
		observations.PickPresses != baseline.PickPresses ||
		observations.ActionTrueFrames != baseline.ActionTrueFrames ||
		observations.ActionPressed {
		t.Fatalf(
			"late input side effects: baseline=%+v observations=%+v",
			baseline,
			observations,
		)
	}
}

func readExpiredInputResponse(t *testing.T, root string, session string) (int, int) {
	t.Helper()

	responses, globErr := filepath.Glob(filepath.Join(
		root,
		".godot",
		"godot-mcp",
		"runtime",
		"sessions",
		session,
		"responses",
		"*.json",
	))
	if globErr != nil {
		t.Fatal(globErr)
	}

	for _, path := range responses {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}

		var response struct {
			OK         bool   `json:"ok"`
			Code       string `json:"code"`
			Message    string `json:"message"`
			Events     int    `json:"events"`
			Dispatched int    `json:"dispatched"`
		}

		if err := json.Unmarshal(data, &response); err != nil {
			t.Fatalf("decoding response %s: %v", filepath.Base(path), err)
		}

		if response.OK || !strings.Contains(response.Message, "expired before dispatch") {
			continue
		}

		if response.Code != "action_failed" {
			t.Fatalf("expired input response code = %q; want action_failed", response.Code)
		}

		return response.Events, response.Dispatched
	}

	t.Fatal("the queued input request was not rejected as expired")

	return 0, 0
}

func TestHeadlessInputPollingAndDelivery(t *testing.T) {
	executable := requireGodot(t)

	root := newInputProject(t)
	registerEditor(t, root, executable)
	start := startHeadlessRuntime(t, root)

	t.Cleanup(func() {
		_, _ = gdruntime.Stop(root, gdruntime.StopParams{
			Session: start.Session,
			Force:   true,
		})
	})

	result, err := gdruntime.Input(root, gdruntime.InputParams{
		Session: start.Session,
		Events: []gdruntime.InputEvent{
			{Type: "action_press", Action: "jump"},
			{Type: "action_release", Action: "jump"},
			{Type: "key_press", Key: "A", Device: inputInt(3)},
			{Type: "key_release", Key: "A", Device: inputInt(3)},
		},
		TickFrames:     3,
		TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatalf("dispatching input: %v", err)
	}

	if result.Events != 4 || result.Dispatched != 4 || len(result.Reports) != 4 {
		t.Fatalf("input result = %+v; want four dispatched reports", result)
	}

	if result.TickFrames != 3 || result.FramesElapsed < 9 {
		t.Fatalf("input result = %+v; want tick separation to advance frames", result)
	}

	assertInputReport(t, result.Reports[0], "action_press", "polling", true, false)
	assertInputReport(t, result.Reports[1], "action_release", "polling", false, false)
	assertInputReport(t, result.Reports[2], "key_press", "parse_input_event", true, true)
	assertInputReport(t, result.Reports[3], "key_release", "parse_input_event", false, true)

	if result.Reports[2].Device == nil || *result.Reports[2].Device != 3 {
		t.Fatalf("key report device = %v; want native device 3", result.Reports[2].Device)
	}

	if result.Held == nil ||
		len(result.Held.Actions) != 0 ||
		len(result.Held.Keys) != 0 ||
		len(result.Held.MouseButtons) != 0 {
		t.Fatalf("held inputs = %+v; want everything released", result.Held)
	}

	if _, err := gdruntime.Wait(root, gdruntime.WaitParams{
		Session:        start.Session,
		Frames:         2,
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("waiting for the fixture to observe polling: %v", err)
	}

	observations := readInputObservations(t, root)

	if observations.ActionTrueFrames < 2 {
		t.Fatalf("action polling frames = %d; want the fixture to observe the held action", observations.ActionTrueFrames)
	}

	if observations.ActionPressEvents != 0 {
		t.Fatalf("action events = %d; action_press must not emit InputEvent callbacks", observations.ActionPressEvents)
	}

	if observations.KeyEvents < 2 || observations.KeyReleaseEvents < 1 {
		t.Fatalf("key callbacks = %+v; want press and release delivery", observations)
	}

	if observations.ActionPressed {
		t.Fatal("fixture still reports the action as pressed after release")
	}
}

func assertInputReport(
	t *testing.T,
	report gdruntime.InputEventReport,
	kind string,
	delivery string,
	polled bool,
	callback bool,
) {
	t.Helper()

	if report.Type != kind || report.Delivery != delivery {
		t.Fatalf("report = %+v; want %s delivered via %s", report, kind, delivery)
	}

	if report.Polled != polled || report.Callback != callback {
		t.Fatalf(
			"report = %+v; want polled=%v and callback=%v",
			report,
			polled,
			callback,
		)
	}
}

func TestHeadlessVirtualCursorPicking(t *testing.T) {
	executable := requireGodot(t)

	root := newInputProject(t)
	registerEditor(t, root, executable)
	start := startHeadlessRuntime(t, root)

	t.Cleanup(func() {
		_, _ = gdruntime.Stop(root, gdruntime.StopParams{
			Session: start.Session,
			Force:   true,
		})
	})

	result, err := gdruntime.Input(root, gdruntime.InputParams{
		Session:       start.Session,
		VirtualCursor: true,
		Events: []gdruntime.InputEvent{
			{Type: "mouse_motion", X: inputFloat(220), Y: inputFloat(160)},
			{Type: "mouse_button", Button: "left", Pressed: inputBool(true)},
			{Type: "mouse_motion", DX: inputFloat(20), DY: inputFloat(0)},
			{Type: "mouse_button", Button: "left", Pressed: inputBool(false)},
			{Type: "mouse_scroll", Button: "wheel_up", Factor: inputFloat(2)},
			{Type: "gesture_magnify", Factor: inputFloat(1.25)},
			{Type: "gesture_pan", DX: inputFloat(10), DY: inputFloat(-5)},
		},
		TickFrames:     2,
		TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatalf("dispatching virtual cursor input: %v", err)
	}

	if len(result.Reports) != 7 {
		t.Fatalf("reports = %d; want 7", len(result.Reports))
	}

	if !result.VirtualCursor || result.Cursor == nil {
		t.Fatalf("input result = %+v; want a virtual cursor", result)
	}

	if result.Cursor.X != 240 || result.Cursor.Y != 160 {
		t.Fatalf("cursor = %+v; want the maintained position (240, 160)", result.Cursor)
	}

	assertPicked(t, result.Reports[0], 220, 160)
	assertPicked(t, result.Reports[1], 220, 160)
	assertPicked(t, result.Reports[2], 240, 160)
	assertPicked(t, result.Reports[3], 240, 160)
	assertPicked(t, result.Reports[4], 240, 160)

	assertInputReport(t, result.Reports[1], "mouse_button", "parse_input_event", true, true)
	assertInputReport(t, result.Reports[3], "mouse_button", "parse_input_event", false, true)
	assertInputReport(t, result.Reports[4], "mouse_scroll", "parse_input_event", false, true)

	if !result.Reports[5].Callback || !result.Reports[6].Callback {
		t.Fatalf("gesture reports = %+v; want engine-delivered gesture callbacks", result.Reports[5:])
	}

	if result.Held == nil || len(result.Held.MouseButtons) != 0 {
		t.Fatalf("held mouse buttons = %+v; want the button released", result.Held)
	}

	observations := readInputObservations(t, root)
	if observations.GestureEvents < 2 {
		t.Fatalf("gesture callbacks = %d; want synthetic gesture delivery", observations.GestureEvents)
	}
}

func assertPicked(t *testing.T, report gdruntime.InputEventReport, x float64, y float64) {
	t.Helper()

	if report.Position == nil || report.Position.X != x || report.Position.Y != y {
		t.Fatalf("report position = %+v; want (%v, %v)", report.Position, x, y)
	}

	if !report.PickChecked {
		t.Fatalf("report = %+v; want a physics picking check", report)
	}

	found := false

	for _, pick := range report.Picks {
		if pick == "PickBody" {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("report picks = %v; want the fixture body at (%v, %v)", report.Picks, x, y)
	}
}

func TestHeadlessInputRejectsUndefinedAction(t *testing.T) {
	executable := requireGodot(t)

	root := newInputProject(t)
	registerEditor(t, root, executable)
	start := startHeadlessRuntime(t, root)

	t.Cleanup(func() {
		_, _ = gdruntime.Stop(root, gdruntime.StopParams{
			Session: start.Session,
			Force:   true,
		})
	})

	_, err := gdruntime.Input(root, gdruntime.InputParams{
		Session: start.Session,
		Events: []gdruntime.InputEvent{
			{Type: "action_press", Action: "definitely_not_an_action"},
		},
		TimeoutSeconds: 10,
	})

	assertRuntimeError(t, err, "invalid_argument")
	assertErrorContains(t, err, "not defined")
	assertErrorContains(t, err, "definitely_not_an_action")
}

func TestHeadlessVirtualCursorPersistsAcrossCalls(t *testing.T) {
	executable := requireGodot(t)

	root := newInputProject(t)
	registerEditor(t, root, executable)
	start := startHeadlessRuntime(t, root)

	t.Cleanup(func() {
		_, _ = gdruntime.Stop(root, gdruntime.StopParams{
			Session: start.Session,
			Force:   true,
		})
	})

	first, err := gdruntime.Input(root, gdruntime.InputParams{
		Session:       start.Session,
		VirtualCursor: true,
		Events: []gdruntime.InputEvent{
			{Type: "mouse_motion", X: inputFloat(220), Y: inputFloat(160)},
		},
		TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatalf("positioning the virtual cursor: %v", err)
	}

	if first.Cursor == nil || first.Cursor.X != 220 || first.Cursor.Y != 160 {
		t.Fatalf("cursor = %+v; want (220, 160)", first.Cursor)
	}

	second, err := gdruntime.Input(root, gdruntime.InputParams{
		Session:       start.Session,
		VirtualCursor: true,
		Events: []gdruntime.InputEvent{
			{Type: "mouse_button", Button: "left", Pressed: inputBool(true)},
			{Type: "mouse_button", Button: "left", Pressed: inputBool(false)},
		},
		TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatalf("clicking at the maintained cursor: %v", err)
	}

	if len(second.Reports) != 2 {
		t.Fatalf("reports = %d; want 2", len(second.Reports))
	}

	assertPicked(t, second.Reports[0], 220, 160)
	assertPicked(t, second.Reports[1], 220, 160)

	if second.Held == nil || len(second.Held.MouseButtons) != 0 {
		t.Fatalf("held mouse buttons = %+v; want the button released", second.Held)
	}
}

func TestInputSchemaMatchesValidation(t *testing.T) {
	reflector := jsonschema.Reflector{Anonymous: true, ExpandedStruct: true}
	schema := reflector.ReflectFromType(reflect.TypeFor[gdruntime.InputParams]())

	if schema == nil || schema.Properties == nil {
		t.Fatal("input schema has no properties")
	}

	property := func(name string) *jsonschema.Schema {
		t.Helper()

		item, found := schema.Properties.Get(name)
		if !found || item == nil {
			t.Fatalf("input schema is missing %q", name)
		}

		return item
	}

	events := property("events")

	if events.MinItems == nil || *events.MinItems != 1 ||
		events.MaxItems == nil || *events.MaxItems != 32 {
		t.Fatalf("events limits = %v/%v; want 1/32", events.MinItems, events.MaxItems)
	}

	itemSchema := events.Items

	if itemSchema == nil {
		t.Fatal("events items have no schema")
	}

	if itemSchema.Ref != "" {
		name := strings.TrimPrefix(itemSchema.Ref, "#/$defs/")
		itemSchema = schema.Definitions[name]

		if itemSchema == nil {
			t.Fatalf("events item reference %q did not resolve", events.Items.Ref)
		}
	}

	if itemSchema.Properties == nil {
		t.Fatal("events items have no schema properties")
	}

	eventProperty := func(name string) *jsonschema.Schema {
		t.Helper()

		item, found := itemSchema.Properties.Get(name)
		if !found || item == nil {
			t.Fatalf("input event schema is missing %q", name)
		}

		return item
	}

	assertEnum(t, eventProperty("type"), []string{
		"action_press",
		"action_release",
		"key_press",
		"key_release",
		"mouse_button",
		"mouse_motion",
		"mouse_scroll",
		"gesture_magnify",
		"gesture_pan",
	})

	assertEnum(t, eventProperty("button"), []string{
		"left",
		"right",
		"middle",
		"wheel_up",
		"wheel_down",
		"wheel_left",
		"wheel_right",
		"xbutton1",
		"xbutton2",
	})

	assertSchemaNumber(t, "strength minimum", eventProperty("strength").Minimum, "0")
	assertSchemaNumber(t, "strength maximum", eventProperty("strength").Maximum, "1")
	assertSchemaNumber(t, "x minimum", eventProperty("x").Minimum, "-100000")
	assertSchemaNumber(t, "x maximum", eventProperty("x").Maximum, "100000")
	assertSchemaNumber(t, "y minimum", eventProperty("y").Minimum, "-100000")
	assertSchemaNumber(t, "y maximum", eventProperty("y").Maximum, "100000")
	assertSchemaNumber(t, "dx minimum", eventProperty("dx").Minimum, "-100000")
	assertSchemaNumber(t, "dx maximum", eventProperty("dx").Maximum, "100000")
	assertSchemaNumber(t, "dy minimum", eventProperty("dy").Minimum, "-100000")
	assertSchemaNumber(t, "dy maximum", eventProperty("dy").Maximum, "100000")
	assertSchemaNumber(t, "factor exclusive minimum", eventProperty("factor").ExclusiveMinimum, "0")

	assertSchemaNumber(t, "tick_frames minimum", property("tick_frames").Minimum, "0")
	assertSchemaNumber(t, "tick_frames maximum", property("tick_frames").Maximum, "600")
	assertSchemaNumber(t, "timeout_seconds minimum", property("timeout_seconds").Minimum, "0")
	assertSchemaNumber(t, "timeout_seconds maximum", property("timeout_seconds").Maximum, "60")

	if description := property("virtual_cursor").Description; !strings.Contains(description, "diagnostic physics picking query") {
		t.Fatalf("virtual_cursor description = %q; want the diagnostic query wording", description)
	}
}

func assertEnum(t *testing.T, schema *jsonschema.Schema, required []string) {
	t.Helper()

	values := make(map[string]bool, len(schema.Enum))

	for _, value := range schema.Enum {
		if text, ok := value.(string); ok {
			values[text] = true
		}
	}

	for _, value := range required {
		if !values[value] {
			t.Fatalf("enum = %v; missing %q", schema.Enum, value)
		}
	}
}

func assertSchemaNumber(t *testing.T, name string, value json.Number, want string) {
	t.Helper()

	if value.String() != want {
		t.Fatalf("%s = %q; want %q", name, value.String(), want)
	}
}

func TestInputReleasesHeldInputsOnErrorAndStop(t *testing.T) {
	executable := requireGodot(t)

	root := newInputProject(t)
	registerEditor(t, root, executable)
	start := startHeadlessRuntime(t, root)

	t.Cleanup(func() {
		_, _ = gdruntime.Stop(root, gdruntime.StopParams{
			Session: start.Session,
			Force:   true,
		})
	})

	held, err := gdruntime.Input(root, gdruntime.InputParams{
		Session:       start.Session,
		VirtualCursor: true,
		Events: []gdruntime.InputEvent{
			{Type: "mouse_motion", X: inputFloat(220), Y: inputFloat(160)},
			{Type: "mouse_button", Button: "left", Pressed: inputBool(true)},
			{Type: "key_press", Key: "A"},
			{Type: "action_press", Action: "jump"},
		},
		TickFrames:     1,
		TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatalf("holding inputs: %v", err)
	}

	if held.Held == nil ||
		len(held.Held.Actions) != 1 ||
		len(held.Held.Keys) != 1 ||
		len(held.Held.MouseButtons) != 1 {
		t.Fatalf("held inputs = %+v; want the action, key, and mouse button held", held.Held)
	}

	_, err = gdruntime.Input(root, gdruntime.InputParams{
		Session: start.Session,
		Events: []gdruntime.InputEvent{
			{Type: "key_press", Key: "DefinitelyNotAKey"},
		},
		TimeoutSeconds: 10,
	})

	assertRuntimeError(t, err, "invalid_argument")
	assertErrorContains(t, err, "event 0")
	assertErrorContains(t, err, "valid key")

	if _, err := gdruntime.Wait(root, gdruntime.WaitParams{
		Session:        start.Session,
		Frames:         4,
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("waiting after the failed input request: %v", err)
	}

	observations := readInputObservations(t, root)

	if observations.ActionPressed {
		t.Fatal("failed input request left the action held")
	}

	if observations.KeyReleaseEvents == 0 || observations.MouseReleaseEvents == 0 {
		t.Fatalf("release callbacks = %+v; want held key and mouse button released", observations)
	}

	// Hold inputs again and stop the runtime: the runner must release them
	// before writing its exit report.
	if _, err := gdruntime.Input(root, gdruntime.InputParams{
		Session:       start.Session,
		VirtualCursor: true,
		Events: []gdruntime.InputEvent{
			{Type: "mouse_motion", X: inputFloat(220), Y: inputFloat(160)},
			{Type: "mouse_button", Button: "left", Pressed: inputBool(true)},
			{Type: "key_press", Key: "B"},
			{Type: "action_press", Action: "jump"},
		},
		TickFrames:     1,
		TimeoutSeconds: 10,
	}); err != nil {
		t.Fatalf("holding inputs before stop: %v", err)
	}

	stop, err := gdruntime.Stop(root, gdruntime.StopParams{
		Session:        start.Session,
		TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatalf("stopping runtime with held inputs: %v", err)
	}

	if !stop.Stopped || stop.State != gdruntime.StateExited {
		t.Fatalf("stop = %+v; want the runtime to exit", stop)
	}

	released := readReleasedHeldInputs(t, root, start.Session)

	if released.Actions < 1 || released.Keys < 1 || released.MouseButtons < 1 {
		t.Fatalf("released held inputs = %+v; want all held inputs released on stop", released)
	}
}

func TestGraphicalInputButtonClickAndPicking(t *testing.T) {
	if os.Getenv("GODOT_MODE") != "graphical" {
		t.Skip("set GODOT_MODE=graphical to exercise GUI clicks and engine physics picking")
	}

	executable := requireGodot(t)

	root := newInputProject(t)
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

	result, err := gdruntime.Input(root, gdruntime.InputParams{
		Session:       start.Session,
		VirtualCursor: true,
		Events: []gdruntime.InputEvent{
			{Type: "mouse_motion", X: inputFloat(80), Y: inputFloat(50)},
			{Type: "mouse_button", Button: "left", Pressed: inputBool(true)},
			{Type: "mouse_button", Button: "left", Pressed: inputBool(false)},
			{Type: "mouse_motion", X: inputFloat(220), Y: inputFloat(160)},
			{Type: "mouse_button", Button: "left", Pressed: inputBool(true)},
			{Type: "mouse_motion", X: inputFloat(245), Y: inputFloat(160)},
			{Type: "mouse_button", Button: "left", Pressed: inputBool(false)},
		},
		TickFrames:     2,
		TimeoutSeconds: 15,
	})
	if err != nil {
		t.Fatalf("dispatching graphical input: %v", err)
	}

	if result.Dispatched != 7 {
		t.Fatalf("dispatched = %d; want 7", result.Dispatched)
	}

	for _, report := range result.Reports {
		if !report.Callback {
			t.Fatalf("report = %+v; windowed input should reach engine callbacks", report)
		}
	}

	if _, err := gdruntime.Wait(root, gdruntime.WaitParams{
		Session:        start.Session,
		Frames:         3,
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("waiting for the fixture to process input: %v", err)
	}

	observations := readInputObservations(t, root)

	if observations.ButtonPresses < 1 {
		t.Fatalf("button presses = %d; want a real Button press", observations.ButtonPresses)
	}

	if observations.PickPresses < 1 || observations.PickMotions < 1 || observations.PickReleases < 1 {
		t.Fatalf("physics picking = %+v; want press, drag, and release", observations)
	}

	if len(observations.LastPickPosition) != 2 ||
		observations.LastPickPosition[0] != 245 ||
		observations.LastPickPosition[1] != 160 {
		t.Fatalf(
			"last pick position = %v; want the dragged position (245, 160)",
			observations.LastPickPosition,
		)
	}
}

func TestInputRunnerDeadlineReleasesHeldInputs(t *testing.T) {
	executable := requireGodot(t)

	root := newInputProject(t)
	registerEditor(t, root, executable)
	start := startHeadlessRuntime(t, root)

	t.Cleanup(func() {
		_, _ = gdruntime.Stop(root, gdruntime.StopParams{
			Session: start.Session,
			Force:   true,
		})
	})

	if _, err := gdruntime.Input(root, gdruntime.InputParams{
		Session:       start.Session,
		VirtualCursor: true,
		Events: []gdruntime.InputEvent{
			{Type: "mouse_motion", X: inputFloat(220), Y: inputFloat(160)},
			{Type: "mouse_button", Button: "left", Pressed: inputBool(true)},
			{Type: "key_press", Key: "A"},
			{Type: "action_press", Action: "jump"},
		},
		TickFrames:     1,
		TimeoutSeconds: 10,
	}); err != nil {
		t.Fatalf("holding inputs: %v", err)
	}

	// The host deadline is 200ms while the runner deadline is half of that, so
	// a sequence that would take hundreds of frames must fail runner-side and
	// release the inputs before the caller times out.
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
		TickFrames:     600,
		TimeoutSeconds: 0.2,
	})

	assertRuntimeError(t, err, "action_failed")
	assertErrorContains(t, err, "deadline")
	assertErrorContains(t, err, "events were delivered before the failure")

	if _, err := gdruntime.Wait(root, gdruntime.WaitParams{
		Session:        start.Session,
		Frames:         3,
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("waiting after the runner deadline: %v", err)
	}

	observations := readInputObservations(t, root)

	if observations.ActionPressed {
		t.Fatal("runner deadline left the action held")
	}

	if observations.KeyReleaseEvents == 0 || observations.MouseReleaseEvents == 0 {
		t.Fatalf("release callbacks = %+v; want the runner deadline to release held inputs", observations)
	}
}

type inputObservations struct {
	ButtonPresses      int       `json:"button_presses"`
	PickPresses        int       `json:"pick_presses"`
	PickReleases       int       `json:"pick_releases"`
	PickMotions        int       `json:"pick_motions"`
	ActionPressed      bool      `json:"action_pressed"`
	ActionTrueFrames   int       `json:"action_true_frames"`
	ActionPressEvents  int       `json:"action_press_events"`
	KeyEvents          int       `json:"key_events"`
	KeyReleaseEvents   int       `json:"key_release_events"`
	MouseButtonEvents  int       `json:"mouse_button_events"`
	MouseReleaseEvents int       `json:"mouse_release_events"`
	GestureEvents      int       `json:"gesture_events"`
	LastPickPosition   []float64 `json:"last_pick_position"`
}

type releasedHeldInputs struct {
	Actions      int `json:"actions"`
	Keys         int `json:"keys"`
	MouseButtons int `json:"mouse_buttons"`
}

func readInputObservations(t *testing.T, root string) inputObservations {
	t.Helper()

	path := filepath.Join(root, "input_probe.json")
	deadline := time.Now().Add(5 * time.Second)

	for {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			var observations inputObservations

			if jsonErr := json.Unmarshal(data, &observations); jsonErr == nil {
				return observations
			}
		}

		if time.Now().After(deadline) {
			t.Fatalf("reading fixture observations from %s failed", path)
		}

		time.Sleep(25 * time.Millisecond)
	}
}

func readReleasedHeldInputs(t *testing.T, root string, session string) releasedHeldInputs {
	t.Helper()

	path := filepath.Join(
		root,
		".godot",
		"godot-mcp",
		"runtime",
		"sessions",
		session,
		"exit.json",
	)

	var report struct {
		Released releasedHeldInputs `json:"released_held_inputs"`
	}

	deadline := time.Now().Add(5 * time.Second)

	for {
		data, err := os.ReadFile(path)
		if err == nil {
			if jsonErr := json.Unmarshal(data, &report); jsonErr == nil {
				return report.Released
			}
		}

		if time.Now().After(deadline) {
			t.Fatalf("reading exit report from %s failed", path)
		}

		time.Sleep(25 * time.Millisecond)
	}
}

func newInputProject(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

	project := `config_version=5

[application]
config/name="godot-mcp input test"
run/main_scene="res://main.tscn"

[display]
window/size/viewport_width=640
window/size/viewport_height=480

[input]

jump={
"deadzone": 0.5,
"events": [Object(InputEventKey,"resource_local_to_scene":false,"resource_name":"","device":-1,"window_id":0,"alt_pressed":false,"shift_pressed":false,"ctrl_pressed":false,"meta_pressed":false,"pressed":false,"keycode":32,"physical_keycode":32,"key_label":32,"unicode":32,"location":0,"echo":false,"script":null)
]
}

[rendering]
renderer/rendering_method="gl_compatibility"
renderer/rendering_method.mobile="gl_compatibility"
`

	if err := os.WriteFile(filepath.Join(root, "project.godot"), []byte(project), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "main.gd"),
		[]byte(inputFixtureScript),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	scene := `[gd_scene load_steps=2 format=3]

[ext_resource type="Script" path="res://main.gd" id="1"]

[node name="Main" type="Node2D"]
script = ExtResource("1")
`

	if err := os.WriteFile(filepath.Join(root, "main.tscn"), []byte(scene), 0644); err != nil {
		t.Fatal(err)
	}

	return root
}

const inputFixtureScript = `extends Node2D

var observations := {
	"button_presses": 0,
	"pick_presses": 0,
	"pick_releases": 0,
	"pick_motions": 0,
	"last_pick_position": [],
	"action_pressed": false,
	"action_true_frames": 0,
	"action_press_events": 0,
	"key_events": 0,
	"key_release_events": 0,
	"mouse_button_events": 0,
	"mouse_release_events": 0,
	"gesture_events": 0,
	"frames": 0,
}

func _ready() -> void:
	get_viewport().physics_object_picking = true
	get_viewport().notify_mouse_entered()

	var button := Button.new()
	button.name = "ClickButton"
	button.text = "Click"
	button.position = Vector2(20, 20)
	button.size = Vector2(120, 60)
	button.pressed.connect(_on_button_pressed)
	add_child(button)

	var body := StaticBody2D.new()
	body.name = "PickBody"
	body.position = Vector2(220, 160)
	body.input_pickable = true

	var shape := CollisionShape2D.new()
	var rectangle := RectangleShape2D.new()
	rectangle.size = Vector2(100, 100)
	shape.shape = rectangle
	body.add_child(shape)
	body.input_event.connect(_on_pick_event)
	add_child(body)

	_flush()

func _input(event: InputEvent) -> void:
	if event is InputEventAction:
		observations["action_press_events"] += 1
	elif event is InputEventKey:
		observations["key_events"] += 1

		if not event.pressed:
			observations["key_release_events"] += 1
	elif event is InputEventMouseButton:
		observations["mouse_button_events"] += 1

		if not event.pressed:
			observations["mouse_release_events"] += 1
	elif event is InputEventMagnifyGesture or event is InputEventPanGesture:
		observations["gesture_events"] += 1

	_flush()

func _process(_delta: float) -> void:
	observations["frames"] += 1
	observations["action_pressed"] = Input.is_action_pressed("jump")

	if observations["action_pressed"]:
		observations["action_true_frames"] += 1

	_flush()

func _on_button_pressed() -> void:
	observations["button_presses"] += 1
	_flush()

func _on_pick_event(_viewport: Node, event: InputEvent, _shape_index: int) -> void:
	if event is InputEventMouseButton:
		if event.pressed:
			observations["pick_presses"] += 1
		else:
			observations["pick_releases"] += 1

		observations["last_pick_position"] = [event.position.x, event.position.y]
	elif event is InputEventMouseMotion:
		observations["pick_motions"] += 1
		observations["last_pick_position"] = [event.position.x, event.position.y]

	_flush()

func _flush() -> void:
	var file := FileAccess.open("res://input_probe.json", FileAccess.WRITE)

	if file == null:
		return

	file.store_string(JSON.stringify(observations))
	file.close()
`

func TestInputRequiresActiveRuntime(t *testing.T) {
	root := newInputProject(t)

	_, err := gdruntime.Input(root, gdruntime.InputParams{
		Events: []gdruntime.InputEvent{
			{Type: "action_press", Action: "jump"},
		},
	})

	assertRuntimeError(t, err, "failed_precondition")
	assertErrorContains(t, err, "no runtime session is active")
}
