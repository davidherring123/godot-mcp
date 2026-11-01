@tool
extends Node

const ScriptItem = preload("res://cases/variant_typed_array/script_item.gd")

@export var items: Array[Resource] = []
@export var textures: Array[Texture2D] = []
@export var typed_items: Array[TypedItem] = []
@export var script_items: Array[ScriptItem] = []
@export var nodes: Array[Node] = []
