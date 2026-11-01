package core_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/davidherring123/godot-mcp/internal/adapter/bridge"
	"github.com/davidherring123/godot-mcp/internal/core/node"
	"github.com/davidherring123/godot-mcp/internal/core/resource"
	"github.com/davidherring123/godot-mcp/internal/core/scene"
	"github.com/davidherring123/godot-mcp/internal/protocol/apierr"
	"github.com/davidherring123/godot-mcp/tests/harness"
)

func TestInlineAndNestedPropertiesCannotClaimResourcePath(t *testing.T) {
	session := requireSuite(t)
	scenePath := "res://cases/resource_path_safety/main.tscn"
	victimPath := "res://cases/resource_path_safety/victim.tres"

	openFixtureScene(t, session, scenePath)

	beforeBytes := readResourceFixture(t, session, victimPath)
	beforeMarker := resourcePropertyValue(t, session, victimPath, "marker")
	beforeMesh := inspectorProperty(t, session, scenePath, "Mesh", "mesh").Value

	inline := json.RawMessage(
		`{"type":"BoxMesh","properties":{"resource_path":` +
			strconv.Quote(victimPath) + `}}`,
	)

	attempts := []struct {
		name       string
		properties map[string]json.RawMessage
	}{
		{
			name: "inline resource properties",
			properties: map[string]json.RawMessage{
				"mesh": inline,
			},
		},
		{
			name: "nested resource path",
			properties: map[string]json.RawMessage{
				"mesh:resource_path": json.RawMessage(strconv.Quote(victimPath)),
			},
		},
	}

	for _, attempt := range attempts {
		t.Run(attempt.name, func(t *testing.T) {
			_, err := bridge.Call[node.SetParams, node.SetResult](
				session.Client,
				node.SetName,
				node.SetParams{
					Node:          "Mesh",
					ExpectedScene: scenePath,
					Save:          true,
					Properties:    attempt.properties,
				},
			)

			assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)
			assertErrorMessageContains(t, err, "resource_path")

			assertFixtureUnchanged(t, session, victimPath, beforeBytes, beforeMarker)

			afterMesh := inspectorProperty(t, session, scenePath, "Mesh", "mesh").Value

			if !bytes.Equal(afterMesh, beforeMesh) {
				t.Fatalf(
					"rejected resource path claim changed the mesh: before %s, after %s",
					beforeMesh,
					afterMesh,
				)
			}

			requireSceneUnsaved(t, session, scenePath, false)
		})
	}

	saveScene(t, session, scenePath)
	assertFixtureUnchanged(t, session, victimPath, beforeBytes, beforeMarker)
}

func TestInlineResourceAllowsNonPathIdentityProperties(t *testing.T) {
	session := requireSuite(t)
	scenePath := "res://cases/resource_path_safety/main.tscn"

	openFixtureScene(t, session, scenePath)

	result := setNodeProperties(t, session, scenePath, "Mesh", map[string]json.RawMessage{
		"mesh": json.RawMessage(`{
			"type": "BoxMesh",
			"properties": {
				"size": {"x": 2, "y": 3, "z": 4},
				"resource_name": "kept_name",
				"resource_local_to_scene": true
			}
		}`),
	})

	value := decodeInlineValue(t, result.Properties["mesh"])
	properties := inlineProperties(t, value)

	if properties["resource_name"] != "kept_name" {
		t.Fatalf("expected resource_name to be kept, got %v", properties)
	}

	if properties["resource_local_to_scene"] != true {
		t.Fatalf("expected resource_local_to_scene to be kept, got %v", properties)
	}

	if path, found := properties["resource_path"]; found && path != "" {
		t.Fatalf("inline resource unexpectedly claimed a path: %v", path)
	}

	size := inlineObject(t, properties["size"])

	if size["x"] != 2.0 || size["y"] != 3.0 || size["z"] != 4.0 {
		t.Fatalf("expected size (2, 3, 4), got %v", size)
	}
}

func TestResourceSetAllowsUnchangedResourcePath(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/resource_path_safety/victim.tres"

	currentPath := resourcePropertyValue(t, session, path, "resource_path")

	result, err := bridge.Call[resource.SetParams, resource.SetResult](
		session.Client,
		resource.SetName,
		resource.SetParams{
			Resource: path,
			Properties: map[string]json.RawMessage{
				"resource_path": currentPath,
			},
		},
	)
	if err != nil {
		t.Fatalf("expected a resource to keep its current path: %v", err)
	}

	if result.Changed {
		t.Fatal("rewriting the current resource path reported a change")
	}
}

func TestResourceReferenceReadbackCanBeReapplied(t *testing.T) {
	session := requireSuite(t)
	scenePath := "res://cases/variant_reference_compat/main.tscn"
	meshPath := "res://cases/variant_reference_compat/mesh.tres"

	openFixtureScene(t, session, scenePath)

	inspected := inspectorProperty(t, session, scenePath, "Mesh", "mesh")

	var reference struct {
		Resource string `json:"resource"`
		Type     string `json:"type"`
	}

	if err := json.Unmarshal(inspected.Value, &reference); err != nil {
		t.Fatalf("decoding inspected mesh reference: %v", err)
	}

	if reference.Resource != meshPath || reference.Type != "BoxMesh" {
		t.Fatalf("unexpected reference readback: %+v", reference)
	}

	result, err := bridge.Call[node.SetParams, node.SetResult](
		session.Client,
		node.SetName,
		node.SetParams{
			Node:          "Mesh",
			ExpectedScene: scenePath,
			Properties: map[string]json.RawMessage{
				"mesh": inspected.Value,
			},
		},
	)
	if err != nil {
		t.Fatalf("expected a readback reference with an advisory type to be accepted: %v", err)
	}

	if result.Changed {
		t.Fatal("reapplying the same resource reference reported a change")
	}

	advisory := json.RawMessage(
		`{"resource":` + strconv.Quote(meshPath) +
			`,"type":"SphereShape3D"}`,
	)

	if _, err := bridge.Call[node.SetParams, node.SetResult](
		session.Client,
		node.SetName,
		node.SetParams{
			Node:          "Mesh",
			ExpectedScene: scenePath,
			Properties: map[string]json.RawMessage{
				"mesh": advisory,
			},
		},
	); err != nil {
		t.Fatalf("expected the reference type to be advisory: %v", err)
	}

	ambiguous := json.RawMessage(
		`{"resource":` + strconv.Quote(meshPath) +
			`,"type":"BoxMesh","properties":{"size":{"x":9,"y":9,"z":9}}}`,
	)

	_, err = bridge.Call[node.SetParams, node.SetResult](
		session.Client,
		node.SetName,
		node.SetParams{
			Node:          "Mesh",
			ExpectedScene: scenePath,
			Properties: map[string]json.RawMessage{
				"mesh": ambiguous,
			},
		},
	)
	assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)
	assertErrorMessageContains(t, err, "either")

	after := inspectorProperty(t, session, scenePath, "Mesh", "mesh")

	var afterReference struct {
		Resource string `json:"resource"`
	}

	if err := json.Unmarshal(after.Value, &afterReference); err != nil {
		t.Fatalf("decoding mesh reference after rejection: %v", err)
	}

	if afterReference.Resource != meshPath {
		t.Fatalf("ambiguous reference changed the mesh: %s", after.Value)
	}
}

func TestIncompleteResourceGraphsAreRejected(t *testing.T) {
	session := requireSuite(t)
	scenePath := "res://cases/variant_graph_safety/main.tscn"

	openFixtureScene(t, session, scenePath)

	before := inspectorProperty(t, session, scenePath, "Mesh", "mesh").Value

	cases := []struct {
		name       string
		value      json.RawMessage
		errorMatch string
	}{
		{
			name: "truncated inline graph",
			value: json.RawMessage(`{
				"type": "BoxMesh",
				"truncated": true,
				"properties": {"size": {"x": 2, "y": 2, "z": 2}}
			}`),
			errorMatch: "truncated",
		},
		{
			name: "nested reference marker",
			value: json.RawMessage(`{
				"type": "BoxMesh",
				"properties": {
					"material": {"type": "StandardMaterial3D", "ref": 1}
				}
			}`),
			errorMatch: "ref",
		},
		{
			name: "nested cycle marker",
			value: json.RawMessage(`{
				"type": "BoxMesh",
				"properties": {
					"material": {
						"type": "StandardMaterial3D",
						"cycle": true,
						"ref": 1
					}
				}
			}`),
			errorMatch: "cycle",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := bridge.Call[node.SetParams, node.SetResult](
				session.Client,
				node.SetName,
				node.SetParams{
					Node:          "Mesh",
					ExpectedScene: scenePath,
					Save:          true,
					Properties: map[string]json.RawMessage{
						"mesh": testCase.value,
					},
				},
			)

			assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)
			assertErrorMessageContains(t, err, testCase.errorMatch)

			after := inspectorProperty(t, session, scenePath, "Mesh", "mesh").Value

			if !bytes.Equal(after, before) {
				t.Fatalf(
					"rejected incomplete graph changed the mesh: before %s, after %s",
					before,
					after,
				)
			}

			requireSceneUnsaved(t, session, scenePath, false)
		})
	}
}

func TestIncompleteGraphDryRunIsRejectedWithoutMutation(t *testing.T) {
	session := requireSuite(t)
	scenePath := "res://cases/variant_graph_safety/main.tscn"

	openFixtureScene(t, session, scenePath)

	beforeMesh := inspectorProperty(t, session, scenePath, "Mesh", "mesh").Value
	beforeShape := inspectorProperty(t, session, scenePath, "Shape", "shape").Value

	_, err := bridge.Call[scene.ApplyParams, scene.ApplyResult](
		session.Client,
		scene.ApplyName,
		scene.ApplyParams{
			ExpectedScene: scenePath,
			DryRun:        true,
			Operations: []scene.ApplyOperation{
				{
					Operation: "node.set",
					Node:      "Mesh",
					Properties: map[string]json.RawMessage{
						"mesh": json.RawMessage(`{
							"type": "BoxMesh",
							"properties": {"size": {"x": 9, "y": 9, "z": 9}}
						}`),
					},
				},
				{
					Operation: "node.set",
					Node:      "Shape",
					Properties: map[string]json.RawMessage{
						"shape": json.RawMessage(`{
							"type": "SphereShape3D",
							"truncated": true
						}`),
					},
				},
			},
		},
	)
	assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)
	assertErrorMessageContains(t, err, "Prevalidation failed", "operation 1")

	afterMesh := inspectorProperty(t, session, scenePath, "Mesh", "mesh").Value
	afterShape := inspectorProperty(t, session, scenePath, "Shape", "shape").Value

	if !bytes.Equal(afterMesh, beforeMesh) {
		t.Fatalf("dry run changed the mesh: before %s, after %s", beforeMesh, afterMesh)
	}

	if !bytes.Equal(afterShape, beforeShape) {
		t.Fatalf("dry run changed the shape: before %s, after %s", beforeShape, afterShape)
	}

	requireSceneUnsaved(t, session, scenePath, false)
}

func TestCompleteInlineGraphReadbackWithIdentityIsAccepted(t *testing.T) {
	session := requireSuite(t)
	scenePath := "res://cases/variant_graph_safety/main.tscn"

	openFixtureScene(t, session, scenePath)

	result := setNodeProperties(t, session, scenePath, "Shape", map[string]json.RawMessage{
		"shape": json.RawMessage(`{
			"type": "SphereShape3D",
			"properties": {"radius": 2.5}
		}`),
	})

	readback := decodeInlineValue(t, result.Properties["shape"])

	if _, found := readback["id"]; !found {
		t.Fatalf("expected the anonymous graph readback to include an id: %v", readback)
	}

	replayed, err := bridge.Call[node.SetParams, node.SetResult](
		session.Client,
		node.SetName,
		node.SetParams{
			Node:          "Shape",
			ExpectedScene: scenePath,
			Properties: map[string]json.RawMessage{
				"shape": result.Properties["shape"],
			},
		},
	)
	if err != nil {
		t.Fatalf("expected a complete readback graph with an id to be accepted: %v", err)
	}

	value := decodeInlineValue(t, replayed.Properties["shape"])

	if value["type"] != "SphereShape3D" {
		t.Fatalf("expected SphereShape3D after replay, got %v", value)
	}

	radius := inlineProperties(t, value)["radius"]

	if radius != 2.5 {
		t.Fatalf("expected radius 2.5 after replay, got %v", radius)
	}
}

func TestTypedResourceArrayWritesInlineResources(t *testing.T) {
	session := requireSuite(t)
	scenePath := "res://cases/variant_typed_array/main.tscn"

	openFixtureScene(t, session, scenePath)

	result := setNodeProperties(t, session, scenePath, "Holder", map[string]json.RawMessage{
		"items": json.RawMessage(`[
			{"type": "BoxMesh", "properties": {"size": {"x": 1, "y": 2, "z": 3}}}
		]`),
	})

	var values []json.RawMessage

	if err := json.Unmarshal(result.Properties["items"], &values); err != nil {
		t.Fatalf("decoding typed resource array: %v", err)
	}

	if len(values) != 1 {
		t.Fatalf("expected one typed resource array element, got %s", result.Properties["items"])
	}

	value := decodeInlineValue(t, values[0])

	if value["type"] != "BoxMesh" {
		t.Fatalf("expected an inline BoxMesh element, got %v", value)
	}

	size := inlineObject(t, inlineProperties(t, value)["size"])

	if size["x"] != 1.0 || size["y"] != 2.0 || size["z"] != 3.0 {
		t.Fatalf("expected the inline element size (1, 2, 3), got %v", size)
	}

	requireSceneUnsaved(t, session, scenePath, true)

	reloadScene(t, session, scene.ReloadParams{
		ExpectedScene:  scenePath,
		DiscardUnsaved: true,
	})

	requireSceneUnsaved(t, session, scenePath, false)
}

func readResourceFixture(
	t *testing.T,
	session *harness.Session,
	path string,
) []byte {
	t.Helper()

	data, err := os.ReadFile(resourceFilesystemPath(session, path))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}

	return data
}

func resourcePropertyValue(
	t *testing.T,
	session *harness.Session,
	path string,
	name string,
) json.RawMessage {
	t.Helper()

	inspected := inspectResource(t, session, resource.InspectParams{
		Resource:   path,
		Properties: []string{name},
	})

	if len(inspected.Properties) != 1 {
		t.Fatalf("expected one %s property on %s, got %+v", name, path, inspected.Properties)
	}

	return inspected.Properties[0].Value
}

func assertFixtureUnchanged(
	t *testing.T,
	session *harness.Session,
	path string,
	beforeBytes []byte,
	beforeMarker json.RawMessage,
) {
	t.Helper()

	afterBytes := readResourceFixture(t, session, path)

	if !bytes.Equal(afterBytes, beforeBytes) {
		t.Fatalf("resource %s changed on disk:\nbefore:\n%s\nafter:\n%s", path, beforeBytes, afterBytes)
	}

	afterMarker := resourcePropertyValue(t, session, path, "marker")

	if !bytes.Equal(afterMarker, beforeMarker) {
		t.Fatalf(
			"resource %s cache identity changed: before %s, after %s",
			path,
			beforeMarker,
			afterMarker,
		)
	}
}
