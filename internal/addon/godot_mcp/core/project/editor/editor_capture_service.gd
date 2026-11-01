class_name EditorCaptureService
extends ActionService

const DEFAULT_TIMEOUT_SECONDS := 5.0
const MAX_TIMEOUT_SECONDS := 8.0
const CAPTURE_DIR := "res://.godot/godot-mcp/editor/captures"
const MIN_VIEWPORT_SIZE := 2
const MAX_3D_VIEWPORT_INDEX := 3
const ACTIVE_SETTLE_FRAMES := 5

var editor: EditorInterface

func _init(editor_interface: EditorInterface) -> void:
    editor = editor_interface

func get_actions() -> Dictionary:
    return {
        "editor.capture": ActionHandler.new(capture, false, false, false),
    }

func capture(request: Request) -> Response:
    if DisplayServer.get_name() == "headless":
        return Response.failure(
            ErrorCode.UNSUPPORTED,
            "Editor viewport capture requires a rendering display; start the editor without --headless"
        )

    var viewport_name := str(request.params.get("viewport", "3d")).to_lower()

    if viewport_name != "2d" and viewport_name != "3d":
        return Response.failure(
            ErrorCode.INVALID_ARGUMENT,
            "viewport must be 2d or 3d"
        )

    var index_value := _index_value(request.params.get("index"))

    if index_value == null:
        return Response.failure(
            ErrorCode.INVALID_ARGUMENT,
            "index must be a nonnegative integer"
        )

    var index: int = index_value

    if viewport_name == "2d" and index != 0:
        return Response.failure(
            ErrorCode.INVALID_ARGUMENT,
            "the 2D viewport only supports index 0"
        )

    if viewport_name == "3d" and index > MAX_3D_VIEWPORT_INDEX:
        return Response.failure(
            ErrorCode.INVALID_ARGUMENT,
            "the 3D viewport index must be between 0 and %d"
            % MAX_3D_VIEWPORT_INDEX
        )

    var timeout := _timeout_seconds(
        request.params.get("timeout_seconds"),
        DEFAULT_TIMEOUT_SECONDS
    )

    if timeout < 0.0:
        return Response.failure(
            ErrorCode.INVALID_ARGUMENT,
            "timeout_seconds must be between 0 and %s" % MAX_TIMEOUT_SECONDS
        )

    var previous_screen := _active_main_screen()
    var target_screen := "2D" if viewport_name == "2d" else "3D"
    var switched := not previous_screen.is_empty() and previous_screen != target_screen

    if switched:
        editor.set_main_screen_editor(target_screen)

    var viewport: SubViewport

    if viewport_name == "2d":
        viewport = editor.get_editor_viewport_2d()
    else:
        viewport = editor.get_editor_viewport_3d(index)

    if viewport == null:
        _restore_main_screen(previous_screen, switched)

        return Response.failure(
            ErrorCode.NOT_FOUND,
            "Editor %s viewport %d is not available" % [viewport_name, index]
        )

    var tree := editor.get_base_control().get_tree()

    for _frame in ACTIVE_SETTLE_FRAMES:
        if _viewport_active(viewport):
            break

        await tree.process_frame

    if not _viewport_active(viewport):
        _restore_main_screen(previous_screen, switched)

        return Response.failure(
            ErrorCode.FAILED_PRECONDITION,
            "Editor %s viewport %d is not active; select that viewport or enable the split view before capturing"
            % [viewport_name, index]
        )

    var image := await _rendered_image(viewport, timeout)

    if image == null:
        _restore_main_screen(previous_screen, switched)

        return Response.failure(
            ErrorCode.ACTION_FAILED,
            "The %s viewport did not produce a rendered image within %s seconds"
            % [viewport_name, timeout]
        )

    var directory := ProjectSettings.globalize_path(CAPTURE_DIR)
    var directory_error := DirAccess.make_dir_recursive_absolute(directory)

    if directory_error != OK and directory_error != ERR_ALREADY_EXISTS:
        _restore_main_screen(previous_screen, switched)

        return Response.failure(
            ErrorCode.ACTION_FAILED,
            "Could not create %s: %s" % [directory, error_string(directory_error)]
        )

    var filename := "capture-%d-%d.png" % [Time.get_ticks_usec(), randi()]
    var artifact := CAPTURE_DIR.path_join(filename)
    var absolute := ProjectSettings.globalize_path(artifact)
    var save_error := image.save_png(absolute)

    _restore_main_screen(previous_screen, switched)

    if save_error != OK:
        return Response.failure(
            ErrorCode.ACTION_FAILED,
            "Could not save editor viewport capture: %s" % error_string(save_error)
        )

    var bytes := 0
    var file := FileAccess.open(absolute, FileAccess.READ)

    if file != null:
        bytes = file.get_length()

    return Response.success({
        "viewport": viewport_name,
        "index": index,
        "artifact": artifact,
        "mime_type": "image/png",
        "width": image.get_width(),
        "height": image.get_height(),
        "bytes": bytes,
    })

func _rendered_image(viewport: SubViewport, timeout: float) -> Image:
    var tree := editor.get_base_control().get_tree()
    var deadline := Time.get_ticks_msec() + int(timeout * 1000.0)

    while Time.get_ticks_msec() < deadline:
        await tree.process_frame

        if viewport.size.x <= MIN_VIEWPORT_SIZE or viewport.size.y <= MIN_VIEWPORT_SIZE:
            continue

        RenderingServer.force_draw(false)

        var image := viewport.get_texture().get_image()

        if image != null and image.get_width() > MIN_VIEWPORT_SIZE \
        and image.get_height() > MIN_VIEWPORT_SIZE:
            return image

    return null

func _viewport_active(viewport: SubViewport) -> bool:
    var parent := viewport.get_parent()

    if not parent is CanvasItem:
        return false

    return (
        parent.is_visible_in_tree()
        and viewport.size.x > MIN_VIEWPORT_SIZE
        and viewport.size.y > MIN_VIEWPORT_SIZE
    )

func _active_main_screen() -> String:
    for child in editor.get_editor_main_screen().get_children():
        if not child is Control or not child.visible:
            continue

        match child.get_class():
            "CanvasItemEditor":
                return "2D"
            "Node3DEditor":
                return "3D"
            "ScriptEditor":
                return "Script"
            "EditorAssetLibrary":
                return "AssetLib"

    return ""

func _restore_main_screen(previous: String, switched: bool) -> void:
    if switched and not previous.is_empty():
        editor.set_main_screen_editor(previous)

func _index_value(value: Variant) -> Variant:
    if value == null:
        return 0

    if value is int:
        if value < 0:
            return null

        return value

    if not value is float:
        return null

    var number := float(value)

    if number < 0.0 or number != floor(number):
        return null

    return int(number)

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
