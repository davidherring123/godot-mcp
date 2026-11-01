class_name ResourceFactory
extends RefCounted

# Shared resource instantiation for resource.* actions and inline resource
# values. Global script classes are resolved through the project class list so
# custom Resource scripts run their constructors and property setters in both
# real writes and dry runs.
static func instantiate(type_name: String) -> Resource:
    if ClassDB.class_exists(type_name):
        if not ClassDB.can_instantiate(type_name):
            return null

        var instance = ClassDB.instantiate(type_name)

        if instance is Resource:
            return instance

        _free_instance(instance)

        return null

    for entry in ProjectSettings.get_global_class_list():
        if str(entry.get("class", "")) != type_name:
            continue

        if bool(entry.get("is_abstract", false)):
            return null

        var script: Script = load(entry.get("path", ""))

        if script == null:
            return null

        var instance = script.new()

        if instance is Resource:
            return instance

        _free_instance(instance)

        return null

    return null

static func type_name(resource: Resource) -> String:
    var script: Script = resource.get_script()

    while script != null:
        var global_name := str(script.get_global_name())

        if not global_name.is_empty():
            return global_name

        script = script.get_base_script()

    return resource.get_class()

static func _free_instance(instance: Variant) -> void:
    if instance is Node:
        instance.free()
