@tool
extends Node3D

func _ready() -> void:
    var box := get_node_or_null("Box") as MeshInstance3D

    if box == null:
        return

    var material := StandardMaterial3D.new()
    material.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
    material.cull_mode = BaseMaterial3D.CULL_DISABLED
    material.albedo_color = Color(0.1, 0.15, 0.95, 1.0)
    box.material_override = material
