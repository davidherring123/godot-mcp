package core_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	invopop "github.com/invopop/jsonschema"

	"github.com/davidherring123/godot-mcp/internal/adapter/bridge"
	"github.com/davidherring123/godot-mcp/internal/core/node"
	"github.com/davidherring123/godot-mcp/internal/core/resource"
	"github.com/davidherring123/godot-mcp/internal/core/scene"
	"github.com/davidherring123/godot-mcp/internal/protocol/apierr"
	"github.com/davidherring123/godot-mcp/tests/harness"
)

func TestNodeSetInlineResourcesSavesWithoutExtraFiles(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/node_set_inline_resources/main.tscn"

	openFixtureScene(t, session, path)

	mesh := setNodeProperties(t, session, path, "Mesh", map[string]json.RawMessage{
		"mesh": json.RawMessage(`{
			"type": "BoxMesh",
			"properties": {
				"size": {"x": 1, "y": 2, "z": 3},
				"material": {
					"type": "StandardMaterial3D",
					"properties": {
						"albedo_color": {"r": 0.25, "g": 0.5, "b": 0.75, "a": 1}
					}
				}
			}
		}`),
	})

	meshValue := decodeInlineValue(t, mesh.Properties["mesh"])

	if meshValue["type"] != "BoxMesh" {
		t.Fatalf("expected an inline BoxMesh, got %v", meshValue)
	}

	if _, found := meshValue["resource"]; found {
		t.Fatalf("expected an anonymous inline mesh, got %v", meshValue)
	}

	meshProperties := inlineProperties(t, meshValue)
	size := inlineObject(t, meshProperties["size"])

	if size["x"] != 1.0 || size["y"] != 2.0 || size["z"] != 3.0 {
		t.Fatalf("expected size (1, 2, 3), got %v", size)
	}

	material := inlineObject(t, meshProperties["material"])

	if material["type"] != "StandardMaterial3D" {
		t.Fatalf("expected a nested inline material, got %v", material)
	}

	shape := setNodeProperties(t, session, path, "Shape", map[string]json.RawMessage{
		"shape": json.RawMessage(`{
			"type": "SphereShape3D",
			"properties": {"radius": 0.75}
		}`),
	})

	shapeValue := decodeInlineValue(t, shape.Properties["shape"])

	if shapeValue["type"] != "SphereShape3D" {
		t.Fatalf("expected an inline SphereShape3D, got %v", shapeValue)
	}

	texture := setNodeProperties(t, session, path, "Sprite", map[string]json.RawMessage{
		"texture": json.RawMessage(`{
			"type": "PlaceholderTexture2D",
			"properties": {"size": {"x": 8, "y": 4}}
		}`),
	})

	textureValue := decodeInlineValue(t, texture.Properties["texture"])

	if textureValue["type"] != "PlaceholderTexture2D" {
		t.Fatalf("expected an inline PlaceholderTexture2D, got %v", textureValue)
	}

	saveScene(t, session, path)

	saved := readSavedScene(t, session, path)

	for _, expected := range []string{
		`[sub_resource type="BoxMesh"`,
		`[sub_resource type="StandardMaterial3D"`,
		`[sub_resource type="SphereShape3D"`,
		`[sub_resource type="PlaceholderTexture2D"`,
	} {
		if !strings.Contains(saved, expected) {
			t.Fatalf("expected %q in the saved scene:\n%s", expected, saved)
		}
	}

	if strings.Contains(saved, "[ext_resource") {
		t.Fatalf("inline resources must not create external files:\n%s", saved)
	}

	files := filesWithExtensions(t, session, path, ".tres", ".res")

	if len(files) != 0 {
		t.Fatalf("inline resources created extra resource files: %v", files)
	}

	boxMeshID := subResourceID(t, saved, "BoxMesh")

	reloadScene(t, session, scene.ReloadParams{ExpectedScene: path})

	inspected := inspectorProperty(t, session, path, "Mesh", "mesh")
	inspectedValue := decodeInlineValue(t, inspected.Value)

	expectedPath := path + "::" + boxMeshID

	if inspectedValue["resource"] != expectedPath {
		t.Fatalf(
			"expected the saved embedded mesh at %s, got %v",
			expectedPath,
			inspectedValue,
		)
	}

	if inspectedValue["type"] != "BoxMesh" {
		t.Fatalf("expected type BoxMesh, got %v", inspectedValue)
	}
}

func TestNodeSetInlineResourceFailureLeavesNodeUnchanged(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/node_set_inline_invalid/main.tscn"

	openFixtureScene(t, session, path)

	before := inspectorProperty(t, session, path, "Mesh", "mesh")

	_, err := bridge.Call[node.SetParams, node.SetResult](
		session.Client,
		node.SetName,
		node.SetParams{
			Node:          "Mesh",
			ExpectedScene: path,
			Properties: map[string]json.RawMessage{
				"position": json.RawMessage(`{"x": 4, "y": 5, "z": 6}`),
				"mesh": json.RawMessage(`{
					"type": "BoxMesh",
					"properties": {
						"material": {
							"type": "StandardMaterial3D",
							"properties": {"missing_property": 1}
						}
					}
				}`),
			},
		},
	)
	assertAPIErrorCode(t, err, apierr.CodeNotFound)

	after := inspectorProperty(t, session, path, "Mesh", "mesh")

	if string(before.Value) != string(after.Value) {
		t.Fatalf(
			"failed inline validation changed the live mesh: before %s, after %s",
			before.Value,
			after.Value,
		)
	}

	position := inspectorProperty(t, session, path, "Mesh", "position")

	if strings.Contains(string(position.Value), `"x":4`) {
		t.Fatalf("failed inline validation changed position: %s", position.Value)
	}

	_, err = bridge.Call[node.SetParams, node.SetResult](
		session.Client,
		node.SetName,
		node.SetParams{
			Node:          "Mesh",
			ExpectedScene: path,
			Properties: map[string]json.RawMessage{
				"mesh": json.RawMessage(`{"type": "SphereShape3D"}`),
			},
		},
	)
	assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)

	afterMismatch := inspectorProperty(t, session, path, "Mesh", "mesh")

	if string(before.Value) != string(afterMismatch.Value) {
		t.Fatalf(
			"type-mismatched inline resource changed the live mesh: before %s, after %s",
			before.Value,
			afterMismatch.Value,
		)
	}
}

func TestNodeInspectInlineResourceCycleIsBounded(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/node_set_inline_cycle/main.tscn"

	openFixtureScene(t, session, path)

	setNodeProperties(t, session, path, ".", map[string]json.RawMessage{
		"data": json.RawMessage(`{
			"type": "CycleData",
			"properties": {"loop": true}
		}`),
	})

	inspected := inspectorProperty(t, session, path, ".", "data")
	value := decodeInlineValue(t, inspected.Value)

	if value["type"] != "CycleData" {
		t.Fatalf("expected an inline CycleData, got %v", value)
	}

	properties := inlineProperties(t, value)
	next := inlineObject(t, properties["next"])

	if next["cycle"] != true {
		t.Fatalf("expected the self reference to be marked as a cycle, got %v", next)
	}

	if next["ref"] != value["id"] {
		t.Fatalf(
			"expected the cycle to reference identity %v, got %v",
			value["id"],
			next["ref"],
		)
	}

	if _, found := next["properties"]; found {
		t.Fatalf("cycle references must not recurse into properties: %v", next)
	}
}

func TestNodeSetInlineResourceDepthIsBounded(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/node_set_inline_cycle/main.tscn"

	openFixtureScene(t, session, path)

	value := map[string]any{
		"type":       "CycleData",
		"properties": map[string]any{"loop": false},
	}

	for depth := 0; depth < 20; depth++ {
		value = map[string]any{
			"type":       "CycleData",
			"properties": map[string]any{"next": value},
		}
	}

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encoding the deep inline resource: %v", err)
	}

	before := inspectorProperty(t, session, path, ".", "data")

	_, err = bridge.Call[node.SetParams, node.SetResult](
		session.Client,
		node.SetName,
		node.SetParams{
			Node:          ".",
			ExpectedScene: path,
			Properties: map[string]json.RawMessage{
				"data": data,
			},
		},
	)
	assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)

	if !strings.Contains(err.Error(), "maximum depth") {
		t.Fatalf("expected a bounded-depth error, got %v", err)
	}

	after := inspectorProperty(t, session, path, ".", "data")

	if string(before.Value) != string(after.Value) {
		t.Fatalf(
			"over-deep inline resource changed the live value: before %s, after %s",
			before.Value,
			after.Value,
		)
	}
}

func TestNodeInspectPackedVector2ArrayDefault(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/node_inspect_packed_default/main.tscn"

	openFixtureScene(t, session, path)

	result, err := bridge.Call[node.InspectParams, node.InspectResult](
		session.Client,
		node.InspectName,
		node.InspectParams{
			Node:          ".",
			Properties:    []string{"points"},
			ExpectedScene: path,
		},
	)
	if err != nil {
		t.Fatalf("expected node.inspect to succeed: %v", err)
	}

	if len(result.Properties) != 1 {
		t.Fatalf("expected exactly one filtered property, got %d", len(result.Properties))
	}

	property := result.Properties[0]

	if property.Name != "points" || property.Type != "PackedVector2Array" {
		t.Fatalf("unexpected packed property: %+v", property)
	}

	var points []struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}

	if err := json.Unmarshal(property.Value, &points); err != nil {
		t.Fatalf("decoding points: %v", err)
	}

	if len(points) != 2 {
		t.Fatalf("expected two points, got %+v", points)
	}

	if points[0].X != 1 || points[0].Y != 2 || points[1].X != 3 || points[1].Y != 4 {
		t.Fatalf("expected (1,2) and (3,4), got %+v", points)
	}
}

func TestResourceCreateInlinePropertiesSucceeds(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/resource_create_inline/mesh.tres"

	result := createResource(t, session, resource.CreateParams{
		Resource: path,
		Type:     "BoxMesh",
		Properties: map[string]json.RawMessage{
			"size": json.RawMessage(`{"x": 2, "y": 3, "z": 4}`),
			"material": json.RawMessage(`{
				"type": "StandardMaterial3D",
				"properties": {
					"albedo_color": {"r": 0.1, "g": 0.2, "b": 0.3, "a": 1}
				}
			}`),
		},
	})

	if result.Type != "BoxMesh" {
		t.Fatalf("expected type BoxMesh, got %s", result.Type)
	}

	text := readResourceFile(t, session, path)

	if strings.Contains(text, "[ext_resource") {
		t.Fatalf("inline material must not create an external file:\n%s", text)
	}

	if !strings.Contains(text, `[sub_resource type="StandardMaterial3D"`) {
		t.Fatalf("expected an embedded material subresource:\n%s", text)
	}

	files := resourceFiles(t, session, path)

	if len(files) != 1 || files[0] != filepath.Base(path) {
		t.Fatalf("inline material created extra files: %v", files)
	}

	materialID := subResourceID(t, text, "StandardMaterial3D")

	inspected := inspectResource(t, session, resource.InspectParams{
		Resource:   path,
		Properties: []string{"material"},
	})

	if len(inspected.Properties) != 1 {
		t.Fatalf("expected one inspected material property, got %d", len(inspected.Properties))
	}

	embeddedProperty := decodeInlineValue(t, inspected.Properties[0].Value)
	expectedAddress := path + "::" + materialID

	if embeddedProperty["resource"] != expectedAddress {
		t.Fatalf(
			"expected the embedded material at %s, got %v",
			expectedAddress,
			embeddedProperty,
		)
	}

	embedded := inspectResource(t, session, resource.InspectParams{
		Resource:   expectedAddress,
		Properties: []string{"albedo_color"},
	})

	if embedded.Type != "StandardMaterial3D" {
		t.Fatalf("expected embedded type StandardMaterial3D, got %s", embedded.Type)
	}

	if !embedded.Embedded || embedded.Source != "disk" || embedded.UnsavedChanges {
		t.Fatalf("unexpected embedded source metadata: %+v", embedded)
	}

	if len(embedded.Properties) != 1 || embedded.Properties[0].Name != "albedo_color" {
		t.Fatalf("unexpected embedded properties: %+v", embedded.Properties)
	}
}

func TestResourceSetInlineResourceSucceeds(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/resource_set_inline/mesh.tres"

	createResource(t, session, resource.CreateParams{
		Resource: path,
		Type:     "BoxMesh",
	})

	result := setResourceProperties(t, session, path, map[string]json.RawMessage{
		"material": json.RawMessage(`{
			"type": "StandardMaterial3D",
			"properties": {
				"albedo_color": {"r": 0.9, "g": 0.1, "b": 0.2, "a": 1}
			}
		}`),
	})

	if !result.Changed {
		t.Fatal("expected resource.set with an inline material to report a change")
	}

	text := readResourceFile(t, session, path)

	if !strings.Contains(text, `[sub_resource type="StandardMaterial3D"`) {
		t.Fatalf("expected an embedded material subresource:\n%s", text)
	}

	files := resourceFiles(t, session, path)

	if len(files) != 1 || files[0] != filepath.Base(path) {
		t.Fatalf("inline set created extra files: %v", files)
	}
}

func TestResourceCreateInlineTypeMismatchWritesNothing(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/resource_create_inline_invalid/mesh.tres"

	_, err := bridge.Call[resource.CreateParams, resource.CreateResult](
		session.Client,
		resource.CreateName,
		resource.CreateParams{
			Resource: path,
			Type:     "BoxMesh",
			Properties: map[string]json.RawMessage{
				"material": json.RawMessage(`{
					"type": "SphereShape3D",
					"properties": {"radius": 1}
				}`),
			},
		},
	)
	assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)

	if _, statErr := os.Stat(resourceFilesystemPath(session, path)); !os.IsNotExist(statErr) {
		t.Fatalf("failed inline create wrote %s: %v", path, statErr)
	}
}

func TestResourceInspectEmbeddedDistinguishesDiskAndLive(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/resource_inspect_embedded/main.tscn"
	address := path + "::BoxMesh_inlined"

	disk := inspectResource(t, session, resource.InspectParams{
		Resource:   address,
		Properties: []string{"size"},
	})

	if disk.Source != "disk" || !disk.Embedded || disk.UnsavedChanges {
		t.Fatalf("unexpected disk metadata: %+v", disk)
	}

	if disk.Type != "BoxMesh" {
		t.Fatalf("expected embedded BoxMesh, got %s", disk.Type)
	}

	openFixtureScene(t, session, path)

	live := inspectResource(t, session, resource.InspectParams{
		Resource:   address,
		Properties: []string{"size"},
	})

	if live.Source != "live" || !live.Embedded || live.UnsavedChanges {
		t.Fatalf("unexpected clean live metadata: %+v", live)
	}

	setNodeProperties(t, session, path, ".", map[string]json.RawMessage{
		"position": json.RawMessage(`{"x": 7, "y": 8, "z": 9}`),
	})

	unsaved := inspectResource(t, session, resource.InspectParams{
		Resource:   address,
		Properties: []string{"size"},
	})

	if unsaved.Source != "live" || !unsaved.UnsavedChanges {
		t.Fatalf("unexpected unsaved live metadata: %+v", unsaved)
	}
}

func TestResourceInspectRejectsWritableEmbeddedAddress(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/resource_set_succeeds/material.tres::Embedded"

	setResult, err := bridge.Call[resource.SetParams, resource.SetResult](
		session.Client,
		resource.SetName,
		resource.SetParams{
			Resource: path,
			Properties: map[string]json.RawMessage{
				"roughness": json.RawMessage(`0.5`),
			},
		},
	)
	if err == nil {
		t.Fatalf("expected resource.set to reject an embedded address: %+v", setResult)
	}

	assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)

	deleteResult, err := bridge.Call[resource.DeleteParams, resource.DeleteResult](
		session.Client,
		resource.DeleteName,
		resource.DeleteParams{Resource: path},
	)
	if err == nil {
		t.Fatalf("expected resource.delete to reject an embedded address: %+v", deleteResult)
	}

	assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)
}

func TestResourceApplyAcceptsQualifiedOperationAliases(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/resource_apply_alias/material.tres"

	applied, err := bridge.Call[resource.ApplyParams, resource.ApplyResult](
		session.Client,
		resource.ApplyName,
		resource.ApplyParams{
			Operations: []resource.ApplyOperation{
				{
					Operation: "resource.create",
					Resource:  path,
					Type:      "StandardMaterial3D",
					Properties: map[string]json.RawMessage{
						"albedo_color": json.RawMessage(
							`{"r":0.2,"g":0.4,"b":0.6,"a":1}`,
						),
					},
				},
				{
					Operation: "resource.set",
					Resource:  path,
					Properties: map[string]json.RawMessage{
						"roughness": json.RawMessage(`0.3`),
					},
				},
				{
					Operation: "resource.delete",
					Resource:  path,
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("expected qualified resource.apply aliases to succeed: %v", err)
	}

	if len(applied.Operations) != 3 {
		t.Fatalf("expected three operation summaries, got %+v", applied.Operations)
	}

	for index, expected := range []string{"create", "set", "delete"} {
		if applied.Operations[index].Operation != expected {
			t.Fatalf(
				"expected short operation %s at %d, got %+v",
				expected,
				index,
				applied.Operations[index],
			)
		}
	}

	if _, statErr := os.Stat(resourceFilesystemPath(session, path)); !os.IsNotExist(statErr) {
		t.Fatalf("expected the alias batch to delete %s: %v", path, statErr)
	}

	dryRun, err := resource.Apply(session.Root(), resource.ApplyParams{
		Operations: []resource.ApplyOperation{
			{
				Operation: "resource.create",
				Resource:  "res://cases/resource_apply_alias_dry/mesh.tres",
				Type:      "BoxMesh",
				Properties: map[string]json.RawMessage{
					"material": json.RawMessage(`{
						"type": "StandardMaterial3D",
						"properties": {"roughness": 0.2}
					}`),
				},
			},
		},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("expected a qualified dry run to pass Go validation: %v", err)
	}

	if len(dryRun.Operations) != 1 || dryRun.Operations[0].Operation != "create" {
		t.Fatalf("expected a short create summary, got %+v", dryRun.Operations)
	}

	if _, statErr := os.Stat(
		resourceFilesystemPath(session, "res://cases/resource_apply_alias_dry/mesh.tres"),
	); !os.IsNotExist(statErr) {
		t.Fatalf("dry run wrote the inline resource: %v", statErr)
	}
}

func TestResourceApplyOperationSchemaIncludesQualifiedAliases(t *testing.T) {
	reflector := invopop.Reflector{Anonymous: true, ExpandedStruct: true}
	schema := reflector.ReflectFromType(
		reflect.TypeFor[resource.ApplyParams](),
	)

	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("encoding the resource.apply schema: %v", err)
	}

	for _, operation := range []string{
		"create",
		"resource.create",
		"set",
		"resource.set",
		"delete",
		"resource.delete",
	} {
		if !strings.Contains(string(data), `"`+operation+`"`) {
			t.Fatalf("expected %q in the operation enum: %s", operation, data)
		}
	}
}

func decodeInlineValue(t *testing.T, data json.RawMessage) map[string]any {
	t.Helper()

	var value map[string]any

	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("decoding inline resource %s: %v", data, err)
	}

	return value
}

func inlineProperties(t *testing.T, value map[string]any) map[string]any {
	t.Helper()

	properties, ok := value["properties"].(map[string]any)

	if !ok {
		t.Fatalf("expected inline properties object, got %v", value)
	}

	return properties
}

func inlineObject(t *testing.T, value any) map[string]any {
	t.Helper()

	object, ok := value.(map[string]any)

	if !ok {
		t.Fatalf("expected an object, got %v", value)
	}

	return object
}

func filesWithExtensions(
	t *testing.T,
	session *harness.Session,
	scenePath string,
	extensions ...string,
) []string {
	t.Helper()

	directory := filepath.Dir(resourceFilesystemPath(session, scenePath))

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("reading %s: %v", directory, err)
	}

	var files []string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		for _, extension := range extensions {
			if filepath.Ext(entry.Name()) == extension {
				files = append(files, entry.Name())

				break
			}
		}
	}

	return files
}

func resourceFiles(t *testing.T, session *harness.Session, resourcePath string) []string {
	t.Helper()

	directory := filepath.Dir(resourceFilesystemPath(session, resourcePath))

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("reading %s: %v", directory, err)
	}

	var files []string

	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, entry.Name())
		}
	}

	return files
}

func subResourceID(t *testing.T, text string, resourceType string) string {
	t.Helper()

	pattern := regexp.MustCompile(
		`\[sub_resource type="` + regexp.QuoteMeta(resourceType) + `" id="([^"]+)"\]`,
	)
	match := pattern.FindStringSubmatch(text)

	if len(match) < 2 {
		t.Fatalf("expected a %s sub_resource in:\n%s", resourceType, text)
	}

	return match[1]
}
