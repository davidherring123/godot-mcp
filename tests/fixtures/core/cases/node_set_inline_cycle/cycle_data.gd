class_name CycleData
extends Resource

@export var label: String = "cycle"
@export var next: Resource

@export var loop: bool = false:
    set(value):
        loop = value
        if value:
            next = self
        elif next == self:
            next = null
