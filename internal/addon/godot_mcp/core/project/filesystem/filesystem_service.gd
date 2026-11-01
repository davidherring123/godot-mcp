class_name FileSystemService
extends ActionService

const DEFAULT_TIMEOUT_SECONDS := FileSystemIndex.DEFAULT_TIMEOUT_SECONDS
const MAX_TIMEOUT_SECONDS := FileSystemIndex.MAX_TIMEOUT_SECONDS
const MAX_PATHS := 100

var editor: EditorInterface
var filesystem_index: FileSystemIndex

func _init(editor_interface: EditorInterface) -> void:
    editor = editor_interface
    filesystem_index = FileSystemIndex.new(editor_interface)

func get_actions() -> Dictionary:
    return {
        "filesystem.scan": ActionHandler.new(scan, false, false, false),
    }

func scan(request: Request) -> Response:
    var path_response := _requested_paths(request.params.get("paths", []))

    if not path_response.code.is_empty():
        return path_response

    var timeout := _timeout_seconds(
        request.params.get("timeout_seconds"),
        DEFAULT_TIMEOUT_SECONDS
    )

    if timeout < 0.0:
        return Response.failure(
            ErrorCode.INVALID_ARGUMENT,
            "timeout_seconds must be between 0 and %s" % MAX_TIMEOUT_SECONDS
        )

    var filesystem := editor.get_resource_filesystem()

    if filesystem == null:
        return Response.failure(
            ErrorCode.FAILED_PRECONDITION,
            "The editor resource filesystem is not available"
        )

    var already_scanning := filesystem.is_scanning()
    var started := Time.get_ticks_msec()
    var deadline := started + int(timeout * 1000.0)
    var tree := editor.get_base_control().get_tree()
    var follow_up_scan := false
    var completed := false

    if already_scanning:
        # A scan already in progress may have enumerated a directory before an
        # external edit that happened before this request. Wait for it, then
        # start a fresh scan within the same deadline so those edits are seen.
        completed = await filesystem_index.wait_for_scan(
            filesystem,
            tree,
            deadline
        )

        if completed and Time.get_ticks_msec() < deadline:
            filesystem.scan()
            follow_up_scan = true
            await tree.process_frame
            completed = await filesystem_index.wait_for_scan(
                filesystem,
                tree,
                deadline
            )
    else:
        filesystem.scan()
        await tree.process_frame
        completed = await filesystem_index.wait_for_scan(
            filesystem,
            tree,
            deadline
        )

    var scanning := filesystem.is_scanning()

    if completed and filesystem.get_filesystem() == null:
        return Response.failure(
            ErrorCode.ACTION_FAILED,
            "The editor filesystem finished scanning without producing a filesystem tree"
        )

    var results: Array[Dictionary] = []

    for path in path_response.result:
        results.append(_verify_path(filesystem, path))

    return Response.success({
        "completed": completed,
        "already_scanning": already_scanning,
        "follow_up_scan": follow_up_scan,
        "scanning": scanning,
        "timed_out": not completed,
        "duration_ms": Time.get_ticks_msec() - started,
        "paths": results,
    })

func _requested_paths(value: Variant) -> Response:
    if not value is Array:
        return Response.failure(
            ErrorCode.INVALID_ARGUMENT,
            "paths must be an array of res:// resource paths"
        )

    if value.size() > MAX_PATHS:
        return Response.failure(
            ErrorCode.INVALID_ARGUMENT,
            "At most %d resource paths can be verified per scan" % MAX_PATHS
        )

    var paths: Array[String] = []
    var seen := {}

    for requested in value:
        if not requested is String or requested.is_empty():
            return Response.failure(
                ErrorCode.INVALID_ARGUMENT,
                "Resource paths must be nonempty strings"
            )

        var path: String = requested.simplify_path()

        if not path.begins_with("res://") or path == "res://":
            return Response.failure(
                ErrorCode.INVALID_ARGUMENT,
                "Resource paths must be res:// paths inside the project: %s" % requested
            )

        for segment in path.trim_prefix("res://").split("/"):
            if segment == "..":
                return Response.failure(
                    ErrorCode.INVALID_ARGUMENT,
                    "Resource paths must stay inside the project: %s" % requested
                )

        if seen.has(path):
            continue

        seen[path] = true
        paths.append(path)

    return Response.success(paths)

func _timeout_seconds(value: Variant, default_value: float) -> float:
    if value == null:
        return default_value

    if not (value is int or value is float):
        return -1.0

    var timeout := float(value)

    if timeout < 0.0 or timeout > MAX_TIMEOUT_SECONDS:
        return -1.0

    if timeout == 0.0:
        return default_value

    return timeout

func _verify_path(filesystem: EditorFileSystem, path: String) -> Dictionary:
    var file_type := str(filesystem.get_file_type(path))
    var uid_text := ""
    var uid_path := ""

    var uid := ResourceLoader.get_resource_uid(path)

    if uid != ResourceUID.INVALID_ID:
        uid_text = ResourceUID.id_to_text(uid)

        # get_id_path() logs "Unrecognized UID" for an unregistered ID, so the
        # registry is checked before resolving the path.
        if ResourceUID.has_id(uid):
            uid_path = ResourceUID.get_id_path(uid)
    else:
        var path_uid := ResourceUID.path_to_uid(path)

        if path_uid.begins_with("uid://"):
            var path_uid_id := ResourceUID.text_to_id(path_uid)

            if path_uid_id != ResourceUID.INVALID_ID:
                uid_text = path_uid

                if ResourceUID.has_id(path_uid_id):
                    uid_path = ResourceUID.get_id_path(path_uid_id)

    return {
        "path": path,
        "found": not file_type.is_empty(),
        "type": file_type,
        "uid": uid_text,
        "uid_path": uid_path,
        "uid_valid": not uid_text.is_empty() and uid_path == path,
    }
