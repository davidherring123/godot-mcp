extends Node2D

const PROBE_KEY := KEY_SPACE
const LOG_PREFIX := "runtime_automation"

var key_press_events := 0
var key_release_events := 0
var physics_frames := 0
var held_physics_frames := 0
var released_physics_frames := 0
var logged_held := false
var logged_released := false

func _input(event: InputEvent) -> void:
	if not (event is InputEventKey) or event.keycode != PROBE_KEY:
		return

	if event.pressed and not event.echo:
		key_press_events += 1
		print("%s: _input saw key_press key=%s" % [LOG_PREFIX, OS.get_keycode_string(event.keycode)])
	elif not event.pressed:
		key_release_events += 1
		print("%s: _input saw key_release key=%s" % [LOG_PREFIX, OS.get_keycode_string(event.keycode)])

func _physics_process(_delta: float) -> void:
	physics_frames += 1

	if Input.is_key_pressed(PROBE_KEY):
		held_physics_frames += 1

		if not logged_held:
			logged_held = true
			print("%s: physics poll observed held=true press_events=%d frame=%d" % [
				LOG_PREFIX,
				key_press_events,
				physics_frames,
			])
	elif key_press_events > 0:
		released_physics_frames += 1

		if not logged_released:
			logged_released = true
			print("%s: physics poll observed held=false release_events=%d frame=%d" % [
				LOG_PREFIX,
				key_release_events,
				physics_frames,
			])
