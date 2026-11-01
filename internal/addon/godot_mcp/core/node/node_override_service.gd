class_name NodeOverrideService
extends ActionService

# Read-only inventory of overrides recorded on a linked PackedScene instance.
# This uses only native PackedScene/SceneState data; it never saves, reverts,
# or edits the active scene.

const MAX_OVERRIDE_PROPERTIES := 1024
const MAX_OVERRIDE_NODES := 256

const BASELINE_SOURCE_SCENE := "source_scene"
const BASELINE_INHERITED_SCENE := "inherited_scene"
const BASELINE_DEFAULT := "default"
const DISK_COMPARED := "compared"
const DISK_UNAVAILABLE := "unavailable"

const LIMITATIONS := [
    "Instance-root overrides are explicit scene-state properties. Editable-child entries come from saved child scene state and from live child values that differ from the source scene; a value equal to the source is not classified as an override.",
    "Baselines absent from the source scene state come from an in-memory source instance, so they may be script or engine defaults and may run script initialization.",
    "Live values come from packing the active editor scene and the target instance in memory. Disk values come from loading the saved scene file with cache bypass; inspection never saves, reverts, or switches scenes.",
    "Editable-child and inherited-scene coverage depends on what Godot exposes through PackedScene/SceneState; unavailable baselines or child entries are omitted. Live child values changed by script initialization or runtime state can appear even without an explicit child override.",
    "A script override identical to the source scene's script is omitted as redundant scene-state bookkeeping; a different script or a cleared script is still reported.",
    "Child properties marked inferred are live values that differ from the source scene but were not recorded in saved child scene state; they are save candidates, not proof of an intentional override.",
    "This action does not infer whether an override was intentional or stale, and it does not compare against later source-scene edits beyond what the current source resource reports.",
]

var scene_context: EditorSceneContext

func _init(context: EditorSceneContext) -> void:
    scene_context = context

func get_actions() -> Dictionary:
    return {
        "node.overrides": ActionHandler.new(inspect_overrides, true, false, true),
    }

func inspect_overrides(request: Request) -> Response:
    var node_path = request.params.get("node")

    if not node_path is String or node_path.is_empty():
        return Response.failure(
            ErrorCode.INVALID_ARGUMENT,
            "A node path is required"
        )

    var filters := {}
    var requested = request.params.get("properties", [])

    if not requested is Array:
        return Response.failure(
            ErrorCode.INVALID_ARGUMENT,
            "properties must be an array of property names"
        )

    for property_name in requested:
        if not property_name is String or property_name.is_empty():
            return Response.failure(
                ErrorCode.INVALID_ARGUMENT,
                "Property filters must be nonempty strings"
            )

        filters[property_name] = true

    var root := scene_context.edited_root()
    var target := scene_context.find_node(node_path)

    if target == null:
        return Response.failure(
            ErrorCode.NOT_FOUND,
            "Could not find node: %s" % node_path
        )

    var live_state := _live_state(root)

    if live_state == null:
        return Response.failure(
            ErrorCode.ACTION_FAILED,
            "Could not pack the active scene to inspect instance overrides"
        )

    var target_state_path := _state_path(root, target)
    var target_index := _find_node_index(live_state, target_state_path)

    if target_index < 0:
        return Response.failure(
            ErrorCode.ACTION_FAILED,
            "Could not locate %s in the packed active scene" % node_path
        )

    var source: PackedScene = live_state.get_node_instance(target_index)

    if source == null:
        return Response.failure(
            ErrorCode.FAILED_PRECONDITION,
            "Node is not a linked PackedScene instance root: %s" % node_path
        )

    var disk := _disk_state(root.scene_file_path)
    var disk_state: SceneState = disk.get("state")
    var disk_status: String = disk.get("status")
    var source_instances := {}
    var limitations: Array = LIMITATIONS.duplicate()
    var budget := {
        "remaining": MAX_OVERRIDE_PROPERTIES,
        "truncated": false,
    }

    if disk_status != DISK_COMPARED:
        limitations.append(str(disk.get("note", "Disk scene state is unavailable.")))

    var root_properties := _finalize_properties(
        _property_map(live_state, target_state_path, disk_state),
        target,
        source,
        ".",
        filters,
        source_instances,
        budget
    )
    # Packing the whole edited scene keeps the instance link and root overrides
    # but skips editable children owned by the instance root. Packing the target
    # itself exposes those live child values; source comparison below keeps only
    # values that differ from the source scene.
    var target_live_state := _live_state(target)

    var children := _collect_children(
        live_state,
        target_state_path,
        target_live_state,
        disk_state,
        source,
        target,
        filters,
        source_instances,
        budget
    )
    var truncated := bool(budget.get("truncated", false))

    if truncated:
        limitations.append(
            "Override output was truncated to %d properties across at most %d child nodes."
            % [MAX_OVERRIDE_PROPERTIES, MAX_OVERRIDE_NODES]
        )

    _free_source_instances(source_instances)

    var description := scene_context.describe()

    return Response.success({
        "scene": root.scene_file_path,
        "path": str(root.get_path_to(target)),
        "name": str(target.name),
        "type": target.get_class(),
        "source_scene": str(source.resource_path),
        "linked": true,
        "unsaved": bool(description.get("unsaved", false)),
        "disk_status": disk_status,
        "root": root_properties,
        "children": children,
        "truncated": truncated,
        "limitations": limitations,
    })

func _collect_children(
    live_state: SceneState,
    target_state_path: String,
    target_live_state: SceneState,
    disk_state: SceneState,
    source: PackedScene,
    target: Node,
    filters: Dictionary,
    source_instances: Dictionary,
    budget: Dictionary
) -> Array:
    var groups := {}

    for index in range(live_state.get_node_count()):
        var state_path := str(live_state.get_node_path(index))

        if not _is_descendant_path(state_path, target_state_path):
            continue

        # Editable-child overrides appear as typeless node entries. Added nodes
        # have a type and are not overrides, so leave them out of the inventory.
        if not str(live_state.get_node_type(index)).is_empty():
            continue

        var relative_path := _relative_state_path(target_state_path, state_path)
        var group := _ensure_group(groups, relative_path, target)

        group["properties"] = _merge_property_maps(
            group["properties"],
            _property_map(live_state, state_path, disk_state)
        )

    if disk_state != null:
        for index in range(disk_state.get_node_count()):
            var state_path := str(disk_state.get_node_path(index))

            if not _is_descendant_path(state_path, target_state_path):
                continue

            if not str(disk_state.get_node_type(index)).is_empty():
                continue

            var relative_path := _relative_state_path(target_state_path, state_path)
            var group := _ensure_group(groups, relative_path, target)

            group["properties"] = _merge_property_maps(
                group["properties"],
                _property_map(live_state, state_path, disk_state)
            )

    if target_live_state != null:
        for index in range(1, target_live_state.get_node_count()):
            var state_path := str(target_live_state.get_node_path(index))

            if not state_path.begins_with("./"):
                continue

            var relative_path := state_path.substr(2)
            var known_source_node := _source_has_node(source, relative_path)

            if not known_source_node and not groups.has(relative_path):
                continue

            var actual := target.get_node_or_null(NodePath(relative_path))

            if actual == null:
                continue

            var group := _ensure_group(groups, relative_path, target)

            for property_index in range(
                target_live_state.get_node_property_count(index)
            ):
                var property_name := str(
                    target_live_state.get_node_property_name(index, property_index)
                )
                var value = target_live_state.get_node_property_value(
                    index,
                    property_index
                )
                if not known_source_node:
                    continue

                var baseline := _baseline(
                    source,
                    relative_path,
                    property_name,
                    actual,
                    source_instances
                )

                if not baseline.has("raw") or value == baseline["raw"]:
                    continue

                var existing: Dictionary = group["properties"].get(
                    property_name,
                    {}
                )
                var explicit := (
                    bool(existing.get("on_disk", false))
                    or bool(existing.get("live", false))
                )

                existing["live"] = true
                existing["value"] = value

                if not explicit:
                    existing["inferred"] = true

                group["properties"][property_name] = existing

    var paths: Array = groups.keys()

    paths.sort()

    if paths.size() > MAX_OVERRIDE_NODES:
        paths = paths.slice(0, MAX_OVERRIDE_NODES)
        budget["truncated"] = true

    var result: Array = []

    for relative_path in paths:
        if int(budget.get("remaining", 0)) <= 0:
            budget["truncated"] = true
            break

        var group: Dictionary = groups[relative_path]
        var properties := _finalize_properties(
            group["properties"],
            group["node"],
            source,
            relative_path,
            filters,
            source_instances,
            budget
        )

        if properties.is_empty():
            continue

        var item := {
            "path": relative_path,
            "properties": properties,
        }

        if group["node"] != null:
            item["type"] = group["node"].get_class()

        result.append(item)

    return result

func _ensure_group(
    groups: Dictionary,
    relative_path: String,
    target: Node
) -> Dictionary:
    if groups.has(relative_path):
        return groups[relative_path]

    var group := {
        "node": target.get_node_or_null(NodePath(relative_path)),
        "properties": {},
    }

    groups[relative_path] = group

    return group

func _merge_property_maps(target: Dictionary, source: Dictionary) -> Dictionary:
    for property_name in source:
        var incoming: Dictionary = source[property_name]

        if not target.has(property_name):
            target[property_name] = incoming.duplicate()
            continue

        var existing: Dictionary = target[property_name]

        if incoming.get("live", false):
            existing["live"] = true
            existing["value"] = incoming.get("value")

        if incoming.get("on_disk", false):
            existing["on_disk"] = true
            existing["disk_value"] = incoming.get("disk_value")

    return target

func _property_map(
    live_state: SceneState,
    state_path: String,
    disk_state: SceneState
) -> Dictionary:
    var result := {}
    var live_index := _find_node_index(live_state, state_path)

    if live_index >= 0:
        for property_index in range(
            live_state.get_node_property_count(live_index)
        ):
            var property_name := str(
                live_state.get_node_property_name(live_index, property_index)
            )

            result[property_name] = {
                "live": true,
                "value": live_state.get_node_property_value(
                    live_index,
                    property_index
                ),
            }

    if disk_state != null:
        var disk_index := _find_node_index(disk_state, state_path)

        if disk_index >= 0:
            for property_index in range(
                disk_state.get_node_property_count(disk_index)
            ):
                var property_name := str(
                    disk_state.get_node_property_name(disk_index, property_index)
                )
                var value = disk_state.get_node_property_value(
                    disk_index,
                    property_index
                )

                if result.has(property_name):
                    result[property_name]["on_disk"] = true
                    result[property_name]["disk_value"] = value
                else:
                    result[property_name] = {
                        "live": false,
                        "on_disk": true,
                        "disk_value": value,
                    }

    return result

func _finalize_properties(
    property_map: Dictionary,
    actual_node: Node,
    source: PackedScene,
    relative_path: String,
    filters: Dictionary,
    source_instances: Dictionary,
    budget: Dictionary
) -> Array:
    var names: Array = []

    for property_name in property_map:
        if not filters.is_empty() and not filters.has(property_name):
            continue

        names.append(property_name)

    names.sort()

    var remaining := int(budget.get("remaining", 0))

    if remaining <= 0 and not names.is_empty():
        budget["truncated"] = true
        return []

    if names.size() > remaining:
        names = names.slice(0, remaining)
        budget["truncated"] = true

    budget["remaining"] = remaining - names.size()

    var result: Array = []

    for property_name in names:
        var entry: Dictionary = property_map[property_name]
        var item := {
            "property": property_name,
            "live": bool(entry.get("live", false)),
            "inferred": bool(entry.get("inferred", false)),
            "on_disk": bool(entry.get("on_disk", false)),
        }

        if item["live"]:
            item["value"] = VariantSerializer.serialize(entry.get("value"))

        if actual_node != null and _has_property(actual_node, property_name):
            item["effective"] = VariantSerializer.serialize(
                actual_node.get(property_name)
            )

        if item["on_disk"]:
            item["disk_value"] = VariantSerializer.serialize(
                entry.get("disk_value")
            )

        var baseline := _baseline(
            source,
            relative_path,
            property_name,
            actual_node,
            source_instances
        )

        if baseline.has("value"):
            item["baseline"] = baseline["value"]
            item["baseline_source"] = baseline["source"]

        # PackedScene.pack() can record the source script on an instance root
        # even when it is identical to the source scene. That is redundant
        # scene-state bookkeeping, not a meaningful override. A script that
        # actually differs, clears the source script, or is absent from the
        # baseline is kept.
        if (
            property_name == "script"
            and item.has("value")
            and item.has("baseline")
            and item["value"] == item["baseline"]
        ):
            continue

        result.append(item)

    return result

func _baseline(
    source: PackedScene,
    relative_path: String,
    property_name: String,
    actual_node: Node,
    source_instances: Dictionary
) -> Dictionary:
    if source == null:
        return {}

    var source_state_path := (
        "." if relative_path == "." else "./" + relative_path
    )
    var recorded := _recorded_property(
        source.get_state(),
        source_state_path,
        property_name
    )

    if recorded.has("value"):
        return recorded

    var instance := _source_instance(source, source_instances)

    if instance == null:
        return {}

    var source_node: Node = instance

    if relative_path != ".":
        source_node = instance.get_node_or_null(NodePath(relative_path))

    if source_node == null or not _has_property(source_node, property_name):
        return {}

    var value = source_node.get(property_name)

    return {
        "value": VariantSerializer.serialize(value),
        "raw": value,
        "source": BASELINE_DEFAULT,
    }

func _source_has_node(source: PackedScene, relative_path: String) -> bool:
    if source == null:
        return false

    var state_path := "." if relative_path == "." else "./" + relative_path

    return _state_has_node(source.get_state(), state_path)

func _state_has_node(state: SceneState, state_path: String) -> bool:
    if state == null:
        return false

    if _find_node_index(state, state_path) >= 0:
        return true

    return _state_has_node(state.get_base_scene_state(), state_path)

func _recorded_property(
    state: SceneState,
    state_path: String,
    property_name: String
) -> Dictionary:
    if state == null:
        return {}

    var index := _find_node_index(state, state_path)

    if index >= 0:
        for property_index in range(state.get_node_property_count(index)):
            if str(state.get_node_property_name(index, property_index)) != property_name:
                continue

            var value = state.get_node_property_value(index, property_index)

            return {
                "value": VariantSerializer.serialize(value),
                "raw": value,
                "source": BASELINE_SOURCE_SCENE,
            }

    var base_state := state.get_base_scene_state()

    if base_state != null:
        var inherited := _recorded_property(
            base_state,
            state_path,
            property_name
        )

        if inherited.has("value"):
            inherited["source"] = BASELINE_INHERITED_SCENE
            return inherited

    return {}

func _source_instance(
    source: PackedScene,
    source_instances: Dictionary
) -> Node:
    var key := str(source.get_instance_id())

    if source_instances.has(key):
        return source_instances[key]

    var instance := source.instantiate()

    source_instances[key] = instance

    return instance

func _free_source_instances(source_instances: Dictionary) -> void:
    for key in source_instances:
        var instance = source_instances[key]

        if is_instance_valid(instance):
            instance.free()

func _live_state(root: Node) -> SceneState:
    if root == null:
        return null

    var packed := PackedScene.new()

    if packed.pack(root) != OK:
        return null

    return packed.get_state()

func _disk_state(path: String) -> Dictionary:
    if path.is_empty():
        return {
            "status": DISK_UNAVAILABLE,
            "state": null,
            "note": "The active scene has no saved scene path, so disk state is unavailable.",
        }

    if (
        not FileAccess.file_exists(path)
        or not ResourceLoader.exists(path, "PackedScene")
    ):
        return {
            "status": DISK_UNAVAILABLE,
            "state": null,
            "note": "The saved scene file is not available, so disk state is unavailable.",
        }

    var packed := ResourceLoader.load(
        path,
        "PackedScene",
        ResourceLoader.CACHE_MODE_IGNORE
    ) as PackedScene

    if packed == null:
        return {
            "status": DISK_UNAVAILABLE,
            "state": null,
            "note": "The saved scene file could not be loaded, so disk state is unavailable.",
        }

    return {
        "status": DISK_COMPARED,
        "state": packed.get_state(),
    }

func _state_path(root: Node, node: Node) -> String:
    if node == root:
        return "."

    return "./" + str(root.get_path_to(node))

func _find_node_index(state: SceneState, state_path: String) -> int:
    for index in range(state.get_node_count()):
        if str(state.get_node_path(index)) == state_path:
            return index

    return -1

func _is_descendant_path(state_path: String, ancestor_path: String) -> bool:
    if ancestor_path == ".":
        return state_path.begins_with("./")

    return state_path.begins_with(ancestor_path + "/")

func _relative_state_path(ancestor_path: String, state_path: String) -> String:
    if ancestor_path == ".":
        return state_path.substr(2)

    return state_path.substr(ancestor_path.length() + 1)

func _has_property(object: Object, property_name: String) -> bool:
    for property in object.get_property_list():
        if str(property.get("name", "")) == property_name:
            return true

    return false
