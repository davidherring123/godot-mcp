extends SceneTree

const SESSION_DIR_ARG := "--session-dir"
const SCENE_ARG := "--scene"
const READY_FRAMES := 2
const HEARTBEAT_INTERVAL := 0.5
const BASIS_IDLE := "idle"
const BASIS_PHYSICS := "physics"
const OP_WAIT := "wait"
const OP_CAPTURE := "capture"
const OP_INPUT := "input"
const MAX_INPUT_EVENTS := 32
const MAX_INPUT_TICK_FRAMES := 600
const MAX_INPUT_COORDINATE := 100000.0
const CAPTURE_BARRIER_TIMEOUT_MS := 5000

class InputProbe extends Node:
    var total := 0

    func _input(_event: InputEvent) -> void:
        total += 1

var session_dir := ""
var scene_path := ""
var start_ticks := 0
var frame_count := 0
var physics_frame_count := 0
var heartbeat_elapsed := 0.0
var ready_written := false
var quitting := false

var active_request = null
var active_request_id := ""
var active_kind := ""
var active_basis := BASIS_IDLE
var active_wait_frames := 0
var active_wait_seconds := 0.0
var active_start_frame := 0
var active_start_ticks := 0
var active_waited_frames := 0
var active_waited_ms := 0
var capture_output := ""
var capture_pending := false
var capture_barrier_ticks := 0

var input_probe: InputProbe = null
var input_cursor := Vector2.ZERO
var input_cursor_known := false
var active_events := []
var active_event_index := 0
var active_tick_frames := 0
var active_virtual_cursor := false
var active_next_dispatch_frame := 0
var active_input_start_frame := 0
var active_input_deadline_ms := 0
var active_deadline_ticks := 0
var active_expires_at_unix_ms := 0
var active_input_reports := []
var active_input_picks := []

var held_actions := {}
var held_keys := {}
var held_buttons := {}

func _initialize() -> void:
    start_ticks = Time.get_ticks_msec()

    var args := OS.get_cmdline_user_args()
    session_dir = _argument(args, SESSION_DIR_ARG)
    scene_path = _argument(args, SCENE_ARG)

    if session_dir.is_empty() or scene_path.is_empty():
        printerr("godot-mcp runtime: missing --session-dir or --scene")
        quit(2)
        return

    if not _make_directories():
        printerr("godot-mcp runtime: could not create the session directories")
        quit(2)
        return

    if not ResourceLoader.exists(scene_path, "PackedScene"):
        _write_ready(false, "Scene not found: %s" % scene_path)
        _finish(1, "scene_not_found")
        return

    var packed := ResourceLoader.load(scene_path, "PackedScene") as PackedScene

    if packed == null:
        _write_ready(false, "Could not load scene: %s" % scene_path)
        _finish(1, "scene_load_failed")
        return

    var instance := packed.instantiate()

    if instance == null:
        _write_ready(false, "Could not instantiate scene: %s" % scene_path)
        _finish(1, "scene_instantiate_failed")
        return

    get_root().add_child(instance)
    current_scene = instance

func _process(delta: float) -> bool:
    frame_count += 1

    if not ready_written and frame_count >= READY_FRAMES:
        _write_ready(true, "")
        _write_heartbeat()

    heartbeat_elapsed += delta

    if heartbeat_elapsed >= HEARTBEAT_INTERVAL:
        heartbeat_elapsed = 0.0
        _write_heartbeat()

    if FileAccess.file_exists(_path("stop.request")):
        _abort_active_request("runtime stopped before the request completed")
        _finish(0, "stop")
        return false

    # Bound every active request, including a capture barrier that never gets a
    # post-render frame. The runner's own deadline is earlier than the host
    # timeout, so this frees the session queue before the caller gives up.
    if active_request != null:
        _enforce_active_deadline()

    if active_request == null:
        _process_requests()

    if active_request != null and not capture_pending:
        if active_kind == OP_INPUT:
            _advance_input_request()
        elif active_basis == BASIS_IDLE:
            _advance_active_request()

    return false

func _physics_process(_delta: float) -> bool:
    physics_frame_count += 1

    if active_request != null and not capture_pending:
        if active_kind == OP_INPUT:
            _resolve_input_picks()
        elif active_basis == BASIS_PHYSICS:
            _advance_active_request()

    return false

func _finalize() -> void:
    if not quitting:
        _abort_active_request("runtime closed before the request completed")
        _finish(0, "closed")
func _argument(args: PackedStringArray, name: String) -> String:
    if args.is_empty():
        return ""

    for index in range(args.size() - 1):
        if args[index] == name:
            return args[index + 1]

    return ""

func _make_directories() -> bool:
    for name in ["requests", "responses", "captures"]:
        var error := DirAccess.make_dir_recursive_absolute(
            session_dir.path_join(name)
        )

        if error != OK and error != ERR_ALREADY_EXISTS:
            return false

    return true

func _path(name: String) -> String:
    return session_dir.path_join(name)

func _response_path(identifier: String) -> String:
    return _path("responses").path_join(identifier + ".json")

func _write_ready(ok: bool, message: String) -> void:
    if ready_written and ok:
        return

    ready_written = true

    var window_size := get_root().size

    _write_json(_path("ready.json"), {
        "ok": ok,
        "message": message,
        "scene": scene_path,
        "display_server": DisplayServer.get_name(),
        "headless": DisplayServer.get_name() == "headless",
        "window_size": {
            "width": window_size.x,
            "height": window_size.y,
        },
        "frames": frame_count,
        "pid": OS.get_process_id(),
    })

func _write_heartbeat() -> void:
    _write_json(_path("heartbeat.json"), {
        "frames": frame_count,
        "uptime_ms": Time.get_ticks_msec() - start_ticks,
    })

func _finish(code: int, reason: String) -> void:
    if quitting:
        return

    quitting = true

    var released := _release_held_inputs()

    _write_json(_path("exit.json"), {
        "reason": reason,
        "code": code,
        "frames": frame_count,
        "uptime_ms": Time.get_ticks_msec() - start_ticks,
        "released_held_inputs": released,
    })

    quit(code)

func _process_requests() -> void:
    var requests_dir := _path("requests")
    var names := DirAccess.get_files_at(requests_dir)

    if names.is_empty():
        return

    names.sort()

    for name in names:
        if not name.ends_with(".json"):
            continue

        var request_path := requests_dir.path_join(name)
        var identifier := name.get_basename()
        var file := FileAccess.open(request_path, FileAccess.READ)

        # Windows scanners or a concurrent rename can briefly deny access.
        # Keep the request queued instead of misreporting an IO failure as JSON.
        if file == null:
            continue

        var text := file.get_as_text()
        var read_error := file.get_error()
        file.close()

        if read_error != OK and read_error != ERR_FILE_EOF:
            continue

        var request = JSON.parse_string(text)

        DirAccess.remove_absolute(request_path)

        if not request is Dictionary:
            _write_json(_response_path(identifier), _failure(
                "invalid_argument",
                "runtime request was not a JSON object"
            ))
            return

        _begin_request(identifier, request)
        return

func _begin_request(identifier: String, request: Dictionary) -> void:
    var op := str(request.get("op", OP_CAPTURE))

    if op == OP_INPUT:
        _begin_input_request(identifier, request)
        return

    var basis := str(request.get("basis", BASIS_IDLE))

    if basis != BASIS_IDLE and basis != BASIS_PHYSICS:
        _write_json(_response_path(identifier), _failure(
            "invalid_argument",
            "runtime request basis must be idle or physics"
        ))
        return

    if op != OP_WAIT and op != OP_CAPTURE:
        _write_json(_response_path(identifier), _failure(
            "invalid_argument",
            "runtime request op must be wait or capture"
        ))
        return

    var frames: int = max(int(request.get("frames", 0)), 0)
    var seconds: float = max(float(request.get("seconds", 0.0)), 0.0)
    var expires_at_unix_ms: int = max(int(request.get("expires_at_unix_ms", 0)), 0)

    if expires_at_unix_ms > 0 and _unix_milliseconds() >= expires_at_unix_ms:
        # The request waited in the session queue past its submission deadline.
        # Reject it before it becomes the active request.
        _write_json(_response_path(identifier), _failure(
            "action_failed",
            "runtime %s request expired before it started because it waited in the session queue past its submission deadline" % op
        ))
        return

    if op == OP_WAIT and frames == 0 and seconds <= 0.0:
        _write_json(_response_path(identifier), _failure(
            "invalid_argument",
            "runtime wait requires frames, seconds, or both"
        ))
        return

    active_request = request
    active_request_id = identifier
    active_kind = op
    active_basis = basis
    active_wait_frames = frames
    active_wait_seconds = seconds
    active_start_frame = _basis_frame_count(basis)
    active_start_ticks = Time.get_ticks_usec()
    active_waited_frames = 0
    active_waited_ms = 0
    active_deadline_ticks = 0
    active_expires_at_unix_ms = expires_at_unix_ms
    capture_output = str(request.get("path", ""))
    capture_pending = false
    capture_barrier_ticks = 0

    if op == OP_CAPTURE and frames == 0 and seconds <= 0.0:
        _begin_capture_barrier()

func _advance_active_request() -> void:
    if active_request == null:
        return

    _enforce_active_deadline()

    if active_request == null:
        return

    var now := Time.get_ticks_usec()
    var elapsed_ms := int((now - active_start_ticks) / 1000.0)
    var current_frame := _basis_frame_count(active_basis)
    var frames_elapsed := current_frame - active_start_frame

    if active_wait_frames > 0 and frames_elapsed < active_wait_frames:
        return

    if active_wait_seconds > 0.0 and float(elapsed_ms) < active_wait_seconds * 1000.0:
        return

    active_waited_frames = frames_elapsed
    active_waited_ms = elapsed_ms

    if active_kind == OP_WAIT:
        _write_json(_response_path(active_request_id), {
            "ok": true,
            "basis": active_basis,
            "start_frame": active_start_frame,
            "end_frame": current_frame,
            "frames_elapsed": frames_elapsed,
            "requested_frames": active_wait_frames,
            "requested_seconds": active_wait_seconds,
            "elapsed_ms": elapsed_ms,
            "uptime_ms": Time.get_ticks_msec() - start_ticks,
        })
        _clear_active_request()
        return

    _begin_capture_barrier()

func _basis_frame_count(basis: String) -> int:
    if basis == BASIS_PHYSICS:
        return physics_frame_count

    return frame_count

func _begin_capture_barrier() -> void:
    capture_pending = true
    capture_barrier_ticks = Time.get_ticks_msec() + CAPTURE_BARRIER_TIMEOUT_MS

    if not RenderingServer.frame_post_draw.is_connected(_on_frame_post_draw):
        RenderingServer.frame_post_draw.connect(_on_frame_post_draw, CONNECT_ONE_SHOT)

func _on_frame_post_draw() -> void:
    if active_request == null or active_kind != OP_CAPTURE or not capture_pending:
        return

    if _active_request_expired():
        # The caller already gave up (or is about to). Do not write a PNG or a
        # success response after the submission deadline.
        _fail_active_request(
            "action_failed",
            "runtime capture expired at its submission deadline before the post-render capture completed; no screenshot was written"
        )
        return

    capture_pending = false
    capture_barrier_ticks = 0

    var response := _capture(capture_output)

    response["basis"] = active_basis
    response["waited_frames"] = active_waited_frames
    response["waited_ms"] = active_waited_ms

    _write_json(_response_path(active_request_id), response)
    _clear_active_request()

func _abort_active_request(message: String) -> void:
    if active_request == null:
        return

    _release_held_inputs()

    if capture_pending:
        capture_pending = false

        if RenderingServer.frame_post_draw.is_connected(_on_frame_post_draw):
            RenderingServer.frame_post_draw.disconnect(_on_frame_post_draw)

    var failure := _failure("failed_precondition", message)

    if active_kind == OP_INPUT:
        failure["events"] = active_events.size()
        failure["dispatched"] = active_event_index

    _write_json(_response_path(active_request_id), failure)
    _clear_active_request()

func _clear_active_request() -> void:
    active_request = null
    active_request_id = ""
    active_kind = ""
    active_basis = BASIS_IDLE
    active_wait_frames = 0
    active_wait_seconds = 0.0
    active_start_frame = 0
    active_start_ticks = 0
    active_waited_frames = 0
    active_waited_ms = 0
    capture_output = ""
    capture_pending = false
    capture_barrier_ticks = 0
    active_events = []
    active_event_index = 0
    active_tick_frames = 0
    active_virtual_cursor = false
    active_next_dispatch_frame = 0
    active_input_start_frame = 0
    active_input_deadline_ms = 0
    active_deadline_ticks = 0
    active_expires_at_unix_ms = 0
    active_input_reports = []
    active_input_picks = []

func _begin_input_request(identifier: String, request: Dictionary) -> void:
    var events: Array = request.get("events", [])
    var tick_frames: int = max(int(request.get("tick_frames", 0)), 0)
    var virtual_cursor: bool = bool(request.get("virtual_cursor", false))
    var expires_at_unix_ms: int = max(int(request.get("expires_at_unix_ms", 0)), 0)
    var budget_ms: int = max(int(request.get("budget_ms", 0)), 0)
    var event_count := events.size() if events is Array else 0
    var now_unix_ms := _unix_milliseconds()

    if expires_at_unix_ms > 0 and now_unix_ms >= expires_at_unix_ms:
        # The command waited in the session queue past its submission deadline.
        # Reject it before validation, probes, held-input changes, or delivery.
        var expired_message := (
            "runtime input request expired before dispatch: the submission deadline "
            + "had passed while the request waited in the session queue; 0 of %d events were delivered"
        ) % event_count

        _write_json(_response_path(identifier), {
            "ok": false,
            "code": "action_failed",
            "message": expired_message,
            "events": event_count,
            "dispatched": 0,
        })
        return

    var error: String = _validate_input_request(events, tick_frames, virtual_cursor)

    if not error.is_empty():
        _release_held_inputs()

        var invalid_failure := _failure("invalid_argument", error)
        invalid_failure["events"] = event_count
        invalid_failure["dispatched"] = 0

        _write_json(_response_path(identifier), invalid_failure)
        return

    if virtual_cursor:
        if not input_cursor_known:
            input_cursor = get_root().get_visible_rect().get_center()
            input_cursor_known = true
    else:
        # Without the opt-in virtual cursor, positioning follows the engine's
        # current mouse position rather than a cursor kept by the runner.
        input_cursor_known = false

    var effective_budget_ms := budget_ms

    if expires_at_unix_ms > 0:
        var remaining_ms := expires_at_unix_ms - now_unix_ms

        if effective_budget_ms <= 0 or remaining_ms < effective_budget_ms:
            effective_budget_ms = remaining_ms

    active_request = request
    active_request_id = identifier
    active_kind = OP_INPUT
    active_basis = BASIS_IDLE
    active_wait_frames = 0
    active_wait_seconds = 0.0
    active_start_frame = frame_count
    active_start_ticks = Time.get_ticks_usec()
    active_waited_frames = 0
    active_waited_ms = 0
    capture_output = ""
    capture_pending = false
    capture_barrier_ticks = 0
    active_events = events
    active_event_index = 0
    active_tick_frames = tick_frames
    active_virtual_cursor = virtual_cursor
    active_input_start_frame = frame_count
    active_next_dispatch_frame = frame_count
    active_input_deadline_ms = effective_budget_ms
    active_deadline_ticks = 0
    active_expires_at_unix_ms = expires_at_unix_ms

    if effective_budget_ms > 0:
        active_deadline_ticks = Time.get_ticks_msec() + effective_budget_ms

    active_input_reports = []
    active_input_picks = []

    _ensure_input_probe()

func _unix_milliseconds() -> int:
    return int(Time.get_unix_time_from_system() * 1000.0)

func _active_request_expired() -> bool:
    if active_deadline_ticks > 0 and Time.get_ticks_msec() > active_deadline_ticks:
        return true

    if active_expires_at_unix_ms > 0 and _unix_milliseconds() >= active_expires_at_unix_ms:
        return true

    return false

func _enforce_active_deadline() -> void:
    if active_request == null:
        return

    # A capture can only finish from RenderingServer.frame_post_draw. When the
    # render loop is disabled that signal never fires, so the barrier also has
    # its own fallback bound in addition to the submission deadline.
    if active_kind == OP_CAPTURE and capture_pending and capture_barrier_ticks > 0 and Time.get_ticks_msec() > capture_barrier_ticks:
        _fail_active_request(
            "action_failed",
            "runtime capture did not receive a post-render frame within %dms; no screenshot was written" % CAPTURE_BARRIER_TIMEOUT_MS
        )
        return

    if not _active_request_expired():
        return

    if active_kind == OP_INPUT:
        _fail_input_request(
            "action_failed",
            "runtime input sequence expired at its submission deadline after %d of %d events" % [
                active_event_index,
                active_events.size(),
            ]
        )
        return

    if active_kind == OP_CAPTURE:
        _fail_active_request(
            "action_failed",
            "runtime capture expired at its submission deadline before the post-render capture completed; no screenshot was written"
        )
        return

    var frames_elapsed: int = max(_basis_frame_count(active_basis) - active_start_frame, 0)

    _fail_active_request(
        "action_failed",
        "runtime wait expired at its submission deadline after %d frames on the %s basis" % [frames_elapsed, active_basis]
    )

func _fail_active_request(code: String, message: String) -> void:
    if active_request == null:
        return

    if capture_pending:
        capture_pending = false

        if RenderingServer.frame_post_draw.is_connected(_on_frame_post_draw):
            RenderingServer.frame_post_draw.disconnect(_on_frame_post_draw)

    _write_json(_response_path(active_request_id), _failure(code, message))
    _clear_active_request()

func _validate_input_request(events, tick_frames: int, virtual_cursor: bool) -> String:
    if not events is Array:
        return "input events must be an array"

    if events.is_empty():
        return "input requires at least one event"

    if events.size() > MAX_INPUT_EVENTS:
        return "input accepts at most %d events" % MAX_INPUT_EVENTS

    if tick_frames < 0 or tick_frames > MAX_INPUT_TICK_FRAMES:
        return "tick_frames must be between 0 and %d" % MAX_INPUT_TICK_FRAMES

    for index in range(events.size()):
        var candidate = events[index]

        if not candidate is Dictionary:
            return "event %d is not an object" % index

        var event: Dictionary = candidate
        var kind := str(event.get("type", ""))

        match kind:
            "action_press", "action_release":
                var action_name := str(event.get("action", "")).strip_edges()

                if action_name.is_empty():
                    return "event %d (%s) requires an action" % [index, kind]

                if not InputMap.has_action(action_name):
                    return 'event %d (%s): Input Map action "%s" is not defined' % [index, kind, action_name]

                if kind == "action_press" and event.has("strength"):
                    var strength := float(event["strength"])

                    if strength < 0.0 or strength > 1.0:
                        return "event %d strength must be between 0 and 1" % index
            "key_press", "key_release":
                if _event_keycode(event) == 0:
                    return "event %d (%s) requires a valid key or keycode" % [index, kind]
            "mouse_button":
                if not event.has("pressed"):
                    return "event %d (mouse_button) requires pressed" % index

                if _mouse_button_index(str(event.get("button", ""))) < 0:
                    return "event %d has an invalid mouse button" % index

                if not event.has("x") and not event.has("y") and not virtual_cursor:
                    return "event %d (mouse_button) requires x and y unless virtual_cursor is enabled" % index

                var button_position_error := _validate_event_position(event)

                if not button_position_error.is_empty():
                    return "event %d: %s" % [index, button_position_error]
            "mouse_motion":
                var has_x: bool = event.has("x")
                var has_y: bool = event.has("y")
                var has_dx: bool = event.has("dx")
                var has_dy: bool = event.has("dy")

                if not ((has_x and has_y) or has_dx or has_dy):
                    return "event %d (mouse_motion) requires x and y, or dx and dy" % index

                if has_x != has_y:
                    return "event %d (mouse_motion) requires both x and y" % index

                var motion_position_error := _validate_event_position(event)

                if not motion_position_error.is_empty():
                    return "event %d: %s" % [index, motion_position_error]
            "mouse_scroll":
                var button_name := str(event.get("button", "wheel_up"))

                if not button_name.begins_with("wheel_"):
                    return "event %d (mouse_scroll) requires a wheel button" % index

                if _mouse_button_index(button_name) < 0:
                    return "event %d has an invalid mouse wheel button" % index

                if not event.has("x") and not event.has("y") and not virtual_cursor:
                    return "event %d (mouse_scroll) requires x and y unless virtual_cursor is enabled" % index

                var scroll_position_error := _validate_event_position(event)

                if not scroll_position_error.is_empty():
                    return "event %d: %s" % [index, scroll_position_error]

                if event.has("factor") and float(event["factor"]) <= 0.0:
                    return "event %d scroll factor must be greater than 0" % index
            "gesture_magnify":
                if not event.has("factor") or float(event.get("factor", 0.0)) <= 0.0:
                    return "event %d (gesture_magnify) requires a factor greater than 0" % index

                var magnify_position_error := _validate_event_position(event)

                if not magnify_position_error.is_empty():
                    return "event %d: %s" % [index, magnify_position_error]
            "gesture_pan":
                if not event.has("dx") and not event.has("dy"):
                    return "event %d (gesture_pan) requires dx, dy, or both" % index

                var pan_position_error := _validate_event_position(event)

                if not pan_position_error.is_empty():
                    return "event %d: %s" % [index, pan_position_error]
            _:
                return 'event %d has unknown type "%s"' % [index, kind]

    return ""

func _validate_event_position(event: Dictionary) -> String:
    for key in ["x", "y", "dx", "dy"]:
        if not event.has(key):
            continue

        var value := float(event[key])

        if is_nan(value) or is_inf(value) or absf(value) > MAX_INPUT_COORDINATE:
            return "coordinates and deltas must be finite and within %d pixels" % int(MAX_INPUT_COORDINATE)

    if event.has("x") != event.has("y"):
        return "mouse events require both x and y"

    return ""

func _event_keycode(event: Dictionary) -> int:
    var keycode := int(event.get("keycode", 0))

    if keycode > 0:
        return keycode

    var key := str(event.get("key", "")).strip_edges()

    if key.is_empty():
        return 0

    return OS.find_keycode_from_string(key)

func _mouse_button_index(button: String) -> int:
    match button.strip_edges().to_lower():
        "left":
            return MOUSE_BUTTON_LEFT
        "right":
            return MOUSE_BUTTON_RIGHT
        "middle":
            return MOUSE_BUTTON_MIDDLE
        "wheel_up":
            return MOUSE_BUTTON_WHEEL_UP
        "wheel_down":
            return MOUSE_BUTTON_WHEEL_DOWN
        "wheel_left":
            return MOUSE_BUTTON_WHEEL_LEFT
        "wheel_right":
            return MOUSE_BUTTON_WHEEL_RIGHT
        "xbutton1":
            return MOUSE_BUTTON_XBUTTON1
        "xbutton2":
            return MOUSE_BUTTON_XBUTTON2

    return -1

func _mouse_button_name(button: int) -> String:
    match button:
        MOUSE_BUTTON_LEFT:
            return "left"
        MOUSE_BUTTON_RIGHT:
            return "right"
        MOUSE_BUTTON_MIDDLE:
            return "middle"
        MOUSE_BUTTON_WHEEL_UP:
            return "wheel_up"
        MOUSE_BUTTON_WHEEL_DOWN:
            return "wheel_down"
        MOUSE_BUTTON_WHEEL_LEFT:
            return "wheel_left"
        MOUSE_BUTTON_WHEEL_RIGHT:
            return "wheel_right"
        MOUSE_BUTTON_XBUTTON1:
            return "xbutton1"
        MOUSE_BUTTON_XBUTTON2:
            return "xbutton2"

    return str(button)

func _is_wheel_button(button: int) -> bool:
    return button >= MOUSE_BUTTON_WHEEL_UP and button <= MOUSE_BUTTON_WHEEL_RIGHT

func _event_position(event: Dictionary) -> Dictionary:
    var previous := input_cursor
    var has_previous := input_cursor_known
    var has_position := event.has("x") and event.has("y")
    var position := previous

    if has_position:
        position = Vector2(float(event["x"]), float(event["y"]))
    elif active_virtual_cursor and has_previous:
        position = previous
    elif not has_previous:
        position = get_root().get_mouse_position()
    else:
        position = previous

    var relative := Vector2.ZERO

    if event.has("dx") or event.has("dy"):
        relative = Vector2(float(event.get("dx", 0.0)), float(event.get("dy", 0.0)))

        if not has_position:
            position += relative
    elif has_position and has_previous:
        relative = position - previous

    input_cursor = position
    input_cursor_known = true

    return {
        "position": position,
        "relative": relative,
    }

func _event_anchor(event: Dictionary) -> Vector2:
    if event.has("x") and event.has("y"):
        return Vector2(float(event["x"]), float(event["y"]))

    if input_cursor_known:
        return input_cursor

    return get_root().get_mouse_position()

func _held_button_mask() -> int:
    var mask := 0

    for button in held_buttons.keys():
        mask |= 1 << (int(button) - 1)

    return mask

func _advance_input_request() -> void:
    if active_request == null:
        return

    if _active_request_expired():
        var deadline_message := "runtime input sequence exceeded its submission deadline or %dms active budget after %d of %d events" % [
            active_input_deadline_ms,
            active_event_index,
            active_events.size(),
        ]

        _fail_input_request("action_failed", deadline_message)
        return

    if not active_input_picks.is_empty():
        return

    if active_event_index >= active_events.size():
        _finish_input_request()
        return

    if frame_count < active_next_dispatch_frame:
        return

    if _active_request_expired():
        var late_message := "runtime input sequence expired before event %d; %d of %d events were delivered" % [
            active_event_index,
            active_event_index,
            active_events.size(),
        ]

        _fail_input_request("action_failed", late_message)
        return

    var event: Dictionary = active_events[active_event_index]
    var report := _dispatch_input_event(event, active_event_index)

    if report.is_empty():
        _fail_input_request(
            "internal_error",
            "the runtime could not build a report for input event %d" % active_event_index,
        )
        return

    var dispatch_error := str(report.get("_error", ""))

    if not dispatch_error.is_empty():
        _fail_input_request("invalid_argument", dispatch_error)
        return

    active_input_reports.append(report)
    active_event_index += 1
    active_next_dispatch_frame = frame_count + active_tick_frames + 1

    var kind := str(report.get("type", ""))

    if active_virtual_cursor and (
        kind == "mouse_button" or kind == "mouse_motion" or kind == "mouse_scroll"
    ):
        var reported_position = report.get("position", {})

        if reported_position is Dictionary:
            active_input_picks.append({
                "report": report,
                "position": Vector2(
                    float(reported_position.get("x", 0.0)),
                    float(reported_position.get("y", 0.0)),
                ),
            })

    if active_event_index >= active_events.size() and active_input_picks.is_empty():
        _finish_input_request()

func _dispatch_input_event(event: Dictionary, index: int) -> Dictionary:
    var kind := str(event.get("type", ""))
    var report := {
        "index": index,
        "type": kind,
        "delivery": "polling" if kind.begins_with("action_") else "parse_input_event",
        "polled": false,
        "callback": false,
    }

    if event.has("device"):
        report["device"] = int(event["device"])

    var device := int(event.get("device", 0))

    Input.flush_buffered_events()
    var before := _input_probe_total()

    match kind:
        "action_press":
            var action := str(event.get("action", ""))
            var strength := 1.0

            if not InputMap.has_action(action):
                report["_error"] = 'event %d (action_press): Input Map action "%s" is not defined' % [index, action]
                return report

            if event.has("strength"):
                strength = float(event["strength"])

            Input.action_press(action, strength)
            held_actions[action] = strength
            report["polled"] = Input.is_action_pressed(action)
        "action_release":
            var release_action := str(event.get("action", ""))

            if not InputMap.has_action(release_action):
                report["_error"] = 'event %d (action_release): Input Map action "%s" is not defined' % [index, release_action]
                return report

            Input.action_release(release_action)
            held_actions.erase(release_action)
            report["polled"] = Input.is_action_pressed(release_action)
        "key_press", "key_release":
            var keycode := _event_keycode(event)

            if keycode == 0:
                report["_error"] = "event %d (%s) requires a valid key or keycode" % [index, kind]
                return report

            var key_event := InputEventKey.new()
            key_event.keycode = keycode
            key_event.physical_keycode = keycode
            key_event.pressed = kind == "key_press"

            if event.has("device"):
                key_event.device = device

            Input.parse_input_event(key_event)
            Input.flush_buffered_events()

            if key_event.pressed:
                held_keys[keycode] = device
            else:
                held_keys.erase(keycode)

            report["polled"] = Input.is_key_pressed(keycode)
        "mouse_button":
            var button_geometry := _event_position(event)
            var button_position: Vector2 = button_geometry["position"]
            var button := _mouse_button_index(str(event.get("button", "")))
            var pressed := bool(event.get("pressed", false))
            var mouse_event := InputEventMouseButton.new()
            var button_mask := _held_button_mask()

            if not _is_wheel_button(button):
                if pressed:
                    button_mask |= 1 << (button - 1)
                else:
                    button_mask &= ~(1 << (button - 1))

            mouse_event.button_index = button
            mouse_event.pressed = pressed
            mouse_event.button_mask = button_mask
            mouse_event.position = button_position
            mouse_event.global_position = button_position

            if event.has("device"):
                mouse_event.device = device

            Input.parse_input_event(mouse_event)
            Input.flush_buffered_events()

            if pressed and not _is_wheel_button(button):
                held_buttons[button] = true
            elif not pressed:
                held_buttons.erase(button)

            report["position"] = {
                "x": button_position.x,
                "y": button_position.y,
            }
            report["polled"] = Input.is_mouse_button_pressed(button)
        "mouse_motion":
            var motion_geometry := _event_position(event)
            var motion_position: Vector2 = motion_geometry["position"]
            var motion_relative: Vector2 = motion_geometry["relative"]
            var motion_event := InputEventMouseMotion.new()

            motion_event.position = motion_position
            motion_event.global_position = motion_position
            motion_event.relative = motion_relative
            motion_event.button_mask = _held_button_mask()

            if event.has("device"):
                motion_event.device = device

            Input.parse_input_event(motion_event)
            Input.flush_buffered_events()

            report["position"] = {
                "x": motion_position.x,
                "y": motion_position.y,
            }
            report["polled"] = get_root().get_mouse_position().distance_to(motion_position) < 1.0
        "mouse_scroll":
            var scroll_geometry := _event_position(event)
            var scroll_position: Vector2 = scroll_geometry["position"]
            var scroll_button := _mouse_button_index(str(event.get("button", "wheel_up")))
            var scroll_event := InputEventMouseButton.new()

            scroll_event.button_index = scroll_button
            scroll_event.pressed = true
            scroll_event.factor = float(event.get("factor", 1.0))
            scroll_event.button_mask = _held_button_mask()
            scroll_event.position = scroll_position
            scroll_event.global_position = scroll_position

            if event.has("device"):
                scroll_event.device = device

            Input.parse_input_event(scroll_event)
            Input.flush_buffered_events()

            report["position"] = {
                "x": scroll_position.x,
                "y": scroll_position.y,
            }
        "gesture_magnify":
            var magnify_position := _event_anchor(event)
            var magnify_event := InputEventMagnifyGesture.new()

            magnify_event.factor = float(event.get("factor", 1.0))
            magnify_event.position = magnify_position

            if event.has("device"):
                magnify_event.device = device

            Input.parse_input_event(magnify_event)
            Input.flush_buffered_events()

            report["position"] = {
                "x": magnify_position.x,
                "y": magnify_position.y,
            }
        "gesture_pan":
            var pan_position := _event_anchor(event)
            var pan_event := InputEventPanGesture.new()

            pan_event.delta = Vector2(
                float(event.get("dx", 0.0)),
                float(event.get("dy", 0.0)),
            )
            pan_event.position = pan_position

            if event.has("device"):
                pan_event.device = device

            Input.parse_input_event(pan_event)
            Input.flush_buffered_events()

            report["position"] = {
                "x": pan_position.x,
                "y": pan_position.y,
            }
        _:
            report["_error"] = 'event %d has unknown type "%s"' % [index, kind]
            return report

    report["callback"] = _input_probe_total() > before

    return report

func _resolve_input_picks() -> void:
    if active_input_picks.is_empty():
        return

    for pick in active_input_picks:
        var report: Dictionary = pick["report"]
        report["pick_checked"] = true
        report["picks"] = _picks_at(pick["position"])

    active_input_picks = []

func _picks_at(position: Vector2) -> Array:
    var world := get_root().world_2d

    if world == null:
        return []

    var space := world.direct_space_state

    if space == null:
        return []

    var parameters := PhysicsPointQueryParameters2D.new()
    parameters.position = get_root().get_canvas_transform().affine_inverse() * position
    parameters.collide_with_areas = true
    parameters.collide_with_bodies = true

    var hits := space.intersect_point(parameters, 8)
    var names := []

    for hit in hits:
        var collider = hit.get("collider")

        if collider is Node:
            names.append(str(collider.name))

    return names

func _finish_input_request() -> void:
    var cursor = null

    if input_cursor_known:
        cursor = {
            "x": input_cursor.x,
            "y": input_cursor.y,
        }

    _write_json(_response_path(active_request_id), {
        "ok": true,
        "events": active_events.size(),
        "dispatched": active_event_index,
        "tick_frames": active_tick_frames,
        "frames": frame_count,
        "start_frames": active_input_start_frame,
        "frames_elapsed": frame_count - active_input_start_frame,
        "elapsed_ms": int((Time.get_ticks_usec() - active_start_ticks) / 1000.0),
        "uptime_ms": Time.get_ticks_msec() - start_ticks,
        "virtual_cursor": active_virtual_cursor,
        "cursor": cursor,
        "held": _held_inputs_snapshot(),
        "reports": active_input_reports,
    })

    _clear_active_request()

func _fail_input_request(code: String, message: String) -> void:
    _release_held_inputs()

    var failure := _failure(code, message)
    failure["events"] = active_events.size()
    failure["dispatched"] = active_event_index

    _write_json(_response_path(active_request_id), failure)
    _clear_active_request()

func _ensure_input_probe() -> void:
    if input_probe != null and is_instance_valid(input_probe):
        return

    input_probe = InputProbe.new()
    input_probe.name = "GodotMCPInputProbe"
    input_probe.process_mode = Node.PROCESS_MODE_ALWAYS
    get_root().add_child(input_probe, false, Node.INTERNAL_MODE_FRONT)

func _input_probe_total() -> int:
    if input_probe == null or not is_instance_valid(input_probe):
        return 0

    return input_probe.total

func _held_inputs_snapshot() -> Dictionary:
    var actions := []

    for action in held_actions.keys():
        actions.append(str(action))

    actions.sort()

    var keys := []

    for keycode in held_keys.keys():
        keys.append(int(keycode))

    keys.sort()

    var buttons := []

    for button in held_buttons.keys():
        buttons.append(_mouse_button_name(int(button)))

    buttons.sort()

    return {
        "actions": actions,
        "keys": keys,
        "mouse_buttons": buttons,
    }

func _release_held_inputs() -> Dictionary:
    var released := {
        "actions": 0,
        "keys": 0,
        "mouse_buttons": 0,
    }

    for action in held_actions.keys():
        Input.action_release(str(action))
        released["actions"] = int(released["actions"]) + 1

    for keycode in held_keys.keys():
        var key_event := InputEventKey.new()

        key_event.keycode = int(keycode)
        key_event.physical_keycode = int(keycode)
        key_event.pressed = false
        key_event.device = int(held_keys[keycode])

        Input.parse_input_event(key_event)
        released["keys"] = int(released["keys"]) + 1

    for button in held_buttons.keys():
        var button_event := InputEventMouseButton.new()

        button_event.button_index = int(button)
        button_event.pressed = false
        button_event.position = input_cursor if input_cursor_known else get_root().get_visible_rect().get_center()
        button_event.global_position = button_event.position

        Input.parse_input_event(button_event)
        released["mouse_buttons"] = int(released["mouse_buttons"]) + 1

    if int(released["actions"]) + int(released["keys"]) + int(released["mouse_buttons"]) > 0:
        Input.flush_buffered_events()

    held_actions.clear()
    held_keys.clear()
    held_buttons.clear()

    return released

func _failure(code: String, message: String) -> Dictionary:
    return {
        "ok": false,
        "code": code,
        "message": message,
    }

func _capture(output: String) -> Dictionary:
    if output.is_empty():
        return _failure("invalid_argument", "capture request is missing an output path")

    var absolute := output

    if output.begins_with("res://"):
        absolute = ProjectSettings.globalize_path(output)

    absolute = absolute.simplify_path()

    var allowed_root := ProjectSettings.globalize_path(
        "res://.godot/godot-mcp/runtime"
    ).simplify_path()

    if not absolute.begins_with(allowed_root + "/"):
        return _failure(
            "invalid_argument",
            "capture path must be inside res://.godot/godot-mcp/runtime"
        )

    if DisplayServer.get_name() == "headless":
        return _failure(
            "unsupported",
            "Screenshots require a rendering display; start the runtime with headless=false."
        )

    var texture := get_root().get_texture()

    if texture == null:
        return _failure(
            "unsupported",
            "The runtime root window has no rendered texture yet."
        )

    var image := texture.get_image()

    if image == null or image.get_width() == 0 or image.get_height() == 0:
        return _failure(
            "unsupported",
            "The runtime root window did not produce an image."
        )

    var error := image.save_png(absolute)

    if error != OK:
        return _failure(
            "action_failed",
            "Godot could not save the screenshot (error %d)." % error
        )

    var bytes := 0
    var file := FileAccess.open(absolute, FileAccess.READ)

    if file != null:
        bytes = file.get_length()

    return {
        "ok": true,
        "width": image.get_width(),
        "height": image.get_height(),
        "bytes": bytes,
        "frames": frame_count,
    }

func _write_json(path: String, value: Dictionary) -> void:
    var temporary := path + ".tmp"
    var file := FileAccess.open(temporary, FileAccess.WRITE)

    if file == null:
        return

    file.store_string(JSON.stringify(value))
    file.close()

    DirAccess.rename_absolute(temporary, path)

func _read_json(path: String):
    var text := FileAccess.get_file_as_string(path)

    if text.is_empty():
        return null

    return JSON.parse_string(text)
