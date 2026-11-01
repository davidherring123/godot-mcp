@tool
extends EditorPlugin

# Test-only helper for the live core suite. The harness cannot ask the addon
# to start a scan without going through a product action, so tests drop a
# marker file into the project; this plugin starts a filesystem scan and writes
# a second marker once the scan thread has been started.
#
# This script ships only with tests/fixtures/core and is inert until a marker
# appears. It is not part of the released addon.

const REQUEST_PATH := "res://.test_scan_request"
const STARTED_PATH := "res://.test_scan_started"

var filesystem: EditorFileSystem

func _enter_tree() -> void:
	filesystem = get_editor_interface().get_resource_filesystem()
	set_process(true)

func _process(_delta: float) -> void:
	if filesystem == null or not FileAccess.file_exists(REQUEST_PATH):
		return

	# Leave the request in place until the filesystem is actually idle; scan()
	# silently returns while the first scan is still running.
	if filesystem.is_scanning():
		return

	DirAccess.remove_absolute(REQUEST_PATH)
	filesystem.scan()

	var started := FileAccess.open(STARTED_PATH, FileAccess.WRITE)

	if started != null:
		started.store_line("scanning")
		started.close()
