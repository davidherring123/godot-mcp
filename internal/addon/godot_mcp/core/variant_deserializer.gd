class_name VariantDeserializer
extends RefCounted

const MAX_INLINE_RESOURCE_DEPTH := 16

static func deserialize(
    value: Variant,
    type: int,
    hint: int = PROPERTY_HINT_NONE,
    hint_string: String = "",
    resource_overrides: Dictionary = {},
    depth: int = 0,
    current_value: Variant = null
) -> Response:
    match type:
        TYPE_BOOL:
            if value is bool:
                return Response.success(value)

        TYPE_INT:
            if _integer(value):
                return Response.success(int(value))

        TYPE_FLOAT:
            if _number(value):
                return Response.success(float(value))

        TYPE_STRING:
            if value is String:
                return Response.success(value)

        TYPE_STRING_NAME:
            if value is String:
                return Response.success(StringName(value))

        TYPE_NODE_PATH:
            if value is String:
                return Response.success(NodePath(value))

        TYPE_VECTOR2, TYPE_VECTOR2I:
            if _components(value, ["x", "y"], type == TYPE_VECTOR2I):
                if type == TYPE_VECTOR2I:
                    return Response.success(Vector2i(int(value.x), int(value.y)))

                return Response.success(Vector2(value.x, value.y))

        TYPE_VECTOR3, TYPE_VECTOR3I:
            if _components(value, ["x", "y", "z"], type == TYPE_VECTOR3I):
                if type == TYPE_VECTOR3I:
                    return Response.success(
                        Vector3i(int(value.x), int(value.y), int(value.z))
                    )

                return Response.success(Vector3(value.x, value.y, value.z))

        TYPE_VECTOR4, TYPE_VECTOR4I:
            if _components(value, ["x", "y", "z", "w"], type == TYPE_VECTOR4I):
                if type == TYPE_VECTOR4I:
                    return Response.success(
                        Vector4i(
                            int(value.x),
                            int(value.y),
                            int(value.z),
                            int(value.w)
                        )
                    )

                return Response.success(Vector4(value.x, value.y, value.z, value.w))

        TYPE_COLOR:
            if _components(value, ["r", "g", "b", "a"]):
                return Response.success(Color(value.r, value.g, value.b, value.a))

        TYPE_RECT2, TYPE_RECT2I:
            if (
                not value is Dictionary
                or value.size() != 2
                or not value.has_all(["position", "size"])
            ):
                return Response.failure(
                    ErrorCode.INVALID_ARGUMENT,
                    "Expected position and size objects"
                )

            var integers := type == TYPE_RECT2I

            if (
                not _components(value.position, ["x", "y"], integers)
                or not _components(value.size, ["x", "y"], integers)
            ):
                return Response.failure(
                    ErrorCode.INVALID_ARGUMENT,
                    "Expected position and size vectors matching %s" % type_string(type)
                )

            if integers:
                return Response.success(
                    Rect2i(
                        Vector2i(int(value.position.x), int(value.position.y)),
                        Vector2i(int(value.size.x), int(value.size.y))
                    )
                )

            return Response.success(
                Rect2(
                    Vector2(value.position.x, value.position.y),
                    Vector2(value.size.x, value.size.y)
                )
            )

        TYPE_ARRAY:
            if value is Array:
                return _array(
                    value,
                    hint,
                    hint_string,
                    resource_overrides,
                    depth,
                    current_value
                )

            return _expected(type)

        TYPE_DICTIONARY:
            if value is Dictionary:
                return Response.success(value)

            return _expected(type)

        TYPE_PACKED_BYTE_ARRAY:
            return _packed(value, type)

        TYPE_PACKED_INT32_ARRAY:
            return _packed(value, type)

        TYPE_PACKED_INT64_ARRAY:
            return _packed(value, type)

        TYPE_PACKED_FLOAT32_ARRAY:
            return _packed(value, type)

        TYPE_PACKED_FLOAT64_ARRAY:
            return _packed(value, type)

        TYPE_PACKED_STRING_ARRAY:
            return _packed(value, type)

        TYPE_PACKED_VECTOR2_ARRAY:
            return _packed(value, type)

        TYPE_PACKED_VECTOR3_ARRAY:
            return _packed(value, type)

        TYPE_PACKED_VECTOR4_ARRAY:
            return _packed(value, type)

        TYPE_PACKED_COLOR_ARRAY:
            return _packed(value, type)

        TYPE_OBJECT:
            return _object(
                value,
                hint,
                hint_string,
                resource_overrides,
                depth
            )

        _:
            return Response.failure(
                ErrorCode.UNSUPPORTED,
                "Setting %s properties is not supported yet" % type_string(type)
            )

    return Response.failure(
        ErrorCode.INVALID_ARGUMENT,
        "Expected a JSON value matching %s" % type_string(type)
    )

static func _object(
    value: Variant,
    hint: int,
    hint_string: String,
    resource_overrides: Dictionary = {},
    depth: int = 0
) -> Response:
    if value == null:
        return Response.success(null)

    if not value is Dictionary:
        return _object_expected()

    if value.has("resource"):
        # Older readbacks omitted the advisory type; newer readbacks include it.
        # Keep both shapes readable as a reference and reserve inline fields for
        # anonymous resources.
        for field in ["properties", "id", "ref", "cycle", "truncated"]:
            if value.has(field):
                return Response.failure(
                    ErrorCode.INVALID_ARGUMENT,
                    "Use either a resource path or an inline resource, not both"
                )
    elif value.has("type"):
        return _inline(value, hint, hint_string, resource_overrides, depth)
    else:
        return _object_expected()

    var path = value["resource"]

    if not path is String or not path.begins_with("res://"):
        return Response.failure(
            ErrorCode.INVALID_ARGUMENT,
            "A resource reference requires a res:// path"
        )

    path = path.simplify_path()

    var resource: Resource = null

    if resource_overrides.has(path):
        var pending = resource_overrides[path]

        if pending == null:
            return Response.failure(
                ErrorCode.NOT_FOUND,
                "Resource not found: %s" % path
            )

        if not pending is Resource:
            return Response.failure(
                ErrorCode.ACTION_FAILED,
                "Could not load resource: %s" % path
            )

        resource = pending
    else:
        if not ResourceLoader.exists(path):
            return Response.failure(
                ErrorCode.NOT_FOUND,
                "Resource not found: %s" % path
            )

        resource = ResourceLoader.load(path)

        if resource == null:
            return Response.failure(
                ErrorCode.ACTION_FAILED,
                "Could not load resource: %s" % path
            )

    if not resource is Resource:
        return Response.failure(
            ErrorCode.INVALID_ARGUMENT,
            "Not a resource: %s" % path
        )

    var type_error := _resource_type_error(resource, hint, hint_string)

    if not type_error.is_empty():
        return Response.failure(ErrorCode.INVALID_ARGUMENT, type_error)

    return Response.success(resource)

static func _object_expected() -> Response:
    return Response.failure(
        ErrorCode.INVALID_ARGUMENT,
        "Expected null, a resource reference like {\"resource\": \"res://...\"}, " +
        "or an inline resource like {\"type\": \"BoxMesh\", \"properties\": {}}"
    )

static func _array(
    values: Array,
    hint: int,
    hint_string: String,
    resource_overrides: Dictionary,
    depth: int,
    current_value: Variant
) -> Response:
    var metadata := _typed_array_metadata(hint, hint_string, current_value)

    if not metadata.is_empty():
        if int(metadata.get("element_type", TYPE_NIL)) != TYPE_OBJECT:
            return Response.success(values)

        if not _resource_element_type(metadata):
            if not _has_resource_values(values):
                return Response.success(values)

            return Response.failure(
                ErrorCode.UNSUPPORTED,
                "Typed arrays of %s are not supported; only Resource element types are supported"
                % _typed_element_name(metadata)
            )

        return _typed_resource_values(
            values,
            metadata,
            resource_overrides,
            depth
        )

    # Preserve the previous fail-closed behavior for malformed or non-native
    # typed-array hints that still carry resource-shaped values. Nested arrays
    # are not decoded.
    if _typed_resource_array_hint(hint, hint_string) and _has_resource_values(values):
        return Response.failure(
            ErrorCode.UNSUPPORTED,
            "Inline resources inside a typed resource array are not supported yet; set the array only with supported plain values"
        )

    return Response.success(values)

static func _typed_array_metadata(
    hint: int,
    hint_string: String,
    current_value: Variant
) -> Dictionary:
    # Native array metadata is authoritative: a custom Resource element type
    # only appears in get_typed_script(), not in the property hint string.
    if current_value is Array:
        var current: Array = current_value
        var builtin := current.get_typed_builtin()

        if builtin != TYPE_NIL:
            return {
                "element_type": builtin,
                "class_name": str(current.get_typed_class_name()),
                "script": current.get_typed_script(),
            }

    if hint != PROPERTY_HINT_TYPE_STRING or hint_string.is_empty():
        return {}

    var element_types := hint_string.split(":", true, 1)[0].split("/")

    if element_types.is_empty() or not element_types[0].is_valid_int():
        return {}

    var element_type := int(element_types[0])

    if element_type != TYPE_OBJECT:
        return {
            "element_type": element_type,
            "class_name": "",
            "script": null,
        }

    var typed_class := hint_string.get_slice(":", 1).strip_edges()
    var script: Script = null

    # Without the live array, global script classes are only available by name.
    # Normalize them to the same Resource/script shape as native metadata.
    if not typed_class.is_empty() and not ClassDB.class_exists(typed_class):
        script = _global_class_script(typed_class)

        if script != null:
            typed_class = "Resource"

    return {
        "element_type": element_type,
        "class_name": typed_class,
        "script": script,
    }

static func _global_class_script(type_name: String) -> Script:
    for entry in ProjectSettings.get_global_class_list():
        if str(entry.get("class", "")) != type_name:
            continue

        var script = load(str(entry.get("path", "")))

        if script is Script:
            return script

    return null

static func _resource_element_type(metadata: Dictionary) -> bool:
    var script = metadata.get("script")

    if script is Script:
        var base := str((script as Script).get_instance_base_type())

        return base == "Resource" or (
            ClassDB.class_exists(base)
            and ClassDB.is_parent_class(base, "Resource")
        )

    var typed_class := str(metadata.get("class_name", ""))

    if typed_class.is_empty():
        return false

    return typed_class == "Resource" or (
        ClassDB.class_exists(typed_class)
        and ClassDB.is_parent_class(typed_class, "Resource")
    )

static func _typed_resource_values(
    values: Array,
    metadata: Dictionary,
    resource_overrides: Dictionary,
    depth: int
) -> Response:
    var resources: Array = []

    for index in values.size():
        var item = values[index]

        if item == null:
            resources.append(null)
            continue

        if not item is Dictionary:
            return _typed_array_element_expected(index)

        var decoded := _object(
            item,
            PROPERTY_HINT_NONE,
            "",
            resource_overrides,
            depth
        )

        if not decoded.code.is_empty():
            return Response.failure(
                decoded.code,
                "Typed resource array element %d: %s" % [index, decoded.message]
            )

        var resource = decoded.result

        if not resource is Resource:
            return _typed_array_element_expected(index)

        var type_error := _typed_element_error(resource, metadata)

        if not type_error.is_empty():
            return Response.failure(
                ErrorCode.INVALID_ARGUMENT,
                "Typed resource array element %d: %s" % [index, type_error]
            )

        resources.append(resource)

    var typed := Array(
        [],
        int(metadata.get("element_type", TYPE_OBJECT)),
        StringName(str(metadata.get("class_name", ""))),
        metadata.get("script")
    )

    for resource in resources:
        typed.append(resource)

    return Response.success(typed)

static func _typed_array_element_expected(index: int) -> Response:
    return Response.failure(
        ErrorCode.INVALID_ARGUMENT,
        (
            "Typed resource array element %d: expected null, a resource reference " +
            "like {\"resource\": \"res://...\"}, or an inline resource"
        ) % index
    )

static func _typed_element_error(
    resource: Resource,
    metadata: Dictionary
) -> String:
    var script = metadata.get("script")

    if script is Script:
        if _script_matches(resource, script):
            return ""

        return "Expected %s, got %s" % [
            _typed_element_name(metadata),
            ResourceFactory.type_name(resource),
        ]

    var typed_class := str(metadata.get("class_name", ""))

    if typed_class.is_empty():
        typed_class = "Resource"

    if _resource_class_matches(resource, typed_class):
        return ""

    return "Expected %s, got %s" % [
        typed_class,
        ResourceFactory.type_name(resource),
    ]

static func _script_matches(resource: Resource, expected: Script) -> bool:
    var script: Script = resource.get_script()

    while script != null:
        if script == expected:
            return true

        script = script.get_base_script()

    return false

static func _resource_class_matches(
    resource: Resource,
    typed_class: String
) -> bool:
    if resource.is_class(typed_class):
        return true

    var script: Script = resource.get_script()

    while script != null:
        if str(script.get_global_name()) == typed_class:
            return true

        script = script.get_base_script()

    return false

static func _typed_element_name(metadata: Dictionary) -> String:
    var script = metadata.get("script")

    if script is Script:
        var global_name := str((script as Script).get_global_name())

        if not global_name.is_empty():
            return global_name

        var path := str((script as Script).resource_path)

        if not path.is_empty():
            return path

    var typed_class := str(metadata.get("class_name", ""))

    return typed_class if not typed_class.is_empty() else "Resource"

static func _typed_resource_array_hint(hint: int, hint_string: String) -> bool:
    if hint != PROPERTY_HINT_TYPE_STRING or hint_string.is_empty():
        return false

    var element_types := hint_string.split(":", true, 1)[0]

    for part in element_types.split("/"):
        if int(part) == TYPE_OBJECT:
            return true

    return false

static func _has_resource_values(values: Array) -> bool:
    for item in values:
        if not item is Dictionary:
            continue

        for field in ["resource", "type", "ref", "cycle", "truncated"]:
            if item.has(field):
                return true

    return false

static func _inline(
    value: Dictionary,
    hint: int,
    hint_string: String,
    resource_overrides: Dictionary,
    depth: int
) -> Response:
    var markers: Array[String] = []

    for marker in ["truncated", "ref", "cycle"]:
        if value.has(marker):
            markers.append(marker)

    if not markers.is_empty():
        return Response.failure(
            ErrorCode.INVALID_ARGUMENT,
            "Inline resource graph is incomplete: %s markers cannot be read back without losing identity or properties; send a complete inline resource without graph markers"
            % ", ".join(markers)
        )

    if depth >= MAX_INLINE_RESOURCE_DEPTH:
        return Response.failure(
            ErrorCode.INVALID_ARGUMENT,
            "Inline resource nesting exceeds the maximum depth of %d" % MAX_INLINE_RESOURCE_DEPTH
        )

    var type_name = value.get("type")

    if not type_name is String or type_name.is_empty():
        return Response.failure(
            ErrorCode.INVALID_ARGUMENT,
            "An inline resource requires a nonempty type"
        )

    var properties = value.get("properties", {})

    if not properties is Dictionary:
        return Response.failure(
            ErrorCode.INVALID_ARGUMENT,
            "Inline resource properties must be an object"
        )

    var resource := ResourceFactory.instantiate(type_name)

    if resource == null:
        return Response.failure(
            ErrorCode.INVALID_ARGUMENT,
            "Unknown or non-instantiable resource type: %s" % type_name
        )

    # Custom Resource scripts run their constructors and property setters here,
    # in memory, before the live object is touched. A failed nested value
    # discards the whole inline resource.
    var property_error := PropertyCodec.apply_properties(
        resource,
        properties,
        resource_overrides,
        depth + 1
    )

    if property_error != null:
        return Response.failure(
            property_error.code,
            "Inline %s: %s" % [type_name, property_error.message]
        )

    var type_error := _resource_type_error(resource, hint, hint_string)

    if not type_error.is_empty():
        return Response.failure(ErrorCode.INVALID_ARGUMENT, type_error)

    return Response.success(resource)

static func _resource_type_error(
    resource: Resource,
    hint: int,
    hint_string: String
) -> String:
    if hint != PROPERTY_HINT_RESOURCE_TYPE or hint_string.is_empty():
        return ""

    var expected: PackedStringArray = []

    for raw_type in hint_string.split(",", false):
        var type_name := raw_type.strip_edges()

        if not type_name.is_empty():
            expected.append(type_name)

    if expected.is_empty():
        return ""

    for type_name in expected:
        if _resource_matches(resource, type_name):
            return ""

    return "Expected %s, got %s" % [" or ".join(expected), resource.get_class()]

static func _resource_matches(resource: Resource, type_name: String) -> bool:
    if resource.is_class(type_name):
        return true

    var script: Script = resource.get_script()

    while script != null:
        if script.get_global_name() == type_name:
            return true

        script = script.get_base_script()

    return false

static func _number(value: Variant) -> bool:
    return (value is int or value is float) and is_finite(float(value))

static func _integer(value: Variant) -> bool:
    if value is int:
        return true

    return (
        value is float
        and is_finite(value)
        and floor(value) == value
        and abs(value) <= 9007199254740991.0
    )

static func _components(value: Variant, names: Array, integers: bool = false) -> bool:
    if not value is Dictionary or value.size() != names.size() or not value.has_all(names):
        return false

    for name in names:
        if not _number(value[name]):
            return false

        if abs(value[name]) > 3.402823466e38:
            return false

        if integers and (
            not _integer(value[name])
            or value[name] < -2147483648
            or value[name] > 2147483647
        ):
            return false

    return true

static func _expected(type: int) -> Response:
    return Response.failure(
        ErrorCode.INVALID_ARGUMENT,
        "Expected a JSON value matching %s" % type_string(type)
    )

static func _packed(value: Variant, type: int) -> Response:
    if not value is Array:
        return _packed_expected(type)

    var items: Array = []

    match type:
        TYPE_PACKED_BYTE_ARRAY:
            for index in value.size():
                var item = value[index]

                if not _integer(item) or item < 0 or item > 255:
                    return _packed_expected(type, index)

                items.append(int(item))

            return Response.success(PackedByteArray(items))

        TYPE_PACKED_INT32_ARRAY, TYPE_PACKED_INT64_ARRAY:
            for index in value.size():
                var item = value[index]

                if not _integer(item):
                    return _packed_expected(type, index)

                if type == TYPE_PACKED_INT32_ARRAY and (
                    item < -2147483648 or item > 2147483647
                ):
                    return _packed_expected(type, index)

                items.append(int(item))

            if type == TYPE_PACKED_INT32_ARRAY:
                return Response.success(PackedInt32Array(items))

            return Response.success(PackedInt64Array(items))

        TYPE_PACKED_FLOAT32_ARRAY, TYPE_PACKED_FLOAT64_ARRAY:
            for index in value.size():
                var item = value[index]

                if not _number(item):
                    return _packed_expected(type, index)

                items.append(float(item))

            if type == TYPE_PACKED_FLOAT32_ARRAY:
                return Response.success(PackedFloat32Array(items))

            return Response.success(PackedFloat64Array(items))

        TYPE_PACKED_STRING_ARRAY:
            for index in value.size():
                var item = value[index]

                if not item is String:
                    return _packed_expected(type, index)

                items.append(item)

            return Response.success(PackedStringArray(items))

        TYPE_PACKED_VECTOR2_ARRAY:
            for index in value.size():
                var item = value[index]

                if not _components(item, ["x", "y"]):
                    return _packed_expected(type, index)

                items.append(Vector2(item.x, item.y))

            return Response.success(PackedVector2Array(items))

        TYPE_PACKED_VECTOR3_ARRAY:
            for index in value.size():
                var item = value[index]

                if not _components(item, ["x", "y", "z"]):
                    return _packed_expected(type, index)

                items.append(Vector3(item.x, item.y, item.z))

            return Response.success(PackedVector3Array(items))

        TYPE_PACKED_VECTOR4_ARRAY:
            for index in value.size():
                var item = value[index]

                if not _components(item, ["x", "y", "z", "w"]):
                    return _packed_expected(type, index)

                items.append(Vector4(item.x, item.y, item.z, item.w))

            return Response.success(PackedVector4Array(items))

        TYPE_PACKED_COLOR_ARRAY:
            for index in value.size():
                var item = value[index]

                if not _components(item, ["r", "g", "b", "a"]):
                    return _packed_expected(type, index)

                items.append(Color(item.r, item.g, item.b, item.a))

            return Response.success(PackedColorArray(items))

    return _packed_expected(type)

static func _packed_expected(type: int, index: int = -1) -> Response:
    var shape := "a JSON array"

    match type:
        TYPE_PACKED_BYTE_ARRAY:
            shape = "an array of integers from 0 to 255"
        TYPE_PACKED_INT32_ARRAY:
            shape = "an array of 32-bit integers"
        TYPE_PACKED_INT64_ARRAY:
            shape = "an array of integers"
        TYPE_PACKED_FLOAT32_ARRAY, TYPE_PACKED_FLOAT64_ARRAY:
            shape = "an array of numbers"
        TYPE_PACKED_STRING_ARRAY:
            shape = "an array of strings"
        TYPE_PACKED_VECTOR2_ARRAY:
            shape = 'an array of {"x","y"} number objects'
        TYPE_PACKED_VECTOR3_ARRAY:
            shape = 'an array of {"x","y","z"} number objects'
        TYPE_PACKED_VECTOR4_ARRAY:
            shape = 'an array of {"x","y","z","w"} number objects'
        TYPE_PACKED_COLOR_ARRAY:
            shape = 'an array of {"r","g","b","a"} number objects'

    var location := ""

    if index >= 0:
        location = " at index %d" % index

    return Response.failure(
        ErrorCode.INVALID_ARGUMENT,
        "Expected %s for %s%s" % [shape, type_string(type), location]
    )
