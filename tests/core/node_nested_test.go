package core_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/davidherring123/godot-mcp/internal/adapter/bridge"
	"github.com/davidherring123/godot-mcp/internal/core/node"
	"github.com/davidherring123/godot-mcp/internal/core/resource"
	"github.com/davidherring123/godot-mcp/internal/core/scene"
	"github.com/davidherring123/godot-mcp/internal/protocol/apierr"
	"github.com/davidherring123/godot-mcp/tests/harness"
)

const nestedUniform = "shader_parameter/curvature"

func TestNodeSetNestedPropertiesSucceeds(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/node_set_nested/main.tscn"

	openFixtureScene(t, session, path)

	result, err := bridge.Call[node.SetParams, node.SetResult](
		session.Client,
		node.SetName,
		node.SetParams{
			Node:          "Actor",
			ExpectedScene: path,
			Save:          true,
			Properties: map[string]json.RawMessage{
				"position:x":                json.RawMessage(`7.5`),
				"material:" + nestedUniform: json.RawMessage(`0.8`),
			},
		},
	)
	if err != nil {
		t.Fatalf("expected node.set with nested paths to succeed: %v", err)
	}

	if !result.Changed {
		t.Fatal("expected the nested edit to report a change")
	}

	if !result.Saved {
		t.Fatal("expected the nested edit to save the scene")
	}

	assertJSONNumber(t, result.Properties, "position:x", 7.5)
	assertJSONNumber(t, result.Properties, "material:"+nestedUniform, 0.8)

	position := inspectorProperty(t, session, path, "Actor", "position")

	var vector struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}

	if err := json.Unmarshal(position.Value, &vector); err != nil {
		t.Fatalf("decoding Actor position: %v", err)
	}

	if vector.X != 7.5 || vector.Y != 2 {
		t.Fatalf("expected Actor position (7.5, 2), got %+v", vector)
	}

	if got := materialUniform(t, session, path, "Other"); got != 0.25 {
		t.Fatalf("nested write mutated the shared material: curvature = %v", got)
	}

	saved := readSavedScene(t, session, path)

	for _, expected := range []string{
		"position = Vector2(7.5, 2)",
		"shader_parameter/curvature = 0.25",
		"shader_parameter/curvature = 0.8",
	} {
		if !strings.Contains(saved, expected) {
			t.Fatalf("expected the saved scene to contain %q:\n%s", expected, saved)
		}
	}

	if err := session.LogJSON(node.SetName, result); err != nil {
		t.Fatal(err)
	}
}

func TestNodeSetNestedUnknownAndConflictingPathsRejected(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/node_set_nested_invalid/main.tscn"

	openFixtureScene(t, session, path)

	for name, properties := range map[string]map[string]json.RawMessage{
		"unknown root": {
			"missing:x": json.RawMessage(`1`),
		},
		"unknown component": {
			"position:q": json.RawMessage(`1`),
		},
		"unknown resource property": {
			"material:definitely_missing": json.RawMessage(`1`),
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := bridge.Call[node.SetParams, node.SetResult](
				session.Client,
				node.SetName,
				node.SetParams{
					Node:          "Actor",
					ExpectedScene: path,
					Properties:    properties,
				},
			)

			assertAPIErrorCode(t, err, apierr.CodeNotFound)
		})
	}

	_, err := bridge.Call[node.SetParams, node.SetResult](
		session.Client,
		node.SetName,
		node.SetParams{
			Node:          "Actor",
			ExpectedScene: path,
			Properties: map[string]json.RawMessage{
				"position":   json.RawMessage(`{"x":3,"y":4}`),
				"position:x": json.RawMessage(`5`),
			},
		},
	)

	assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)
	assertErrorMessageContains(t, err, "Conflicting property paths")

	for name, properties := range map[string]map[string]json.RawMessage{
		"invalid vector leaf": {
			"position:x": json.RawMessage(`"not a number"`),
		},
		"invalid resource leaf": {
			"material:" + nestedUniform: json.RawMessage(`"not a number"`),
		},
		"invalid resource leaf is atomic": {
			"position:x":                json.RawMessage(`5`),
			"material:" + nestedUniform: json.RawMessage(`"not a number"`),
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := bridge.Call[node.SetParams, node.SetResult](
				session.Client,
				node.SetName,
				node.SetParams{
					Node:          "Actor",
					ExpectedScene: path,
					Properties:    properties,
				},
			)

			assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)
		})
	}

	position := inspectorProperty(t, session, path, "Actor", "position")

	var vector struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}

	if err := json.Unmarshal(position.Value, &vector); err != nil {
		t.Fatalf("decoding Actor position: %v", err)
	}

	if vector.X != 1 || vector.Y != 2 {
		t.Fatalf("conflicting request changed Actor position to %+v", vector)
	}

	if got := materialUniform(t, session, path, "Actor"); got != 0.25 {
		t.Fatalf("conflicting request changed the material curvature to %v", got)
	}
}

func TestNodeAddNestedAndInlinePropertiesSucceeds(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/node_set_inline_resources/main.tscn"

	openFixtureScene(t, session, path)

	result, err := bridge.Call[node.AddParams, node.AddResult](
		session.Client,
		node.AddName,
		node.AddParams{
			Type:          "MeshInstance3D",
			Parent:        ".",
			Name:          "NestedAdded",
			ExpectedScene: path,
			Properties: map[string]json.RawMessage{
				"position:x": json.RawMessage(`4`),
				"mesh": json.RawMessage(
					`{"type":"BoxMesh","properties":{"size":{"x":2,"y":3,"z":4}}}`,
				),
			},
		},
	)
	if err != nil {
		t.Fatalf("expected node.add with nested and inline properties to succeed: %v", err)
	}

	assertJSONNumber(t, result.Properties, "position:x", 4)

	var mesh map[string]any

	if err := json.Unmarshal(result.Properties["mesh"], &mesh); err != nil {
		t.Fatalf("decoding added mesh property: %v", err)
	}

	if mesh["type"] != "BoxMesh" {
		t.Fatalf("expected an inline BoxMesh, got %v", mesh["type"])
	}
}

func TestSceneApplyNestedPropertiesSucceeds(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/scene_apply_nested/main.tscn"

	openFixtureScene(t, session, path)

	result := applyScene(t, session, scene.ApplyParams{
		ExpectedScene: path,
		Save:          true,
		Operations: []scene.ApplyOperation{
			{
				Operation: "node.set",
				Node:      "Actor",
				Properties: map[string]json.RawMessage{
					"position:x":                json.RawMessage(`7.5`),
					"material:" + nestedUniform: json.RawMessage(`0.8`),
				},
			},
		},
	})

	if !result.Applied || !result.Saved {
		t.Fatalf("expected the nested batch to apply and save: %+v", result)
	}

	if result.Summary.Changed != 1 {
		t.Fatalf("expected one changed operation, got %+v", result.Summary)
	}

	if got := materialUniform(t, session, path, "Other"); got != 0.25 {
		t.Fatalf("nested batch mutated the shared material: curvature = %v", got)
	}

	saved := readSavedScene(t, session, path)

	for _, expected := range []string{
		"position = Vector2(7.5, 2)",
		"shader_parameter/curvature = 0.25",
		"shader_parameter/curvature = 0.8",
	} {
		if !strings.Contains(saved, expected) {
			t.Fatalf("expected the saved scene to contain %q:\n%s", expected, saved)
		}
	}
}

func TestSceneApplyNestedDryRunDoesNotMutateSharedMaterial(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/scene_apply_nested_dry_run/main.tscn"

	openFixtureScene(t, session, path)

	result := applyScene(t, session, scene.ApplyParams{
		ExpectedScene: path,
		DryRun:        true,
		Operations: []scene.ApplyOperation{
			{
				Operation: "node.set",
				Node:      "Actor",
				Properties: map[string]json.RawMessage{
					"position:x":                json.RawMessage(`9`),
					"material:" + nestedUniform: json.RawMessage(`0.9`),
				},
			},
		},
	})

	if result.Applied || !result.DryRun {
		t.Fatalf("unexpected dry-run result: %+v", result)
	}

	if got := materialUniform(t, session, path, "Actor"); got != 0.25 {
		t.Fatalf("dry_run mutated the shared material: curvature = %v", got)
	}

	saveScene(t, session, path)
	saved := readSavedScene(t, session, path)

	if strings.Contains(saved, "shader_parameter/curvature = 0.9") {
		t.Fatalf("dry_run wrote the nested value into the saved scene:\n%s", saved)
	}

	if !strings.Contains(saved, "shader_parameter/curvature = 0.25") {
		t.Fatalf("dry_run changed the shared material:\n%s", saved)
	}
}

func TestSceneApplyNestedFailedPrevalidationDoesNotMutateSharedMaterial(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/scene_apply_nested_dry_run/main.tscn"

	openFixtureScene(t, session, path)

	_, err := bridge.Call[scene.ApplyParams, scene.ApplyResult](
		session.Client,
		scene.ApplyName,
		scene.ApplyParams{
			ExpectedScene: path,
			Operations: []scene.ApplyOperation{
				{
					Operation: "node.set",
					Node:      "Actor",
					Properties: map[string]json.RawMessage{
						"material:" + nestedUniform: json.RawMessage(`0.9`),
					},
				},
				{
					Operation: "node.set",
					Node:      "Actor",
					Properties: map[string]json.RawMessage{
						"position:missing": json.RawMessage(`1`),
					},
				},
			},
		},
	)

	assertAPIErrorCode(t, err, apierr.CodeNotFound)
	assertApplyErrorMessage(t, err, "Prevalidation failed", "operation 1")

	if got := materialUniform(t, session, path, "Actor"); got != 0.25 {
		t.Fatalf("failed prevalidation mutated the shared material: curvature = %v", got)
	}

	saveScene(t, session, path)
	saved := readSavedScene(t, session, path)

	if strings.Contains(saved, "shader_parameter/curvature = 0.9") {
		t.Fatalf("failed prevalidation wrote the nested value into the saved scene:\n%s", saved)
	}

	requireSceneUnsaved(t, session, path, false)
}

func TestSceneApplyNestedRollbackRestoresSharedMaterial(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/scene_apply_nested_rollback/main.tscn"

	openFixtureScene(t, session, path)
	requireSceneUnsaved(t, session, path, false)

	_, err := bridge.Call[scene.ApplyParams, scene.ApplyResult](
		session.Client,
		scene.ApplyName,
		scene.ApplyParams{
			ExpectedScene: path,
			Operations: []scene.ApplyOperation{
				{
					Operation: "node.set",
					Node:      "Actor",
					Properties: map[string]json.RawMessage{
						"position:x":                json.RawMessage(`9`),
						"material:" + nestedUniform: json.RawMessage(`0.9`),
					},
				},
				{
					Operation: "node.add",
					Type:      "Node2D",
					Parent:    ".",
					Name:      "RuntimeOnly",
				},
			},
		},
	)

	assertAPIErrorCode(t, err, apierr.CodeConflict)
	assertApplyErrorMessage(t, err, "Batch failed and was rolled back", "operation 1")

	if got := materialUniform(t, session, path, "Actor"); got != 0.25 {
		t.Fatalf("rollback did not restore the shared material: curvature = %v", got)
	}

	position := inspectorProperty(t, session, path, "Actor", "position")

	var vector struct {
		X float64 `json:"x"`
	}

	if err := json.Unmarshal(position.Value, &vector); err != nil {
		t.Fatalf("decoding restored position: %v", err)
	}

	if vector.X != 1 {
		t.Fatalf("rollback did not restore position.x, got %v", vector.X)
	}

	requireSceneUnsaved(t, session, path, false)

	saveScene(t, session, path)
	saved := readSavedScene(t, session, path)

	if strings.Contains(saved, "shader_parameter/curvature = 0.9") {
		t.Fatalf("rollback left the nested value in the scene:\n%s", saved)
	}

	if count := strings.Count(saved, "shader_parameter/curvature = 0.25"); count != 1 {
		t.Fatalf("expected the original shared material once, got %d:\n%s", count, saved)
	}
}

func materialUniform(
	t *testing.T,
	session *harness.Session,
	path string,
	nodePath string,
) float64 {
	t.Helper()

	property := inspectorProperty(t, session, path, nodePath, "material")

	var material struct {
		Resource   string                     `json:"resource"`
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
	}

	if err := json.Unmarshal(property.Value, &material); err != nil {
		t.Fatalf("decoding %s material: %v", nodePath, err)
	}

	if material.Type != "ShaderMaterial" {
		t.Fatalf("expected %s to use a ShaderMaterial, got %q", nodePath, material.Type)
	}

	raw, ok := material.Properties[nestedUniform]

	if !ok && material.Resource != "" {
		inspected, err := bridge.Call[resource.InspectParams, resource.InspectResult](
			session.Client,
			resource.InspectName,
			resource.InspectParams{
				Resource:   material.Resource,
				Properties: []string{nestedUniform},
			},
		)
		if err != nil {
			t.Fatalf("inspecting %s material %s: %v", nodePath, material.Resource, err)
		}

		for _, candidate := range inspected.Properties {
			if candidate.Name == nestedUniform {
				raw = candidate.Value
				ok = true
				break
			}
		}
	}

	if !ok {
		t.Fatalf("%s material has no %s property", nodePath, nestedUniform)
	}

	var value float64

	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decoding %s %s: %v", nodePath, nestedUniform, err)
	}

	return value
}

func assertJSONNumber(
	t *testing.T,
	properties map[string]json.RawMessage,
	name string,
	expected float64,
) {
	t.Helper()

	raw, ok := properties[name]

	if !ok {
		t.Fatalf("result is missing property %s: %v", name, properties)
	}

	var value float64

	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decoding %s: %v", name, err)
	}

	if value != expected {
		t.Fatalf("%s = %v; want %v", name, value, expected)
	}
}
