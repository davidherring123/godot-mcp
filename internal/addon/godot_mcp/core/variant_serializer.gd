class_name VariantSerializer
extends RefCounted

# Anonymous resources (inline values and embedded resources without a path) are
# serialized as a bounded type/properties graph. Identity ids make repeated and
# cyclic references explicit without recursing forever.
const MAX_RESOURCE_DEPTH := 16
const MAX_RESOURCE_NODES := 128
const MAX_RESOURCE_PROPERTIES := 64

class Context:
    var identities: Dictionary = {}
    var active: Dictionary = {}
    var next_identity := 1
    var resource_count := 0

static func serialize(
    value: Variant,
    path_overrides: Dictionary = {},
    context: Context = null,
    depth: int = 0
) -> Variant:
    if context == null:
        context = Context.new()

    if value == null:
        return null

    match typeof(value):
        TYPE_STRING_NAME:
            return str(value)

        TYPE_NODE_PATH:
            return str(value)

        TYPE_VECTOR2, TYPE_VECTOR2I:
            return {
                "x": value.x,
                "y": value.y
            }

        TYPE_VECTOR3, TYPE_VECTOR3I:
            return {
                "x": value.x,
                "y": value.y,
                "z": value.z
            }

        TYPE_VECTOR4, TYPE_VECTOR4I:
            return {
                "x": value.x,
                "y": value.y,
                "z": value.z,
                "w": value.w
            }

        TYPE_COLOR:
            return {
                "r": value.r,
                "g": value.g,
                "b": value.b,
                "a": value.a
            }

        TYPE_RECT2, TYPE_RECT2I:
            return {
                "position": serialize(value.position, path_overrides, context, depth),
                "size": serialize(value.size, path_overrides, context, depth)
            }

        TYPE_ARRAY:
            return _serialize_array(value, path_overrides, context, depth)

        TYPE_DICTIONARY:
            return _serialize_dictionary(value, path_overrides, context, depth)

        TYPE_PACKED_BYTE_ARRAY, TYPE_PACKED_INT32_ARRAY, TYPE_PACKED_INT64_ARRAY, TYPE_PACKED_FLOAT32_ARRAY, TYPE_PACKED_FLOAT64_ARRAY, TYPE_PACKED_STRING_ARRAY, TYPE_PACKED_VECTOR2_ARRAY, TYPE_PACKED_VECTOR3_ARRAY, TYPE_PACKED_VECTOR4_ARRAY, TYPE_PACKED_COLOR_ARRAY:
            return _serialize_array(value, path_overrides, context, depth)

        TYPE_OBJECT:
            return _serialize_object(value, path_overrides, context, depth)

        TYPE_NIL, TYPE_BOOL, TYPE_INT, TYPE_FLOAT, TYPE_STRING:
            return value

        _:
            return str(value)

static func _serialize_array(
    value: Variant,
    path_overrides: Dictionary,
    context: Context,
    depth: int
) -> Array:
    var result: Array = []

    for item in value:
        result.append(serialize(item, path_overrides, context, depth + 1))

    return result

static func _serialize_dictionary(
    value: Dictionary,
    path_overrides: Dictionary,
    context: Context,
    depth: int
) -> Dictionary:
    var result := {}

    for key in value:
        result[str(key)] = serialize(
            value[key],
            path_overrides,
            context,
            depth + 1
        )

    return result

static func _serialize_object(
    value: Object,
    path_overrides: Dictionary,
    context: Context,
    depth: int
) -> Variant:
    if not value is Resource:
        return str(value)

    var path := str(value.resource_path)

    if path.is_empty():
        path = str(path_overrides.get(value.get_instance_id(), ""))

    if not path.is_empty():
        # Keep the established external-reference shape for saved files and
        # named embedded resources such as res://scene.tscn::SubResource.
        return {
            "resource": path,
            "type": ResourceFactory.type_name(value)
        }

    return _serialize_anonymous(value, path_overrides, context, depth)

static func _serialize_anonymous(
    resource: Resource,
    path_overrides: Dictionary,
    context: Context,
    depth: int
) -> Dictionary:
    var instance_id := resource.get_instance_id()
    var type_name := ResourceFactory.type_name(resource)

    if context.active.has(instance_id):
        return {
            "type": type_name,
            "ref": int(context.identities.get(instance_id, 0)),
            "cycle": true
        }

    if context.identities.has(instance_id):
        return {
            "type": type_name,
            "ref": int(context.identities[instance_id])
        }

    var identity: int = context.next_identity

    context.next_identity += 1
    context.resource_count += 1
    context.identities[instance_id] = identity

    var result := {
        "type": type_name,
        "id": identity
    }

    if depth >= MAX_RESOURCE_DEPTH or context.resource_count > MAX_RESOURCE_NODES:
        result["truncated"] = true
        return result

    context.active[instance_id] = true

    var properties := {}
    var names := _serializable_property_names(resource)

    for index in names.size():
        if index >= MAX_RESOURCE_PROPERTIES:
            result["truncated"] = true
            break

        var property_name: String = names[index]

        properties[property_name] = serialize(
            resource.get(property_name),
            path_overrides,
            context,
            depth + 1
        )

    context.active.erase(instance_id)
    result["properties"] = properties

    return result

static func _serializable_property_names(resource: Resource) -> Array:
    var names: Array = []

    for property in resource.get_property_list():
        var usage: int = property.get("usage", 0)

        if (usage & PROPERTY_USAGE_EDITOR) == 0:
            continue

        if int(property.get("type", TYPE_NIL)) == TYPE_NIL:
            continue

        names.append(str(property.get("name", "")))

    names.sort()

    return names
