package addon_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davidherring123/godot-mcp/internal/addon"
)

// TestPropertyCodecTypedResourceArrayUndoRestores exercises the shared codec on
// real native typed arrays and checks that one UndoRedo property step restores
// the original empty arrays and replays the decoded typed arrays on redo.
func TestPropertyCodecTypedResourceArrayUndoRestores(t *testing.T) {
	godot := findGodot(t)
	root := t.TempDir()

	if err := os.WriteFile(
		filepath.Join(root, "project.godot"),
		[]byte("config_version=5\n\n[application]\n\nconfig/name=\"TypedArrayCodecTest\"\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := addon.Install(root); err != nil {
		t.Fatalf("installing addon: %v", err)
	}

	scripts := map[string]string{
		"typed_item.gd": `@tool
extends Resource

class_name TypedItem

@export var label: String = ""
`,
		"typed_holder.gd": `@tool
extends Resource

const ItemScript = preload("res://typed_item.gd")

@export var items: Array[Resource] = []
@export var typed_items: Array[ItemScript] = []
`,
	}

	for name, contents := range scripts {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}

	scan := exec.Command(godot, "--headless", "--editor", "--path", root, "--quit-after", "2")
	scan.Dir = root

	if output, err := scan.CombinedOutput(); err != nil {
		t.Fatalf("scanning typed array classes: %v\n%s", err, output)
	}

	if err := os.WriteFile(
		filepath.Join(root, "typed_array_codec_test.gd"),
		[]byte(typedArrayCodecScript),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(
		godot,
		"--headless",
		"--path",
		root,
		"--script",
		"res://typed_array_codec_test.gd",
	)

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("typed array codec script failed: %v\n%s", err, output)
	}

	if !strings.Contains(string(output), "TYPED_ARRAY_CODEC_OK") {
		t.Fatalf("typed array codec script did not report success:\n%s", output)
	}
}

const typedArrayCodecScript = `extends SceneTree

const Codec = preload("res://addons/godot_mcp/core/property_codec.gd")
const TypedItem = preload("res://typed_item.gd")
const TypedHolder = preload("res://typed_holder.gd")

var failures: Array[String] = []

func _initialize() -> void:
    var holder := TypedHolder.new()

    var empty = Codec.decode_properties(holder, {"items": []})

    if empty.error != null:
        print("TYPED_ARRAY_CODEC_FAILURE: empty typed array decode failed: %s" % empty.error.message)
        quit(1)
        return

    _check(empty.converted["items"] is Array, "empty typed array was not an array")
    _check(
        empty.converted["items"].get_typed_builtin() == TYPE_OBJECT,
        "empty typed array lost its object element type"
    )
    _check(
        empty.converted["items"].get_typed_class_name() == &"Resource",
        "empty typed array lost its Resource class"
    )

    var decoded = Codec.decode_properties(holder, {
        "items": [
            {"type": "BoxMesh", "properties": {"size": {"x": 2, "y": 3, "z": 4}}},
            null,
        ],
        "typed_items": [
            {"type": "TypedItem", "properties": {"label": "inline"}},
            null,
        ],
    })

    if decoded.error != null:
        print("TYPED_ARRAY_CODEC_FAILURE: typed array decode failed: %s" % decoded.error.message)
        quit(1)
        return

    var items: Array = decoded.converted["items"]
    var typed_items: Array = decoded.converted["typed_items"]

    _check(items.size() == 2, "decoded resource array size was wrong")
    _check(items[0] is BoxMesh, "inline BoxMesh was not decoded")
    _check(items[1] == null, "null resource array element was not preserved")
    _check(
        items.get_typed_builtin() == TYPE_OBJECT,
        "decoded resource array lost its object element type"
    )
    _check(
        items.get_typed_class_name() == &"Resource",
        "decoded resource array lost its Resource class"
    )

    _check(typed_items.size() == 2, "decoded custom array size was wrong")
    _check(typed_items[0] is TypedItem, "inline TypedItem was not decoded")
    _check(typed_items[1] == null, "null custom array element was not preserved")
    _check(
        typed_items.get_typed_script() == TypedItem,
        "decoded custom array lost its typed script"
    )

    var incompatible = Codec.decode_properties(holder, {
        "typed_items": [{"type": "BoxMesh"}],
    })

    _check(incompatible.error != null, "incompatible typed array element was accepted")

    var undo_redo := UndoRedo.new()

    undo_redo.create_action("typed array", UndoRedo.MERGE_DISABLE)

    for property_name in Codec.property_order(decoded.converted):
        undo_redo.add_do_property(holder, property_name, decoded.converted[property_name])
        undo_redo.add_undo_property(holder, property_name, decoded.previous[property_name])

    undo_redo.commit_action()

    _check(holder.items.size() == 2, "typed resource array was not applied")
    _check(holder.typed_items.size() == 2, "custom typed array was not applied")

    var applied_item = holder.typed_items[0]

    undo_redo.undo()

    _check(holder.items.is_empty(), "undo did not restore the empty resource array")
    _check(holder.typed_items.is_empty(), "undo did not restore the empty custom array")
    _check(
        holder.items.get_typed_builtin() == TYPE_OBJECT,
        "undo lost the restored resource array element type"
    )
    _check(
        holder.typed_items.get_typed_script() == TypedItem,
        "undo lost the restored custom array typed script"
    )

    undo_redo.redo()

    _check(holder.items.size() == 2, "redo did not restore the resource array")
    _check(holder.typed_items.size() == 2, "redo did not restore the custom array")
    _check(holder.items[0] is BoxMesh, "redo did not restore the inline resource")
    _check(holder.typed_items[0] == applied_item, "redo did not restore resource identity")
    _check(
        holder.typed_items.get_typed_script() == TypedItem,
        "redo lost the custom array typed script"
    )

    if failures.is_empty():
        print("TYPED_ARRAY_CODEC_OK")
        quit(0)
        return

    for failure in failures:
        print("TYPED_ARRAY_CODEC_FAILURE: %s" % failure)

    quit(1)

func _check(condition: bool, message: String) -> void:
    if not condition:
        failures.append(message)
`
