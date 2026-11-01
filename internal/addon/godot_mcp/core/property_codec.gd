class_name PropertyCodec
extends RefCounted

class DecodedProperties:
    var converted: Dictionary = {}
    var previous: Dictionary = {}
    var error: Response = null

# Control layout properties are required for scene layout even though Godot hides
# them (usage 0) or marks them read-only when a Container parents the control.
# Keep this list explicit: other non-editor properties remain rejected.
const CONTROL_LAYOUT_PROPERTIES := {
    "anchor_left": true,
    "anchor_top": true,
    "anchor_right": true,
    "anchor_bottom": true,
    "offset_left": true,
    "offset_top": true,
    "offset_right": true,
    "offset_bottom": true,
    "grow_horizontal": true,
    "grow_vertical": true,
}

static func apply_properties(
    object: Object,
    values: Dictionary,
    resource_overrides: Dictionary = {},
    depth: int = 0
) -> Response:
    if values.has("script"):
        var decoded_script := decode_properties(
            object,
            {"script": values["script"]},
            resource_overrides,
            depth
        )

        if decoded_script.error != null:
            return decoded_script.error

        object.set_script(decoded_script.converted["script"])

    var remaining: Dictionary = values.duplicate()

    remaining.erase("script")

    var decoded := decode_properties(
        object,
        remaining,
        resource_overrides,
        depth
    )

    if decoded.error != null:
        return decoded.error

    for property_name in property_order(decoded.converted):
        object.set(property_name, decoded.converted[property_name])

    return null

static func decode_properties(
    object: Object,
    values: Dictionary,
    resource_overrides: Dictionary = {},
    depth: int = 0
) -> DecodedProperties:
    var path_error := _property_path_error(values)

    if path_error != null:
        return decode_failure(path_error.code, path_error.message)

    var direct := {}
    var nested := {}

    for raw_path in values:
        if _is_nested_path(str(raw_path)):
            nested[raw_path] = values[raw_path]
        else:
            direct[raw_path] = values[raw_path]

    var result := _decode_direct_properties(
        object,
        direct,
        resource_overrides,
        depth
    )

    if result.error != null:
        return result

    var nested_result := _decode_nested_properties(
        object,
        nested,
        resource_overrides,
        depth
    )

    if nested_result.error != null:
        return nested_result

    for property_name in nested_result.converted:
        result.converted[property_name] = nested_result.converted[property_name]
        result.previous[property_name] = nested_result.previous[property_name]

    return result

static func _decode_direct_properties(
    object: Object,
    values: Dictionary,
    resource_overrides: Dictionary = {},
    depth: int = 0
) -> DecodedProperties:
    var definitions := _definitions(object)
    var dependencies := _dependency_names(object, definitions, values)

    if dependencies.is_empty():
        return _decode_against(
            object,
            definitions,
            values,
            resource_overrides,
            depth
        )

    # Shader parameters only exist after the shader is assigned. Stage the
    # dependency on a duplicate so validation stays atomic and the live object
    # is untouched if any later value is invalid.
    var staging := _staging_copy(object)

    if staging == null:
        return _decode_against(
            object,
            definitions,
            values,
            resource_overrides,
            depth
        )

    var result := DecodedProperties.new()
    var remaining: Dictionary = values.duplicate()
    var staging_definitions := _definitions(staging)

    for property_name in dependencies:
        var partial := _decode_against(
            staging,
            staging_definitions,
            {property_name: values[property_name]},
            resource_overrides,
            depth
        )

        if partial.error != null:
            return partial

        var converted_value = partial.converted[property_name]

        result.converted[property_name] = converted_value
        result.previous[property_name] = _previous_value(
            object,
            definitions,
            property_name
        )
        staging.set(property_name, converted_value)
        staging_definitions = _definitions(staging)
        remaining.erase(property_name)

    var decoded := _decode_against(
        staging,
        staging_definitions,
        remaining,
        resource_overrides,
        depth
    )

    if decoded.error != null:
        return decoded

    for property_name in decoded.converted:
        result.converted[property_name] = decoded.converted[property_name]
        result.previous[property_name] = _previous_value(
            object,
            definitions,
            property_name
        )

    return result

static func _decode_nested_properties(
    object: Object,
    values: Dictionary,
    resource_overrides: Dictionary = {},
    depth: int = 0
) -> DecodedProperties:
    var result := DecodedProperties.new()

    if values.is_empty():
        return result

    var definitions := _definitions(object)
    var groups := {}

    for raw_path in values:
        var segments: PackedStringArray = str(raw_path).split(":")
        var root: String = segments[0]

        if not groups.has(root):
            groups[root] = []

        groups[root].append({
            "path": str(raw_path),
            "segments": segments,
        })

    var roots: Array = groups.keys()

    roots.sort()

    for root in roots:
        if not definitions.has(root):
            return decode_failure(
                ErrorCode.NOT_FOUND,
                "Unknown property: %s" % root
            )

        var definition: Dictionary = definitions[root]

        if not _editor_writable(
            object,
            root,
            int(definition.get("usage", 0))
        ):
            return decode_failure(
                ErrorCode.FAILED_PRECONDITION,
                "Property is not editor-writable: %s" % root
            )

        var root_value = object.get(root)
        var working = root_value
        var owned := {}
        var entries: Array = groups[root]

        if root_value is Object:
            if not root_value is Resource:
                return decode_failure(
                    ErrorCode.FAILED_PRECONDITION,
                    "Nested property paths require a Resource property: %s" % root
                )

            # Copy-on-write: packed-scene previews share built-in resources
            # with the live scene, and saved resources may be shared by several
            # objects too. Never mutate the original while validating or
            # applying a nested write.
            working = (root_value as Resource).duplicate()
            owned[working] = true
        elif not _supports_variant_path(root_value):
            return decode_failure(
                ErrorCode.FAILED_PRECONDITION,
                "Nested property paths are not supported for %s"
                % type_string(typeof(root_value))
            )

        entries.sort_custom(
            func(left: Dictionary, right: Dictionary) -> bool:
                return _nested_sort_key(left) < _nested_sort_key(right)
        )

        var changed := false

        for entry in entries:
            var segments: PackedStringArray = entry["segments"]
            var subnames := segments.slice(1)
            var resolved := _resolve_leaf(working, subnames, entry["path"])

            if resolved.has("error"):
                var failure: Response = resolved["error"]

                return decode_failure(failure.code, failure.message)

            var decoded := VariantDeserializer.deserialize(
                values[entry["path"]],
                int(resolved["type"]),
                int(resolved.get("hint", PROPERTY_HINT_NONE)),
                str(resolved.get("hint_string", "")),
                resource_overrides,
                depth,
                resolved.get("value")
            )

            if not decoded.code.is_empty():
                return decode_failure(
                    decoded.code,
                    "%s: %s" % [entry["path"], decoded.message]
                )

            var leaf_name: String = subnames[subnames.size() - 1]

            if _claims_resource_path(working, leaf_name, decoded.result):
                return decode_failure(
                    ErrorCode.INVALID_ARGUMENT,
                    "Resource paths cannot be reassigned through property values: %s; use resource.* actions to create or move resources instead"
                    % entry["path"]
                )

            if decoded.result != resolved["value"]:
                changed = true

            working = _apply_leaf(
                working,
                subnames,
                decoded.result,
                owned
            )

        result.converted[root] = working if changed else root_value
        result.previous[root] = root_value

    return result

static func _decode_against(
    object: Object,
    definitions: Dictionary,
    values: Dictionary,
    resource_overrides: Dictionary,
    depth: int = 0
) -> DecodedProperties:
    var names: Array = values.keys()

    names.sort()

    var result := DecodedProperties.new()

    for property_name in names:
        if not definitions.has(property_name):
            return decode_failure(
                ErrorCode.NOT_FOUND,
                "Unknown property: %s" % property_name
            )

        var definition: Dictionary = definitions[property_name]
        var usage: int = definition.get("usage", 0)

        if not _editor_writable(object, property_name, usage):
            return decode_failure(
                ErrorCode.FAILED_PRECONDITION,
                "Property is not editor-writable: %s" % property_name
            )

        var current_value: Variant = null

        if (
            int(definition.get("type", TYPE_NIL)) == TYPE_ARRAY
            and values[property_name] is Array
        ):
            current_value = object.get(property_name)

        var decoded := VariantDeserializer.deserialize(
            values[property_name],
            definition.type,
            definition.get("hint", PROPERTY_HINT_NONE),
            str(definition.get("hint_string", "")),
            resource_overrides,
            depth,
            current_value
        )

        if not decoded.code.is_empty():
            return decode_failure(
                decoded.code,
                "%s: %s" % [property_name, decoded.message]
            )

        if _claims_resource_path(object, property_name, decoded.result):
            return decode_failure(
                ErrorCode.INVALID_ARGUMENT,
                "Resource paths cannot be reassigned through property values: %s; use resource.* actions to create or move resources instead"
                % property_name
            )

        result.converted[property_name] = decoded.result
        result.previous[property_name] = _previous_value(
            object,
            definitions,
            property_name
        )

    return result

static func _property_path_error(values: Dictionary) -> Response:
    var paths := {}

    for raw_path in values:
        if not raw_path is String or raw_path.is_empty():
            return Response.failure(
                ErrorCode.INVALID_ARGUMENT,
                "Property paths must be nonempty strings"
            )

        var segments: PackedStringArray = raw_path.split(":")

        for segment in segments:
            if segment.is_empty():
                return Response.failure(
                    ErrorCode.INVALID_ARGUMENT,
                    "Invalid property path: %s" % raw_path
                )

        paths[raw_path] = segments

    var names: Array = paths.keys()

    for left_index in names.size():
        for right_index in range(left_index + 1, names.size()):
            var left: String = names[left_index]
            var right: String = names[right_index]

            if (
                _segments_prefix(paths[left], paths[right])
                or _segments_prefix(paths[right], paths[left])
            ):
                return Response.failure(
                    ErrorCode.INVALID_ARGUMENT,
                    "Conflicting property paths: %s and %s; set either the parent property or its nested path, not both"
                    % [left, right]
                )

    return null

static func _segments_prefix(
    prefix: PackedStringArray,
    value: PackedStringArray
) -> bool:
    if prefix.size() >= value.size():
        return false

    for index in prefix.size():
        if prefix[index] != value[index]:
            return false

    return true

static func _is_nested_path(path: String) -> bool:
    return path.contains(":")

static func _nested_sort_key(entry: Dictionary) -> String:
    var segments: PackedStringArray = entry["segments"]

    # Shader parameters only exist after the shader is assigned to the
    # resource. Apply an explicit shader leaf before any parameter leaves on the
    # same nested root.
    if segments.size() == 2 and segments[1] == "shader":
        return "0:" + str(entry["path"])

    return "1:" + str(entry["path"])

static func _resolve_leaf(
    value: Variant,
    segments: PackedStringArray,
    label: String
) -> Dictionary:
    var current = value

    for index in segments.size():
        var segment: String = segments[index]
        var last := index == segments.size() - 1

        if current == null:
            return _resolve_error(
                ErrorCode.NOT_FOUND,
                "Cannot resolve nested property %s because an intermediate value is null"
                % label
            )

        if current is Object:
            var definitions := _definitions(current)

            if not definitions.has(segment):
                return _resolve_error(
                    ErrorCode.NOT_FOUND,
                    "Unknown nested property: %s" % label
                )

            var definition: Dictionary = definitions[segment]

            if not _editor_writable(
                current,
                segment,
                int(definition.get("usage", 0))
            ):
                return _resolve_error(
                    ErrorCode.FAILED_PRECONDITION,
                    "Nested property is not editor-writable: %s" % label
                )

            if last:
                return {
                    "type": int(definition.get("type", TYPE_NIL)),
                    "hint": int(definition.get("hint", PROPERTY_HINT_NONE)),
                    "hint_string": str(definition.get("hint_string", "")),
                    "value": current.get(segment),
                }

            var child = current.get(segment)

            if child is Object and not child is Resource:
                return _resolve_error(
                    ErrorCode.FAILED_PRECONDITION,
                    "Nested property paths through non-Resource objects are not supported: %s"
                    % label
                )

            current = child
        else:
            var component := _variant_component(current, segment)

            if component.is_empty():
                return _resolve_error(
                    ErrorCode.NOT_FOUND,
                    "Unknown nested property: %s" % label
                )

            if last:
                return {
                    "type": int(component["type"]),
                    "hint": PROPERTY_HINT_NONE,
                    "hint_string": "",
                    "value": component["value"],
                }

            current = component["value"]

    return _resolve_error(
        ErrorCode.NOT_FOUND,
        "Unknown nested property: %s" % label
    )

static func _resolve_error(code: String, message: String) -> Dictionary:
    return {
        "error": Response.failure(code, message),
    }

static func _apply_leaf(
    value: Variant,
    segments: PackedStringArray,
    leaf_value: Variant,
    owned: Dictionary
) -> Variant:
    if value is Object:
        var target: Object = value

        for index in range(segments.size() - 1):
            var segment: String = segments[index]
            var child = target.get(segment)

            if child is Object:
                var resource := child as Resource

                if resource == null:
                    # _resolve_leaf rejects these paths before any mutation.
                    return value

                if not owned.has(resource):
                    var copy := resource.duplicate()

                    owned[copy] = true
                    target.set(segment, copy)
                    resource = copy

                target = resource
            else:
                var updated := _variant_apply(
                    child,
                    segments.slice(index + 1),
                    leaf_value
                )

                target.set(segment, updated)

                return value

        target.set(segments[segments.size() - 1], leaf_value)

        return value

    return _variant_apply(value, segments, leaf_value)

static func _variant_apply(
    value: Variant,
    segments: PackedStringArray,
    leaf_value: Variant
) -> Variant:
    if segments.size() == 1:
        return _variant_set(value, segments[0], leaf_value)

    var component := _variant_component(value, segments[0])

    if component.is_empty():
        return value

    return _variant_set(
        value,
        segments[0],
        _variant_apply(component["value"], segments.slice(1), leaf_value)
    )

static func _supports_variant_path(value: Variant) -> bool:
    match typeof(value):
        TYPE_VECTOR2, TYPE_VECTOR2I, TYPE_VECTOR3, TYPE_VECTOR3I, TYPE_VECTOR4, TYPE_VECTOR4I, TYPE_COLOR, TYPE_RECT2, TYPE_RECT2I, TYPE_AABB, TYPE_TRANSFORM2D, TYPE_TRANSFORM3D, TYPE_BASIS, TYPE_PLANE, TYPE_QUATERNION, TYPE_PROJECTION:
            return true

    return false

static func _variant_component(value: Variant, name: String) -> Dictionary:
    match typeof(value):
        TYPE_VECTOR2:
            return _vector_component(value, name, TYPE_FLOAT, ["x", "y"])
        TYPE_VECTOR2I:
            return _vector_component(value, name, TYPE_INT, ["x", "y"])
        TYPE_VECTOR3:
            return _vector_component(value, name, TYPE_FLOAT, ["x", "y", "z"])
        TYPE_VECTOR3I:
            return _vector_component(value, name, TYPE_INT, ["x", "y", "z"])
        TYPE_VECTOR4:
            return _vector_component(value, name, TYPE_FLOAT, ["x", "y", "z", "w"])
        TYPE_VECTOR4I:
            return _vector_component(value, name, TYPE_INT, ["x", "y", "z", "w"])
        TYPE_COLOR:
            return _vector_component(value, name, TYPE_FLOAT, ["r", "g", "b", "a"])
        TYPE_QUATERNION:
            return _vector_component(value, name, TYPE_FLOAT, ["x", "y", "z", "w"])
        TYPE_RECT2:
            return _rect_component(value, name, TYPE_VECTOR2)
        TYPE_RECT2I:
            return _rect_component(value, name, TYPE_VECTOR2I)
        TYPE_AABB:
            return _rect_component(value, name, TYPE_VECTOR3)
        TYPE_TRANSFORM2D:
            return _transform_component(value, name, TYPE_VECTOR2)
        TYPE_TRANSFORM3D:
            return _transform_component(value, name, TYPE_VECTOR3)
        TYPE_BASIS:
            return _basis_component(value, name)
        TYPE_PLANE:
            if name == "normal":
                return {"type": TYPE_VECTOR3, "value": value.normal}

            if name == "d":
                return {"type": TYPE_FLOAT, "value": value.d}

        TYPE_PROJECTION:
            return _vector_component(value, name, TYPE_VECTOR4, ["x", "y", "z", "w"])

    return {}

static func _vector_component(
    value: Variant,
    name: String,
    type: int,
    names: Array
) -> Dictionary:
    if not names.has(name):
        return {}

    return {
        "type": type,
        "value": value[name],
    }

static func _rect_component(
    value: Variant,
    name: String,
    vector_type: int
) -> Dictionary:
    if name == "position":
        return {"type": vector_type, "value": value.position}

    if name == "size":
        return {"type": vector_type, "value": value.size}

    return {}

static func _transform_component(
    value: Variant,
    name: String,
    vector_type: int
) -> Dictionary:
    if name == "origin":
        return {"type": vector_type, "value": value.origin}

    if name == "x" or name == "y":
        return {"type": vector_type, "value": value[name]}

    if name == "z" and typeof(value) == TYPE_TRANSFORM3D:
        return {"type": vector_type, "value": value.z}

    return {}

static func _basis_component(value: Basis, name: String) -> Dictionary:
    if name == "x" or name == "y" or name == "z":
        return {"type": TYPE_VECTOR3, "value": value[name]}

    return {}

static func _variant_set(
    value: Variant,
    name: String,
    component_value: Variant
) -> Variant:
    var result = value

    match typeof(value):
        TYPE_VECTOR2, TYPE_VECTOR2I, TYPE_VECTOR3, TYPE_VECTOR3I, TYPE_VECTOR4, TYPE_VECTOR4I, TYPE_COLOR, TYPE_QUATERNION, TYPE_PROJECTION:
            result[name] = component_value
        TYPE_RECT2, TYPE_RECT2I, TYPE_AABB:
            result[name] = component_value
        TYPE_TRANSFORM2D, TYPE_TRANSFORM3D, TYPE_BASIS:
            result[name] = component_value
        TYPE_PLANE:
            result[name] = component_value

    return result

static func read_property(object: Object, path: String) -> Variant:
    if path.contains(":"):
        return object.get_indexed(NodePath(path))

    return object.get(path)

static func property_order(values: Dictionary) -> Array:
    var names: Array = []

    # Dependencies are applied first regardless of caller supplied key order.
    if values.has("script"):
        names.append("script")

    if values.has("shader"):
        names.append("shader")

    var remaining: Array = values.keys()

    remaining.sort()

    for property_name in remaining:
        if property_name == "script" or property_name == "shader":
            continue

        names.append(property_name)

    return names

static func _definitions(object: Object) -> Dictionary:
    var definitions := {}

    for property in object.get_property_list():
        definitions[property.name] = property

    return definitions

static func _dependency_names(
    object: Object,
    definitions: Dictionary,
    values: Dictionary
) -> Array:
    if object is ShaderMaterial and values.has("shader") and definitions.has("shader"):
        return ["shader"]

    return []

static func _staging_copy(object: Object) -> Object:
    if object is Resource:
        return (object as Resource).duplicate()

    return null

static func _previous_value(
    object: Object,
    definitions: Dictionary,
    property_name: String
) -> Variant:
    if not definitions.has(property_name):
        return null

    return object.get(property_name)

static func _editor_writable(
    object: Object,
    property_name: String,
    usage: int
) -> bool:
    # Godot marks Control layout properties read-only when a Container owns the
    # child's layout. They remain settable through their property setters, so
    # keep the explicit allowlist above the generic read-only check. Size and
    # position stay read-only because they are not in the allowlist.
    if _control_layout_property(object, property_name):
        return true

    if (usage & PROPERTY_USAGE_READ_ONLY) != 0:
        return false

    return (usage & PROPERTY_USAGE_EDITOR) != 0

static func _control_layout_property(
    object: Object,
    property_name: String
) -> bool:
    return object is Control and CONTROL_LAYOUT_PROPERTIES.has(property_name)

static func _claims_resource_path(
    object: Variant,
    property_name: String,
    value: Variant
) -> bool:
    # Anonymous inline resources read back with an empty resource_path, and a
    # saved resource may echo its current path back. Any other path would take
    # over an existing file and let a later save write through it, so paths stay
    # owned by resource.* actions.
    if (
        not object is Resource
        or property_name != "resource_path"
        or not value is String
    ):
        return false

    var requested := str(value).simplify_path()
    var current := str((object as Resource).resource_path).simplify_path()

    return requested != current

static func decode_failure(code: String, message: String) -> DecodedProperties:
    var result := DecodedProperties.new()

    result.error = Response.failure(code, message)

    return result

static func collect_properties(object: Object, filters: Dictionary = {}) -> Array[Dictionary]:
    var properties: Array[Dictionary] = []
    var context := VariantSerializer.Context.new()

    for property in object.get_property_list():
        var usage: int = property.get("usage", 0)
        var property_type: int = property.get("type", TYPE_NIL)
        var property_name: String = property.get("name", "")

        if (usage & PROPERTY_USAGE_EDITOR) == 0 and not _control_layout_property(
            object,
            property_name
        ):
            continue

        if property_type == TYPE_NIL:
            continue

        if not filters.is_empty() and not filters.has(property_name):
            continue

        if filters.has(property_name):
            filters[property_name] = true

        var value = object.get(property_name)

        var property_hint: int = property.get("hint", PROPERTY_HINT_NONE)
        var hint_string: String = property.get("hint_string", "")

        var serialized_hint := PropertyHintSerializer.serialize(
            property_hint,
            hint_string,
            value
        )

        var serialized_property := {
            "name": property_name,
            "type": type_string(property_type),
            "value": VariantSerializer.serialize(value, {}, context)
        }

        if serialized_hint != null:
            serialized_property["hint"] = serialized_hint

        properties.append(serialized_property)

    return properties
