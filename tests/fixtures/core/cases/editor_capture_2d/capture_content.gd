@tool
extends Node2D

func _ready() -> void:
    queue_redraw()

func _draw() -> void:
    draw_rect(
        Rect2(Vector2(-10000.0, -10000.0), Vector2(20000.0, 20000.0)),
        Color(0.95, 0.12, 0.12, 1.0)
    )
