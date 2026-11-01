package core_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davidherring123/godot-mcp/internal/adapter/bridge"
	"github.com/davidherring123/godot-mcp/internal/core/node"
	"github.com/davidherring123/godot-mcp/internal/core/scene"
	"github.com/davidherring123/godot-mcp/internal/protocol/apierr"
	"github.com/davidherring123/godot-mcp/tests/harness"
)

func TestSceneReloadReflectsExternalFileEdit(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/scene_reload/main.tscn"

	openFixtureScene(t, session, path)

	sceneFile := filepath.Join(
		session.Root(),
		"cases",
		"scene_reload",
		"main.tscn",
	)
	original, err := os.ReadFile(sceneFile)
	if err != nil {
		t.Fatalf("reading the scene fixture: %v", err)
	}

	externallyEdited := string(original) + `
[node name="ExternallyAdded" type="Marker2D" parent="."]
position = Vector2(11, 22)
`

	if err := os.WriteFile(sceneFile, []byte(externallyEdited), 0644); err != nil {
		t.Fatalf("writing the externally edited scene: %v", err)
	}

	result := reloadScene(t, session, scene.ReloadParams{ExpectedScene: path})

	if !result.Reloaded {
		t.Fatal("expected scene.reload to report a completed reload")
	}

	if result.Scene != path {
		t.Fatalf("expected scene %s, got %s", path, result.Scene)
	}

	if result.WasUnsaved || result.DiscardedUnsaved {
		t.Fatalf("expected a clean reload, got %+v", result)
	}

	if !result.UndoHistoryCleared {
		t.Fatal("expected scene.reload to report cleared undo history")
	}

	if result.StaleScripts == nil {
		t.Fatal("expected stale_scripts to be an empty array, not null")
	}

	if !strings.Contains(result.Note, "undo/redo history") {
		t.Fatalf("expected the result note to explain undo history: %s", result.Note)
	}

	if !strings.Contains(result.Note, "best effort") {
		t.Fatalf("expected the result note to bound stale script detection: %s", result.Note)
	}

	tree := readSceneTree(t, session, path)

	if _, found := findTreePath(tree.Root, "ExternallyAdded"); !found {
		t.Fatalf(
			"expected the reloaded scene to contain the externally added node:\n%+v",
			tree.Root,
		)
	}

	if _, found := findTreePath(tree.Root, "World/Marker"); !found {
		t.Fatal("expected the reloaded scene to keep its original nodes")
	}

	if err := session.LogJSON(scene.ReloadName, result); err != nil {
		t.Fatal(err)
	}
}

func TestSceneReloadReportsStaleScript(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/scene_reload_script/main.tscn"

	openFixtureScene(t, session, path)

	scriptFile := filepath.Join(
		session.Root(),
		"scripts",
		"reload_probe.gd",
	)
	original, err := os.ReadFile(scriptFile)
	if err != nil {
		t.Fatalf("reading the script fixture: %v", err)
	}

	externallyEdited := strings.Replace(
		string(original),
		"probe: int = 1",
		"probe: int = 2",
		1,
	)

	if externallyEdited == string(original) {
		t.Fatal("expected the script fixture to export probe = 1")
	}

	if err := os.WriteFile(scriptFile, []byte(externallyEdited), 0644); err != nil {
		t.Fatalf("writing the externally edited script: %v", err)
	}

	result := reloadScene(t, session, scene.ReloadParams{ExpectedScene: path})

	if !containsString(result.StaleScripts, "res://scripts/reload_probe.gd") {
		t.Fatalf(
			"expected the reload to report the stale script, got %v",
			result.StaleScripts,
		)
	}

	if err := session.LogJSON(scene.ReloadName, result); err != nil {
		t.Fatal(err)
	}
}

func TestSceneReloadRefusesDirtySceneByDefault(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/scene_reload/discard_refused.tscn"

	openFixtureScene(t, session, path)

	setNodeProperties(t, session, path, "World/Marker", map[string]json.RawMessage{
		"position": json.RawMessage(`{"x":99,"y":100}`),
	})

	requireUnsaved(t, session, path, true)

	_, err := bridge.Call[scene.ReloadParams, scene.ReloadResult](
		session.Client,
		scene.ReloadName,
		scene.ReloadParams{ExpectedScene: path},
	)
	assertAPIErrorCode(t, err, apierr.CodeFailedPrecondition)
	assertErrorMessageContains(t, err, "unsaved changes", "discard_unsaved=true")

	// A refused reload must leave the unsaved edit in place.
	requireUnsaved(t, session, path, true)
	requireVector2Position(t, session, path, "World/Marker", 99, 100)
}

func TestSceneReloadDiscardsUnsavedWhenRequested(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/scene_reload/discard_applied.tscn"

	openFixtureScene(t, session, path)

	setNodeProperties(t, session, path, "World/Marker", map[string]json.RawMessage{
		"position": json.RawMessage(`{"x":99,"y":100}`),
	})

	requireUnsaved(t, session, path, true)

	result := reloadScene(t, session, scene.ReloadParams{
		ExpectedScene:  path,
		DiscardUnsaved: true,
	})

	if !result.Reloaded {
		t.Fatal("expected scene.reload to report a completed reload")
	}

	if !result.WasUnsaved {
		t.Fatal("expected scene.reload to report the discarded unsaved state")
	}

	if !result.DiscardedUnsaved {
		t.Fatal("expected scene.reload to report discarded unsaved changes")
	}

	if !result.UndoHistoryCleared {
		t.Fatal("expected scene.reload to report cleared undo history")
	}

	if !strings.Contains(result.Note, "Unsaved changes were discarded") {
		t.Fatalf("expected the note to explain the discarded edit: %s", result.Note)
	}

	if !strings.Contains(result.Note, "did not save them") {
		t.Fatalf("expected the note to state that nothing was saved: %s", result.Note)
	}

	requireUnsaved(t, session, path, false)
	requireVector2Position(t, session, path, "World/Marker", 1, 2)
}

func TestSceneReloadExpectedSceneMismatchConflicts(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/scene_reload/expected.tscn"

	openFixtureScene(t, session, path)

	_, err := bridge.Call[scene.ReloadParams, scene.ReloadResult](
		session.Client,
		scene.ReloadName,
		scene.ReloadParams{
			ExpectedScene: "res://cases/scene_current_conflict/main.tscn",
		},
	)
	assertAPIErrorCode(t, err, apierr.CodeConflict)
	assertErrorMessageContains(
		t,
		err,
		"Expected res://cases/scene_current_conflict/main.tscn",
		"active scene is res://cases/scene_reload/expected.tscn",
	)

	// The mismatch must not switch or reload the active scene.
	current := readCurrentScene(t, session, path)

	if current.Scene != path || !current.Active {
		t.Fatalf("expected the active scene to stay unchanged, got %+v", current)
	}
}

func TestSceneReloadInvalidDiscardOptionRejected(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/scene_reload/expected.tscn"

	openFixtureScene(t, session, path)

	type invalidParams struct {
		DiscardUnsaved string `json:"discard_unsaved"`
	}

	_, err := bridge.Call[invalidParams, scene.ReloadResult](
		session.Client,
		scene.ReloadName,
		invalidParams{DiscardUnsaved: "yes"},
	)
	assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)
	assertErrorMessageContains(t, err, "discard_unsaved must be a boolean")
}

func TestSceneReloadRequiresActiveScene(t *testing.T) {
	session, err := harness.Start(nil)
	if errors.Is(err, harness.ErrGodotNotFound) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatalf("starting an isolated editor session: %v", err)
	}
	defer func() { _ = session.Close() }()

	_, err = bridge.Call[scene.ReloadParams, scene.ReloadResult](
		session.Client,
		scene.ReloadName,
		scene.ReloadParams{},
	)
	assertAPIErrorCode(t, err, apierr.CodeFailedPrecondition)
	assertErrorMessageContains(t, err, "No scene is currently being edited")
}

func reloadScene(
	t *testing.T,
	session *harness.Session,
	params scene.ReloadParams,
) scene.ReloadResult {
	t.Helper()

	result, err := bridge.Call[scene.ReloadParams, scene.ReloadResult](
		session.Client,
		scene.ReloadName,
		params,
	)
	if err != nil {
		t.Fatalf("expected scene.reload to succeed: %v", err)
	}

	return result
}

func readSceneTree(
	t *testing.T,
	session *harness.Session,
	path string,
) scene.TreeResult {
	t.Helper()

	result, err := bridge.Call[scene.TreeParams, scene.TreeResult](
		session.Client,
		scene.TreeName,
		scene.TreeParams{ExpectedScene: path},
	)
	if err != nil {
		t.Fatalf("expected scene.tree to succeed: %v", err)
	}

	return result
}

func readCurrentScene(
	t *testing.T,
	session *harness.Session,
	path string,
) scene.CurrentResult {
	t.Helper()

	result, err := bridge.Call[scene.CurrentParams, scene.CurrentResult](
		session.Client,
		scene.CurrentName,
		scene.CurrentParams{ExpectedScene: path},
	)
	if err != nil {
		t.Fatalf("expected scene.current to succeed: %v", err)
	}

	return result
}

func requireUnsaved(
	t *testing.T,
	session *harness.Session,
	path string,
	expected bool,
) {
	t.Helper()

	current := readCurrentScene(t, session, path)

	if current.Unsaved != expected {
		t.Fatalf(
			"expected unsaved=%v for %s, got %+v",
			expected,
			path,
			current,
		)
	}
}

func requireVector2Position(
	t *testing.T,
	session *harness.Session,
	path string,
	nodePath string,
	expectedX float64,
	expectedY float64,
) {
	t.Helper()

	result, err := bridge.Call[node.InspectParams, node.InspectResult](
		session.Client,
		node.InspectName,
		node.InspectParams{
			Node:          nodePath,
			Properties:    []string{"position"},
			ExpectedScene: path,
		},
	)
	if err != nil {
		t.Fatalf("inspecting %s: %v", nodePath, err)
	}

	for _, property := range result.Properties {
		if property.Name != "position" {
			continue
		}

		var position struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		}

		if err := json.Unmarshal(property.Value, &position); err != nil {
			t.Fatalf("decoding %s position: %v", nodePath, err)
		}

		if position.X != expectedX || position.Y != expectedY {
			t.Fatalf(
				"expected %s position (%v, %v), got (%v, %v)",
				nodePath,
				expectedX,
				expectedY,
				position.X,
				position.Y,
			)
		}

		return
	}

	t.Fatalf("expected node.inspect to return the position of %s", nodePath)
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}

	return false
}

func findTreePath(root scene.TreeNode, path string) (scene.TreeNode, bool) {
	if root.Path == path {
		return root, true
	}

	for _, child := range root.Children {
		if found, ok := findTreePath(child, path); ok {
			return found, true
		}
	}

	return scene.TreeNode{}, false
}

func assertErrorMessageContains(t *testing.T, err error, fragments ...string) {
	t.Helper()

	if err == nil {
		t.Fatal("expected an error")
	}

	for _, fragment := range fragments {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("expected error to contain %q: %v", fragment, err)
		}
	}
}
