package core_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/davidherring123/godot-mcp/internal/adapter/bridge"
	"github.com/davidherring123/godot-mcp/internal/core/node"
	"github.com/davidherring123/godot-mcp/internal/core/resource"
	"github.com/davidherring123/godot-mcp/internal/core/scene"
	"github.com/davidherring123/godot-mcp/internal/protocol/apierr"
	"github.com/davidherring123/godot-mcp/tests/harness"
)

const typedArrayFixture = "res://cases/variant_typed_array"

func TestTypedResourceArrayAcceptsSavedInlineAndNull(t *testing.T) {
	session := requireSuite(t)
	scenePath := typedArrayFixture + "/main.tscn"

	openTypedArrayScene(t, session, scenePath)

	meshPath := typedArrayFixture + "/mesh.tres"
	result := setNodeProperties(t, session, scenePath, "Holder", map[string]json.RawMessage{
		"items": json.RawMessage(`[
			{"resource": "` + meshPath + `"},
			{"type": "BoxMesh", "properties": {"size": {"x": 2, "y": 3, "z": 4}}},
			null
		]`),
	})

	values := decodeJSONArray(t, result.Properties["items"])

	if len(values) != 3 {
		t.Fatalf("expected three typed resource array elements, got %s", result.Properties["items"])
	}

	saved := decodeInlineValue(t, values[0])

	if saved["resource"] != meshPath {
		t.Fatalf("expected the saved reference %s, got %v", meshPath, saved)
	}

	inline := decodeInlineValue(t, values[1])

	if inline["type"] != "BoxMesh" {
		t.Fatalf("expected an inline BoxMesh, got %v", inline)
	}

	size := inlineObject(t, inlineProperties(t, inline)["size"])

	if size["x"] != 2.0 || size["y"] != 3.0 || size["z"] != 4.0 {
		t.Fatalf("expected the inline BoxMesh size (2, 3, 4), got %v", size)
	}

	if strings.TrimSpace(string(values[2])) != "null" {
		t.Fatalf("expected a null element, got %s", values[2])
	}

	reloadScene(t, session, scene.ReloadParams{
		ExpectedScene:  scenePath,
		DiscardUnsaved: true,
	})
}

func TestTypedResourceArrayAcceptsEmptyAndThenTypedElements(t *testing.T) {
	session := requireSuite(t)
	scenePath := typedArrayFixture + "/main.tscn"

	openTypedArrayScene(t, session, scenePath)

	empty, err := bridge.Call[node.SetParams, node.SetResult](
		session.Client,
		node.SetName,
		node.SetParams{
			Node:          "Holder",
			ExpectedScene: scenePath,
			Properties: map[string]json.RawMessage{
				"items": json.RawMessage(`[]`),
			},
		},
	)
	if err != nil {
		t.Fatalf("expected an empty typed resource array to be accepted: %v", err)
	}

	if empty.Changed {
		t.Fatal("assigning an empty array to an empty typed resource array reported a change")
	}

	values := decodeJSONArray(t, empty.Properties["items"])

	if len(values) != 0 {
		t.Fatalf("expected an empty array readback, got %s", empty.Properties["items"])
	}

	result := setNodeProperties(t, session, scenePath, "Holder", map[string]json.RawMessage{
		"items": json.RawMessage(`[{"type": "BoxMesh"}]`),
	})

	values = decodeJSONArray(t, result.Properties["items"])

	if len(values) != 1 || decodeInlineValue(t, values[0])["type"] != "BoxMesh" {
		t.Fatalf("expected the typed array to accept an element after an empty assignment, got %s", result.Properties["items"])
	}

	reloadScene(t, session, scene.ReloadParams{
		ExpectedScene:  scenePath,
		DiscardUnsaved: true,
	})
}

func TestTypedCustomClassAndScriptArrays(t *testing.T) {
	session := requireSuite(t)
	scenePath := typedArrayFixture + "/custom.tscn"

	openTypedArrayScene(t, session, scenePath)

	typedPath := typedArrayFixture + "/typed_item.tres"
	scriptPath := typedArrayFixture + "/script_item.tres"
	texturePath := typedArrayFixture + "/texture.tres"

	result := setNodeProperties(t, session, scenePath, "Holder", map[string]json.RawMessage{
		"typed_items": json.RawMessage(`[
			{"resource": "` + typedPath + `"},
			{"type": "TypedItem", "properties": {"label": "inline", "amount": 3}},
			null
		]`),
		"script_items": json.RawMessage(`[
			{"resource": "` + scriptPath + `"},
			null
		]`),
		"textures": json.RawMessage(`[
			{"resource": "` + texturePath + `"},
			{"type": "PlaceholderTexture2D", "properties": {"size": {"x": 8, "y": 8}}}
		]`),
	})

	typedItems := decodeJSONArray(t, result.Properties["typed_items"])

	if len(typedItems) != 3 {
		t.Fatalf("expected three custom typed array elements, got %s", result.Properties["typed_items"])
	}

	savedItem := decodeInlineValue(t, typedItems[0])

	if savedItem["resource"] != typedPath || savedItem["type"] != "TypedItem" {
		t.Fatalf("expected a saved TypedItem reference, got %v", savedItem)
	}

	inlineItem := decodeInlineValue(t, typedItems[1])

	if inlineItem["type"] != "TypedItem" {
		t.Fatalf("expected an inline TypedItem, got %v", inlineItem)
	}

	properties := inlineProperties(t, inlineItem)

	if properties["label"] != "inline" || properties["amount"] != 3.0 {
		t.Fatalf("unexpected inline TypedItem properties: %v", properties)
	}

	if strings.TrimSpace(string(typedItems[2])) != "null" {
		t.Fatalf("expected a null custom typed array element, got %s", typedItems[2])
	}

	scriptItems := decodeJSONArray(t, result.Properties["script_items"])

	if len(scriptItems) != 2 {
		t.Fatalf("expected two script-typed array elements, got %s", result.Properties["script_items"])
	}

	if decodeInlineValue(t, scriptItems[0])["resource"] != scriptPath {
		t.Fatalf("expected the script-typed saved reference %s, got %s", scriptPath, scriptItems[0])
	}

	if strings.TrimSpace(string(scriptItems[1])) != "null" {
		t.Fatalf("expected a null script-typed array element, got %s", scriptItems[1])
	}

	textures := decodeJSONArray(t, result.Properties["textures"])

	if len(textures) != 2 {
		t.Fatalf("expected two texture array elements, got %s", result.Properties["textures"])
	}

	if decodeInlineValue(t, textures[0])["resource"] != texturePath {
		t.Fatalf("expected the saved texture reference %s, got %s", texturePath, textures[0])
	}

	inlineTexture := decodeInlineValue(t, textures[1])

	if inlineTexture["type"] != "PlaceholderTexture2D" {
		t.Fatalf("expected an inline PlaceholderTexture2D, got %v", inlineTexture)
	}

	textureSize := inlineObject(t, inlineProperties(t, inlineTexture)["size"])

	if textureSize["x"] != 8.0 || textureSize["y"] != 8.0 {
		t.Fatalf("expected the inline texture size (8, 8), got %v", textureSize)
	}

	if _, err := bridge.Call[node.SetParams, node.SetResult](
		session.Client,
		node.SetName,
		node.SetParams{
			Node:          "Holder",
			ExpectedScene: scenePath,
			Properties: map[string]json.RawMessage{
				"typed_items": result.Properties["typed_items"],
				"textures":    result.Properties["textures"],
			},
		},
	); err != nil {
		t.Fatalf("expected the custom typed array readback to be reusable: %v", err)
	}

	reloadScene(t, session, scene.ReloadParams{
		ExpectedScene:  scenePath,
		DiscardUnsaved: true,
	})
}

func TestTypedResourceArrayRejectsIncompatibleValuesBeforeMutation(t *testing.T) {
	session := requireSuite(t)
	scenePath := typedArrayFixture + "/rejections.tscn"

	openTypedArrayScene(t, session, scenePath)

	meshPath := typedArrayFixture + "/mesh.tres"
	victimPath := typedArrayFixture + "/victim.tres"

	cases := []struct {
		name       string
		property   string
		value      json.RawMessage
		code       apierr.Code
		errorMatch []string
	}{
		{
			name:     "incompatible saved reference",
			property: "typed_items",
			value: json.RawMessage(`[
				{"resource": "` + meshPath + `"}
			]`),
			code:       apierr.CodeInvalidArgument,
			errorMatch: []string{"element 0", "Expected TypedItem"},
		},
		{
			name:     "incompatible inline resource",
			property: "typed_items",
			value: json.RawMessage(`[
				{"type": "BoxMesh"}
			]`),
			code:       apierr.CodeInvalidArgument,
			errorMatch: []string{"element 0", "Expected TypedItem"},
		},
		{
			name:       "scalar element",
			property:   "typed_items",
			value:      json.RawMessage(`[7]`),
			code:       apierr.CodeInvalidArgument,
			errorMatch: []string{"element 0", "expected null"},
		},
		{
			name:     "incompatible native subclass",
			property: "textures",
			value: json.RawMessage(`[
				{"type": "BoxMesh"}
			]`),
			code:       apierr.CodeInvalidArgument,
			errorMatch: []string{"element 0", "Expected Texture2D"},
		},
		{
			name:     "graph marker",
			property: "items",
			value: json.RawMessage(`[
				{"type": "BoxMesh", "ref": 1}
			]`),
			code:       apierr.CodeInvalidArgument,
			errorMatch: []string{"element 0", "graph is incomplete", "ref"},
		},
		{
			name:     "ambiguous reference and inline body",
			property: "items",
			value: json.RawMessage(`[
				{"resource": "` + meshPath + `", "type": "BoxMesh", "properties": {"size": {"x": 9, "y": 9, "z": 9}}}
			]`),
			code:       apierr.CodeInvalidArgument,
			errorMatch: []string{"element 0", "either"},
		},
		{
			name:     "inline resource path claim",
			property: "items",
			value: json.RawMessage(`[
				{"type": "BoxMesh", "properties": {"resource_path": "` + victimPath + `"}}
			]`),
			code:       apierr.CodeInvalidArgument,
			errorMatch: []string{"element 0", "resource_path"},
		},
		{
			name:       "inline resource depth",
			property:   "typed_items",
			value:      typedItemArrayAtDepth(t, 20),
			code:       apierr.CodeInvalidArgument,
			errorMatch: []string{"element 0", "maximum depth"},
		},
		{
			name:     "non-resource typed array",
			property: "nodes",
			value: json.RawMessage(`[
				{"type": "BoxMesh"}
			]`),
			code:       apierr.CodeUnsupported,
			errorMatch: []string{"only Resource element types"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			before := inspectorProperty(t, session, scenePath, "Holder", testCase.property)

			_, err := bridge.Call[node.SetParams, node.SetResult](
				session.Client,
				node.SetName,
				node.SetParams{
					Node:          "Holder",
					ExpectedScene: scenePath,
					Save:          true,
					Properties: map[string]json.RawMessage{
						testCase.property: testCase.value,
					},
				},
			)

			assertAPIErrorCode(t, err, testCase.code)
			assertErrorMessageContains(t, err, testCase.errorMatch...)

			after := inspectorProperty(t, session, scenePath, "Holder", testCase.property)

			if !bytes.Equal(after.Value, before.Value) {
				t.Fatalf(
					"rejected typed array changed %s: before %s, after %s",
					testCase.property,
					before.Value,
					after.Value,
				)
			}

			requireSceneUnsaved(t, session, scenePath, false)
		})
	}

	reloadScene(t, session, scene.ReloadParams{
		ExpectedScene:  scenePath,
		DiscardUnsaved: true,
	})
}

func TestTypedResourceArrayPersistsAcrossSaveAndReload(t *testing.T) {
	session := requireSuite(t)
	scenePath := typedArrayFixture + "/persistence.tscn"

	openTypedArrayScene(t, session, scenePath)

	meshPath := typedArrayFixture + "/mesh.tres"

	_, err := bridge.Call[node.SetParams, node.SetResult](
		session.Client,
		node.SetName,
		node.SetParams{
			Node:          "Holder",
			ExpectedScene: scenePath,
			Save:          true,
			Properties: map[string]json.RawMessage{
				"items": json.RawMessage(`[
					{"resource": "` + meshPath + `"},
					{"type": "BoxMesh", "properties": {"size": {"x": 5, "y": 6, "z": 7}}},
					null
				]`),
				"typed_items": json.RawMessage(`[
					{"type": "TypedItem", "properties": {"label": "persisted", "amount": 4}},
					null
				]`),
			},
		},
	)
	if err != nil {
		t.Fatalf("expected typed resource array assignment with save to succeed: %v", err)
	}

	requireSceneUnsaved(t, session, scenePath, false)

	reloadScene(t, session, scene.ReloadParams{ExpectedScene: scenePath})

	reloaded := inspectorProperty(t, session, scenePath, "Holder", "items")
	values := decodeJSONArray(t, reloaded.Value)

	if len(values) != 3 {
		t.Fatalf("expected three persisted typed array elements, got %s", reloaded.Value)
	}

	if decodeInlineValue(t, values[0])["resource"] != meshPath {
		t.Fatalf("expected the saved reference to persist, got %s", values[0])
	}

	inline := decodeInlineValue(t, values[1])

	if inline["type"] != "BoxMesh" {
		t.Fatalf("expected the embedded BoxMesh to persist, got %v", inline)
	}

	boxAddress, embedded := inline["resource"].(string)

	if !embedded {
		t.Fatalf("expected the saved scene to embed the inline BoxMesh, got %v", inline)
	}

	persistedSize := decodeInlineValue(t, resourcePropertyValue(t, session, boxAddress, "size"))

	if persistedSize["x"] != 5.0 || persistedSize["y"] != 6.0 || persistedSize["z"] != 7.0 {
		t.Fatalf("expected the persisted size (5, 6, 7), got %v", persistedSize)
	}

	if strings.TrimSpace(string(values[2])) != "null" {
		t.Fatalf("expected the null element to persist, got %s", values[2])
	}

	typed := decodeJSONArray(t, inspectorProperty(t, session, scenePath, "Holder", "typed_items").Value)

	if len(typed) != 2 {
		t.Fatalf("expected two persisted custom typed array elements, got %d", len(typed))
	}

	persistedItem := decodeInlineValue(t, typed[0])

	if persistedItem["type"] != "TypedItem" {
		t.Fatalf("expected the embedded TypedItem to persist, got %v", persistedItem)
	}

	itemAddress, embedded := persistedItem["resource"].(string)

	if !embedded {
		t.Fatalf("expected the saved scene to embed the inline TypedItem, got %v", persistedItem)
	}

	if label := resourcePropertyValue(t, session, itemAddress, "label"); string(label) != `"persisted"` {
		t.Fatalf("expected the persisted TypedItem label, got %s", label)
	}

	if amount := resourcePropertyValue(t, session, itemAddress, "amount"); string(amount) != "4" {
		t.Fatalf("expected the persisted TypedItem amount 4, got %s", amount)
	}
}

func TestResourceApplyTypedArrayUsesBatchResourcesAndDryRunIsIsolated(t *testing.T) {
	session := requireSuite(t)
	holderPath := typedArrayFixture + "/batch_holder.tres"
	createdPath := typedArrayFixture + "/batch_created.tres"

	beforeBytes := readResourceFixture(t, session, holderPath)
	beforeItems := resourcePropertyValue(t, session, holderPath, "items")

	operations := []resource.ApplyOperation{
		{
			Operation: "create",
			Resource:  createdPath,
			Type:      "BoxMesh",
			Properties: map[string]json.RawMessage{
				"size": json.RawMessage(`{"x": 1, "y": 1, "z": 1}`),
			},
		},
		{
			Operation: "set",
			Resource:  holderPath,
			Properties: map[string]json.RawMessage{
				"items": json.RawMessage(`[
					{"resource": "` + createdPath + `"},
					{"type": "BoxMesh", "properties": {"size": {"x": 2, "y": 2, "z": 2}}},
					null
				]`),
			},
		},
	}

	dryRun, err := resource.Apply(session.Root(), resource.ApplyParams{
		Operations: operations,
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("expected a typed array resource dry run to succeed: %v", err)
	}

	if !dryRun.DryRun || dryRun.Applied || dryRun.Changed != 2 {
		t.Fatalf("unexpected typed array dry-run summary: %+v", dryRun)
	}

	if _, err := os.Stat(resourceFilesystemPath(session, createdPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry run wrote the batch-created resource: %v", err)
	}

	if afterBytes := readResourceFixture(t, session, holderPath); !bytes.Equal(afterBytes, beforeBytes) {
		t.Fatalf("dry run changed the typed array holder:\nbefore:\n%s\nafter:\n%s", beforeBytes, afterBytes)
	}

	if afterItems := resourcePropertyValue(t, session, holderPath, "items"); !bytes.Equal(afterItems, beforeItems) {
		t.Fatalf("dry run changed the loaded typed array: before %s, after %s", beforeItems, afterItems)
	}

	applied, err := resource.Apply(session.Root(), resource.ApplyParams{Operations: operations})
	if err != nil {
		t.Fatalf("expected a typed array resource apply to succeed: %v", err)
	}

	if !applied.Applied || applied.Changed != 2 {
		t.Fatalf("unexpected typed array apply summary: %+v", applied)
	}

	holderText := readResourceFile(t, session, holderPath)

	for _, expected := range []string{createdPath, "BoxMesh"} {
		if !strings.Contains(holderText, expected) {
			t.Fatalf("expected %q in the saved typed array holder:\n%s", expected, holderText)
		}
	}

	items := decodeJSONArray(t, resourcePropertyValue(t, session, holderPath, "items"))

	if len(items) != 3 {
		t.Fatalf("expected three saved typed array elements, got %d", len(items))
	}

	if decodeInlineValue(t, items[0])["resource"] != createdPath {
		t.Fatalf("expected the batch-created reference to persist, got %s", items[0])
	}

	if strings.TrimSpace(string(items[2])) != "null" {
		t.Fatalf("expected the saved null element, got %s", items[2])
	}
}

func TestSceneApplyTypedArrayDryRunAndPrevalidationAreIsolated(t *testing.T) {
	session := requireSuite(t)
	scenePath := typedArrayFixture + "/batch_scene.tscn"

	openTypedArrayScene(t, session, scenePath)

	before := inspectorProperty(t, session, scenePath, "Holder", "items").Value

	dryRun := applyScene(t, session, scene.ApplyParams{
		ExpectedScene: scenePath,
		DryRun:        true,
		Operations: []scene.ApplyOperation{
			{
				Operation: "node.set",
				Node:      "Holder",
				Properties: map[string]json.RawMessage{
					"items": json.RawMessage(`[
						{"type": "BoxMesh", "properties": {"size": {"x": 3, "y": 3, "z": 3}}}
					]`),
				},
			},
		},
	})

	if !dryRun.DryRun || dryRun.Applied {
		t.Fatalf("expected an unapplied typed array dry run, got %+v", dryRun)
	}

	if after := inspectorProperty(t, session, scenePath, "Holder", "items").Value; !bytes.Equal(after, before) {
		t.Fatalf("typed array dry run changed the live scene: before %s, after %s", before, after)
	}

	_, err := bridge.Call[scene.ApplyParams, scene.ApplyResult](
		session.Client,
		scene.ApplyName,
		scene.ApplyParams{
			ExpectedScene: scenePath,
			DryRun:        true,
			Operations: []scene.ApplyOperation{
				{
					Operation: "node.set",
					Node:      "Holder",
					Properties: map[string]json.RawMessage{
						"items": json.RawMessage(`[{"type": "BoxMesh"}]`),
					},
				},
				{
					Operation: "node.set",
					Node:      "Holder",
					Properties: map[string]json.RawMessage{
						"items": json.RawMessage(`[7]`),
					},
				},
			},
		},
	)
	assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)
	assertErrorMessageContains(t, err, "Prevalidation failed", "operation 1", "element 0")

	if after := inspectorProperty(t, session, scenePath, "Holder", "items").Value; !bytes.Equal(after, before) {
		t.Fatalf("failed typed array prevalidation changed the live scene: before %s, after %s", before, after)
	}

	requireSceneUnsaved(t, session, scenePath, false)

	applied := applyScene(t, session, scene.ApplyParams{
		ExpectedScene: scenePath,
		Operations: []scene.ApplyOperation{
			{
				Operation: "node.set",
				Node:      "Holder",
				Properties: map[string]json.RawMessage{
					"items": json.RawMessage(`[
						{"type": "BoxMesh", "properties": {"size": {"x": 4, "y": 4, "z": 4}}}
					]`),
				},
			},
		},
	})

	if !applied.Applied || applied.DryRun {
		t.Fatalf("expected the typed array batch to apply, got %+v", applied)
	}

	values := decodeJSONArray(t, inspectorProperty(t, session, scenePath, "Holder", "items").Value)

	if len(values) != 1 || decodeInlineValue(t, values[0])["type"] != "BoxMesh" {
		t.Fatalf("expected the applied typed array element, got %v", values)
	}

	requireSceneUnsaved(t, session, scenePath, true)

	reloadScene(t, session, scene.ReloadParams{
		ExpectedScene:  scenePath,
		DiscardUnsaved: true,
	})
}

func openTypedArrayScene(
	t *testing.T,
	session *harness.Session,
	path string,
) {
	t.Helper()

	openFixtureScene(t, session, path)
	reloadScene(t, session, scene.ReloadParams{
		ExpectedScene:  path,
		DiscardUnsaved: true,
	})
}

func decodeJSONArray(t *testing.T, data json.RawMessage) []json.RawMessage {
	t.Helper()

	var values []json.RawMessage

	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatalf("decoding JSON array %s: %v", data, err)
	}

	return values
}

func typedItemArrayAtDepth(t *testing.T, depth int) json.RawMessage {
	t.Helper()

	var value any = map[string]any{
		"type":       "TypedItem",
		"properties": map[string]any{"label": "leaf"},
	}

	for index := 0; index < depth; index++ {
		value = map[string]any{
			"type":       "TypedItem",
			"properties": map[string]any{"next": value},
		}
	}

	data, err := json.Marshal([]any{value})
	if err != nil {
		t.Fatalf("encoding a deeply nested typed array element: %v", err)
	}

	return data
}
