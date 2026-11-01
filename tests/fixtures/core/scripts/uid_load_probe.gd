extends SceneTree

# Fresh-process load probe for UID regression tests. Run with:
#   godot --headless --path <project> --script res://scripts/uid_load_probe.gd -- <path>
#
# A separate process only knows UIDs persisted in .godot/uid_cache.bin, so any
# ext_resource uid that the editor never registered produces an engine
# "invalid UID" warning on stderr before this script prints its result.
func _initialize() -> void:
	var args := OS.get_cmdline_user_args()
	var path := ""

	if not args.is_empty():
		path = str(args[0])

	var loaded: Resource = null

	if not path.is_empty():
		loaded = load(path)

	print("UID_LOAD_PROBE path=%s ok=%s" % [path, loaded != null])
	quit(0 if loaded != null else 2)
