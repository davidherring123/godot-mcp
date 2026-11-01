package runtime

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/davidherring123/godot-mcp/internal/protocol/apierr"
)

const (
	inputActionPress    = "action_press"
	inputActionRelease  = "action_release"
	inputKeyPress       = "key_press"
	inputKeyRelease     = "key_release"
	inputMouseButton    = "mouse_button"
	inputMouseMotion    = "mouse_motion"
	inputMouseScroll    = "mouse_scroll"
	inputGestureMagnify = "gesture_magnify"
	inputGesturePan     = "gesture_pan"

	maxInputEvents      = 32
	maxInputActionRunes = 128
	maxInputKeyRunes    = 64
	maxInputCoordinate  = 100000.0
	inputTimeoutBase    = 5 * time.Second
)

// InputPoint is a viewport-space coordinate used by synthetic input events.
type InputPoint struct {
	X float64 `json:"x" jsonschema:"description=Horizontal viewport coordinate in pixels."`
	Y float64 `json:"y" jsonschema:"description=Vertical viewport coordinate in pixels."`
}

// InputEvent is one synthetic input event. Only the fields used by Type are
// read; press and release are always explicit.
type InputEvent struct {
	Type     string   `json:"type" jsonschema:"enum=action_press,enum=action_release,enum=key_press,enum=key_release,enum=mouse_button,enum=mouse_motion,enum=mouse_scroll,enum=gesture_magnify,enum=gesture_pan" jsonschema_description:"Event type: action_press, action_release, key_press, key_release, mouse_button, mouse_motion, mouse_scroll, gesture_magnify, or gesture_pan."`
	Action   string   `json:"action,omitempty" jsonschema:"maxLength=128" jsonschema_description:"Input Map action name for action_press and action_release."`
	Strength *float64 `json:"strength,omitempty" jsonschema:"minimum=0,maximum=1" jsonschema_description:"Action strength for action_press (0-1; defaults to 1)."`
	Key      string   `json:"key,omitempty" jsonschema:"maxLength=64" jsonschema_description:"Key name such as Space or A for key_press and key_release, resolved by the runtime."`
	Keycode  int      `json:"keycode,omitempty" jsonschema_description:"Numeric Godot keycode for key_press and key_release; use key or keycode, not both."`
	Button   string   `json:"button,omitempty" jsonschema:"enum=left,enum=right,enum=middle,enum=wheel_up,enum=wheel_down,enum=wheel_left,enum=wheel_right,enum=xbutton1,enum=xbutton2,enum=" jsonschema_description:"Mouse button name: left, right, middle, wheel_up, wheel_down, wheel_left, wheel_right, xbutton1, or xbutton2."`
	Pressed  *bool    `json:"pressed,omitempty" jsonschema_description:"Required for mouse_button; true presses and false releases."`
	X        *float64 `json:"x,omitempty" jsonschema:"minimum=-100000,maximum=100000" jsonschema_description:"Horizontal viewport coordinate. Required for mouse events unless virtual_cursor is enabled."`
	Y        *float64 `json:"y,omitempty" jsonschema:"minimum=-100000,maximum=100000" jsonschema_description:"Vertical viewport coordinate. Required for mouse events unless virtual_cursor is enabled."`
	DX       *float64 `json:"dx,omitempty" jsonschema:"minimum=-100000,maximum=100000" jsonschema_description:"Horizontal movement for mouse_motion and gesture_pan."`
	DY       *float64 `json:"dy,omitempty" jsonschema:"minimum=-100000,maximum=100000" jsonschema_description:"Vertical movement for mouse_motion and gesture_pan."`
	Factor   *float64 `json:"factor,omitempty" jsonschema:"exclusiveMinimum=0" jsonschema_description:"Magnification factor for gesture_magnify (greater than 0) or scroll factor for mouse_scroll (defaults to 1)."`
	Device   *int     `json:"device,omitempty" jsonschema_description:"Native Godot input device ID; defaults to the engine's default device."`
}

// InputParams selects a runtime and dispatches a short ordered input sequence.
// Events are delivered one per frame, with optional tick_frames of separation.
type InputParams struct {
	Session        string       `json:"session,omitempty" jsonschema_description:"Runtime session ID. Omit to use the active session."`
	Events         []InputEvent `json:"events" jsonschema:"minItems=1,maxItems=32" jsonschema_description:"Ordered input events (1-32). Press and release are explicit; use tick_frames to hold an input across frames."`
	TickFrames     int          `json:"tick_frames,omitempty" jsonschema:"minimum=0,maximum=600" jsonschema_description:"Extra idle frames to wait after each event before dispatching the next (0-600; defaults to 0). Consecutive events otherwise land on consecutive idle frames."`
	VirtualCursor  bool         `json:"virtual_cursor,omitempty" jsonschema_description:"Maintain a synthetic cursor position inside the runner and run a diagnostic physics picking query at mouse positions. Does not move or isolate the operating-system cursor; synthetic events share the game's input queue, so real input can interleave."`
	TimeoutSeconds float64      `json:"timeout_seconds,omitempty" jsonschema:"minimum=0,maximum=60" jsonschema_description:"Maximum seconds to wait for the sequence; defaults to a bounded allowance for the events and tick frames, capped at 60. The runtime stops slightly earlier so partial delivery is reported before this timeout."`
}

// InputEventReport is the runtime's observation of one dispatched event.
type InputEventReport struct {
	Index       int         `json:"index" jsonschema:"description=Zero-based position of the event in the sequence."`
	Type        string      `json:"type" jsonschema:"description=Event type that was dispatched."`
	Delivery    string      `json:"delivery" jsonschema:"description=How the event was delivered: polling for action_* events, or parse_input_event for engine-delivered events."`
	Polled      bool        `json:"polled" jsonschema:"description=Whether Input polling reported the pressed state after delivery. Action events update polling without emitting input callbacks."`
	Callback    bool        `json:"callback" jsonschema:"description=Whether the runtime observed an InputEvent callback for this event. action_press and action_release do not emit callbacks."`
	Device      *int        `json:"device,omitempty" jsonschema:"description=Native device ID when one was supplied."`
	Position    *InputPoint `json:"position,omitempty" jsonschema:"description=Viewport position used by a mouse event."`
	PickChecked bool        `json:"pick_checked,omitempty" jsonschema:"description=Whether the runtime queried 2D physics picking at this position."`
	Picks       []string    `json:"picks,omitempty" jsonschema:"description=Names of 2D physics colliders found at the event position when virtual_cursor was enabled. This is a best-effort direct space-state query, not a guarantee that the engine routes the event to those colliders."`
}

// HeldInputs lists inputs still held after the sequence. Held inputs persist
// across runtime.input calls so a later call can release them, and are released
// on a request error, runtime stop, or game exit.
type HeldInputs struct {
	Actions      []string `json:"actions" jsonschema:"description=Input Map actions currently held by the runtime."`
	Keys         []int    `json:"keys" jsonschema:"description=Keycodes currently held by the runtime."`
	MouseButtons []string `json:"mouse_buttons" jsonschema:"description=Mouse button names currently held by the runtime."`
}

// InputResult reports the dispatched sequence and the runtime's observations of
// delivery and polling.
type InputResult struct {
	Session       string             `json:"session" jsonschema:"description=Runtime session ID."`
	Events        int                `json:"events" jsonschema:"description=Number of events requested."`
	Dispatched    int                `json:"dispatched" jsonschema:"description=Number of events delivered before the sequence completed."`
	TickFrames    int                `json:"tick_frames" jsonschema:"description=Idle frames inserted after each event."`
	Frames        int                `json:"frames" jsonschema:"description=Idle frame counter when the sequence completed."`
	StartFrames   int                `json:"start_frames" jsonschema:"description=Idle frame counter when the sequence started."`
	FramesElapsed int                `json:"frames_elapsed" jsonschema:"description=Idle frames that elapsed during the sequence."`
	ElapsedMS     int64              `json:"elapsed_ms" jsonschema:"description=Monotonic milliseconds elapsed during the sequence."`
	UptimeMS      int64              `json:"uptime_ms" jsonschema:"description=Runtime uptime reported by the runner after the sequence."`
	VirtualCursor bool               `json:"virtual_cursor" jsonschema:"description=Whether the synthetic virtual cursor mode was enabled."`
	Cursor        *InputPoint        `json:"cursor,omitempty" jsonschema:"description=Last synthetic cursor position when the runtime tracked one."`
	Held          *HeldInputs        `json:"held" jsonschema:"description=Inputs still held after the sequence."`
	Reports       []InputEventReport `json:"reports" jsonschema:"description=Per-event delivery and polling observations in dispatch order."`
}

type inputRequest struct {
	Op              string       `json:"op"`
	Events          []InputEvent `json:"events"`
	TickFrames      int          `json:"tick_frames"`
	VirtualCursor   bool         `json:"virtual_cursor"`
	ExpiresAtUnixMS int64        `json:"expires_at_unix_ms,omitempty"`
	BudgetMS        int64        `json:"budget_ms,omitempty"`
}

type inputResponse struct {
	OK            bool               `json:"ok"`
	Code          string             `json:"code"`
	Message       string             `json:"message"`
	Events        int                `json:"events"`
	Dispatched    int                `json:"dispatched"`
	TickFrames    int                `json:"tick_frames"`
	Frames        int                `json:"frames"`
	StartFrames   int                `json:"start_frames"`
	FramesElapsed int                `json:"frames_elapsed"`
	ElapsedMS     int64              `json:"elapsed_ms"`
	UptimeMS      int64              `json:"uptime_ms"`
	VirtualCursor bool               `json:"virtual_cursor"`
	Cursor        *InputPoint        `json:"cursor"`
	Held          *HeldInputs        `json:"held"`
	Reports       []InputEventReport `json:"reports"`
}

// Input dispatches a validated, bounded sequence of synthetic input events to a
// running game. Action events use Input.action_press/action_release, which
// update Input polling without emitting InputEvent callbacks. Key, mouse, and
// gesture events use Input.parse_input_event, which delivers authentic engine
// input events to the game. Events are separated by at least one idle frame.
// Each request carries an absolute submission deadline and a monotonic active
// budget, so a command queued behind another runtime request expires instead of
// dispatching late. The runner stops before the host timeout, which keeps the
// partial-delivery report from racing the caller's own deadline.
func Input(projectRoot string, params InputParams) (InputResult, error) {
	events, err := normalizeInputEvents(params.Events, params.VirtualCursor)
	if err != nil {
		return InputResult{}, err
	}

	if params.TickFrames < 0 || params.TickFrames > maxWaitFrames {
		return InputResult{}, newError(
			string(apierr.CodeInvalidArgument),
			"tick_frames must be between 0 and %d",
			maxWaitFrames,
		)
	}

	totalFrames := len(events) + params.TickFrames*(len(events)-1)

	timeout, err := resolveWaitTimeout(
		params.TimeoutSeconds,
		inputTimeoutBase,
		totalFrames,
		0,
	)
	if err != nil {
		return InputResult{}, err
	}

	info, err := requireRunningSession(projectRoot, params.Session)
	if err != nil {
		return InputResult{}, err
	}

	if err := requireRunnerProtocol(info, "deadline-aware input"); err != nil {
		return InputResult{}, err
	}

	// Fix the absolute expiry at submission. Queue time counts against it, and
	// the runner stops earlier than the host's deadline so its partial-delivery
	// report is not lost to the host timeout.
	deadline := newRunnerDeadline(time.Now(), timeout)

	identifier, err := submitRunnerRequest(info, inputRequest{
		Op:              "input",
		Events:          events,
		TickFrames:      params.TickFrames,
		VirtualCursor:   params.VirtualCursor,
		ExpiresAtUnixMS: deadline.expiresAtUnixMS(),
		BudgetMS:        deadline.activeBudgetMS(),
	}, "input")
	if err != nil {
		return InputResult{}, err
	}

	response, err := awaitRunnerDeadline[inputResponse](
		deadline,
		info,
		identifier,
		"input sequence",
	)
	if err != nil {
		return InputResult{}, err
	}

	if !response.OK {
		return InputResult{}, inputError(response)
	}

	held := response.Held
	if held == nil {
		held = &HeldInputs{}
	}

	if held.Actions == nil {
		held.Actions = []string{}
	}

	if held.Keys == nil {
		held.Keys = []int{}
	}

	if held.MouseButtons == nil {
		held.MouseButtons = []string{}
	}

	reports := response.Reports
	if reports == nil {
		reports = []InputEventReport{}
	}

	return InputResult{
		Session:       info.metadata.Session,
		Events:        response.Events,
		Dispatched:    response.Dispatched,
		TickFrames:    response.TickFrames,
		Frames:        response.Frames,
		StartFrames:   response.StartFrames,
		FramesElapsed: response.FramesElapsed,
		ElapsedMS:     response.ElapsedMS,
		UptimeMS:      response.UptimeMS,
		VirtualCursor: response.VirtualCursor,
		Cursor:        response.Cursor,
		Held:          held,
		Reports:       reports,
	}, nil
}

func inputError(response inputResponse) error {
	code := response.Code

	if code == "" {
		code = string(apierr.CodeActionFailed)
	}

	message := response.Message

	if message == "" {
		message = "the runtime could not complete the input sequence"
	}

	if response.Dispatched > 0 {
		// The runtime reports how many events produced side effects before it
		// failed. Keep this in the existing error text rather than adding a new
		// error protocol.
		message = fmt.Sprintf(
			"%s; %d of %d events were delivered before the failure",
			message,
			response.Dispatched,
			response.Events,
		)
	}

	return newError(code, "%s", message)
}

func normalizeInputEvents(events []InputEvent, virtualCursor bool) ([]InputEvent, error) {
	if len(events) == 0 {
		return nil, newError(
			string(apierr.CodeInvalidArgument),
			"events must contain at least one input event",
		)
	}

	if len(events) > maxInputEvents {
		return nil, newError(
			string(apierr.CodeInvalidArgument),
			"events must contain at most %d input events",
			maxInputEvents,
		)
	}

	normalized := make([]InputEvent, 0, len(events))

	for index, event := range events {
		event, err := normalizeInputEvent(event, virtualCursor)
		if err != nil {
			return nil, newError(
				string(apierr.CodeInvalidArgument),
				"event %d: %s",
				index,
				err.Error(),
			)
		}

		normalized = append(normalized, event)
	}

	return normalized, nil
}

func normalizeInputEvent(event InputEvent, virtualCursor bool) (InputEvent, error) {
	event.Type = strings.ToLower(strings.TrimSpace(event.Type))

	switch event.Type {
	case inputActionPress, inputActionRelease:
		return normalizeActionEvent(event)
	case inputKeyPress, inputKeyRelease:
		return normalizeKeyEvent(event)
	case inputMouseButton:
		return normalizeMouseButtonEvent(event, virtualCursor)
	case inputMouseMotion:
		return normalizeMouseMotionEvent(event)
	case inputMouseScroll:
		return normalizeMouseScrollEvent(event, virtualCursor)
	case inputGestureMagnify:
		if event.Factor == nil || math.IsNaN(*event.Factor) ||
			math.IsInf(*event.Factor, 0) || *event.Factor <= 0 {
			return event, newError(
				string(apierr.CodeInvalidArgument),
				"gesture_magnify requires a factor greater than 0",
			)
		}

		if err := validateOptionalPosition(event.X, event.Y); err != nil {
			return event, err
		}

		return event, nil
	case inputGesturePan:
		if event.DX == nil && event.DY == nil {
			return event, newError(
				string(apierr.CodeInvalidArgument),
				"gesture_pan requires dx, dy, or both",
			)
		}

		if err := validateCoordinates(event.DX, event.DY); err != nil {
			return event, err
		}

		if err := validateOptionalPosition(event.X, event.Y); err != nil {
			return event, err
		}

		return event, nil
	default:
		return event, newError(
			string(apierr.CodeInvalidArgument),
			`unknown type %q; use action_press, action_release, key_press, key_release, mouse_button, mouse_motion, mouse_scroll, gesture_magnify, or gesture_pan`,
			event.Type,
		)
	}
}

func normalizeActionEvent(event InputEvent) (InputEvent, error) {
	event.Action = strings.TrimSpace(event.Action)

	if event.Action == "" {
		return event, newError(
			string(apierr.CodeInvalidArgument),
			"%s requires an action",
			event.Type,
		)
	}

	if len([]rune(event.Action)) > maxInputActionRunes {
		return event, newError(
			string(apierr.CodeInvalidArgument),
			"action must be at most %d characters",
			maxInputActionRunes,
		)
	}

	if event.Strength != nil {
		if math.IsNaN(*event.Strength) || math.IsInf(*event.Strength, 0) ||
			*event.Strength < 0 || *event.Strength > 1 {
			return event, newError(
				string(apierr.CodeInvalidArgument),
				"strength must be between 0 and 1",
			)
		}
	}

	if event.Type == inputActionRelease && event.Strength != nil {
		return event, newError(
			string(apierr.CodeInvalidArgument),
			"action_release does not accept strength",
		)
	}

	return event, nil
}

func normalizeKeyEvent(event InputEvent) (InputEvent, error) {
	event.Key = strings.TrimSpace(event.Key)

	switch {
	case event.Key != "" && event.Keycode != 0:
		return event, newError(
			string(apierr.CodeInvalidArgument),
			"specify key or keycode, not both",
		)
	case event.Key != "":
		if len([]rune(event.Key)) > maxInputKeyRunes {
			return event, newError(
				string(apierr.CodeInvalidArgument),
				"key must be at most %d characters",
				maxInputKeyRunes,
			)
		}
	case event.Keycode <= 0:
		return event, newError(
			string(apierr.CodeInvalidArgument),
			"%s requires key or keycode",
			event.Type,
		)
	}

	return event, nil
}

func normalizeMouseButtonEvent(event InputEvent, virtualCursor bool) (InputEvent, error) {
	button, err := normalizeMouseButton(event.Button)
	if err != nil {
		return event, err
	}

	event.Button = button

	if event.Pressed == nil {
		return event, newError(
			string(apierr.CodeInvalidArgument),
			"mouse_button requires pressed true (press) or false (release)",
		)
	}

	if err := validateMousePosition(event.X, event.Y, virtualCursor); err != nil {
		return event, err
	}

	if event.DX != nil || event.DY != nil {
		return event, newError(
			string(apierr.CodeInvalidArgument),
			"mouse_button does not accept dx or dy; use x and y",
		)
	}

	return event, nil
}

func normalizeMouseMotionEvent(event InputEvent) (InputEvent, error) {
	hasPosition := event.X != nil || event.Y != nil
	hasDelta := event.DX != nil || event.DY != nil

	if event.X != nil && event.Y == nil || event.X == nil && event.Y != nil {
		return event, newError(
			string(apierr.CodeInvalidArgument),
			"mouse_motion requires both x and y, or dx and dy",
		)
	}

	if !hasPosition && !hasDelta {
		return event, newError(
			string(apierr.CodeInvalidArgument),
			"mouse_motion requires x and y, or dx and dy",
		)
	}

	if err := validateCoordinates(event.X, event.Y); err != nil {
		return event, err
	}

	if err := validateCoordinates(event.DX, event.DY); err != nil {
		return event, err
	}

	return event, nil
}

func normalizeMouseScrollEvent(event InputEvent, virtualCursor bool) (InputEvent, error) {
	button := strings.ToLower(strings.TrimSpace(event.Button))

	if button == "" {
		button = "wheel_up"
	}

	switch button {
	case "wheel_up", "wheel_down", "wheel_left", "wheel_right":
		event.Button = button
	default:
		return event, newError(
			string(apierr.CodeInvalidArgument),
			"mouse_scroll button must be wheel_up, wheel_down, wheel_left, or wheel_right, got %q",
			event.Button,
		)
	}

	if err := validateMousePosition(event.X, event.Y, virtualCursor); err != nil {
		return event, err
	}

	if event.DX != nil || event.DY != nil {
		return event, newError(
			string(apierr.CodeInvalidArgument),
			"mouse_scroll does not accept dx or dy; use x and y",
		)
	}

	if event.Factor != nil {
		if math.IsNaN(*event.Factor) || math.IsInf(*event.Factor, 0) || *event.Factor <= 0 {
			return event, newError(
				string(apierr.CodeInvalidArgument),
				"mouse_scroll factor must be greater than 0",
			)
		}
	}

	return event, nil
}

func validateMousePosition(x *float64, y *float64, virtualCursor bool) error {
	if x == nil && y == nil {
		if virtualCursor {
			return nil
		}

		return newError(
			string(apierr.CodeInvalidArgument),
			"mouse events require x and y unless virtual_cursor is true",
		)
	}

	if x == nil || y == nil {
		return newError(
			string(apierr.CodeInvalidArgument),
			"mouse events require both x and y",
		)
	}

	return validateCoordinates(x, y)
}

func validateCoordinates(values ...*float64) error {
	for _, value := range values {
		if value == nil {
			continue
		}

		if math.IsNaN(*value) || math.IsInf(*value, 0) ||
			*value < -maxInputCoordinate || *value > maxInputCoordinate {
			return newError(
				string(apierr.CodeInvalidArgument),
				"coordinates and deltas must be finite and within %d pixels",
				int(maxInputCoordinate),
			)
		}
	}

	return nil
}

func validateOptionalPosition(x *float64, y *float64) error {
	if x == nil && y == nil {
		return nil
	}

	if x == nil || y == nil {
		return newError(
			string(apierr.CodeInvalidArgument),
			"supply both x and y, or neither",
		)
	}

	return validateCoordinates(x, y)
}

func normalizeMouseButton(button string) (string, error) {
	button = strings.ToLower(strings.TrimSpace(button))

	switch button {
	case "left", "right", "middle",
		"wheel_up", "wheel_down", "wheel_left", "wheel_right",
		"xbutton1", "xbutton2":
		return button, nil
	default:
		return button, newError(
			string(apierr.CodeInvalidArgument),
			`button must be left, right, middle, wheel_up, wheel_down, wheel_left, wheel_right, xbutton1, or xbutton2, got %q`,
			button,
		)
	}
}
