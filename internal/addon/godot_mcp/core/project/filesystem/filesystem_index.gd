class_name FileSystemIndex
extends RefCounted

# Bounded waits stay below the bridge request timeout and match the limits of
# the filesystem.scan action.
const DEFAULT_TIMEOUT_SECONDS := 5.0
const MAX_TIMEOUT_SECONDS := 8.0

const PENDING_NOTE := (
    "The file exists on disk, but the editor has not registered its UID in the "
    + "filesystem cache yet. Do not create it again; run filesystem.scan and "
    + "retry the dependent operation."
)

const BATCH_PENDING_NOTE := (
    "Every requested write completed, but some written files are not "
    + "registered in the editor filesystem cache yet. Do not recreate them; "
    + "run filesystem.scan and retry the dependent operation."
)

var editor: EditorInterface

func _init(editor_interface: EditorInterface) -> void:
    editor = editor_interface

func deadline_after(timeout_seconds: float) -> int:
    var bounded := timeout_seconds

    if bounded <= 0.0:
        bounded = DEFAULT_TIMEOUT_SECONDS

    bounded = minf(bounded, MAX_TIMEOUT_SECONDS)

    return Time.get_ticks_msec() + int(bounded * 1000.0)

func uid_text(path: String) -> String:
    var uid := ResourceLoader.get_resource_uid(path)

    if uid == ResourceUID.INVALID_ID:
        return ""

    return ResourceUID.id_to_text(uid)

func is_registered(path: String) -> bool:
    return _is_registered_id(ResourceLoader.get_resource_uid(path), path)

# Additive result metadata: the UID that is already stored in the file and
# whether the editor registered it. This never creates or changes a UID.
func registration(path: String, registered: bool) -> Dictionary:
    var result := {
        "uid_registered": registered,
    }

    var uid := uid_text(path)

    if not uid.is_empty():
        result["uid"] = uid

    if not registered:
        result["note"] = PENDING_NOTE

    return result

func ensure_registered(path: String, deadline_ms: int) -> bool:
    var results := await ensure_registered_paths([path], deadline_ms)

    return bool(results.get(path, false))

# Registers the UIDs already stored in the given files with the editor
# filesystem and persists them to .godot/uid_cache.bin.
#
# update_file() is the native registration hook. It reads the UID from the
# file itself (header for .tres/.tscn/.res, .uid sidecar for scripts, .import
# for imported resources), adds it to ResourceUID and writes the UID cache.
# It does nothing while the parent directory is not in the filesystem index, so
# a newly created directory requires a bounded scan first. Wait rules match the
# filesystem.scan action: wait for an in-flight scan, then start a fresh one,
# and allow one frame for the finished tree to be published.
func ensure_registered_paths(paths: Array, deadline_ms: int) -> Dictionary:
    var registered := {}
    var pending: Array[String] = []

    for value in paths:
        var path := str(value)

        if is_registered(path):
            registered[path] = true
        else:
            registered[path] = false
            pending.append(path)

    if pending.is_empty():
        return registered

    var filesystem := editor.get_resource_filesystem()

    if filesystem == null:
        return registered

    var base := editor.get_base_control()

    if base == null or base.get_tree() == null:
        return registered

    var tree := base.get_tree()

    # Fast path for an already indexed directory: register immediately without
    # starting a scan.
    for path in pending:
        filesystem.update_file(path)

        if is_registered(path):
            registered[path] = true

    pending = _unregistered(pending, registered)
    var scans := 0

    while (
        not pending.is_empty()
        and scans < 2
        and Time.get_ticks_msec() < deadline_ms
    ):
        var completed := await wait_for_scan(filesystem, tree, deadline_ms)

        # The parent directory is indexed now, so update_file() can register a
        # file that the scan did not enumerate (it was written mid-scan). The
        # scan usually registers it itself; avoid touching an existing entry.
        for path in pending:
            if is_registered(path):
                registered[path] = true
                continue

            filesystem.update_file(path)

            if is_registered(path):
                registered[path] = true

        pending = _unregistered(pending, registered)

        if (
            pending.is_empty()
            or not completed
            or Time.get_ticks_msec() >= deadline_ms
        ):
            break

        filesystem.scan()
        scans += 1

    return registered

func wait_for_scan(
    filesystem: EditorFileSystem,
    tree: SceneTree,
    deadline_ms: int
) -> bool:
    while Time.get_ticks_msec() < deadline_ms:
        if filesystem.is_scanning():
            await tree.process_frame
            continue

        # The editor publishes the finished filesystem tree one frame after
        # is_scanning() becomes false. Re-check in case another scan starts.
        await tree.process_frame

        if not filesystem.is_scanning():
            return true

    return false

func _is_registered_id(uid: int, path: String) -> bool:
    if uid == ResourceUID.INVALID_ID or not ResourceUID.has_id(uid):
        return false

    return ResourceUID.get_id_path(uid) == path

func _unregistered(paths: Array, registered: Dictionary) -> Array[String]:
    var pending: Array[String] = []

    for value in paths:
        var path := str(value)

        if not bool(registered.get(path, false)):
            pending.append(path)

    return pending
