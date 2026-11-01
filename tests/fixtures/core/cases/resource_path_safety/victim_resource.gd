@tool
extends Resource

# The getter exposes the loaded instance identity without storing it in the
# .tres file, so tests can tell whether the resource cache kept its identity.
@export var marker: String = "":
    get:
        return str(get_instance_id())
    set(_value):
        pass

@export var value: int = 7
