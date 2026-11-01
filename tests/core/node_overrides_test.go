package core_test

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestNodeOverridesLinkedDefaultAndRootOverride(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/node_overrides/main.tscn"

	openFixtureScene(t, session, path)

	defaultResult, err := bridge.Call[node.OverridesParams, node.OverridesResult](
		session.Client,
		node.OverridesName,
		node.OverridesParams{
			Node:          "Default",
			ExpectedScene: path,
		},
	)
	if err != nil {
		t.Fatalf("expected node.overrides to succeed: %v", err)
	}

	if !defaultResult.Linked {
		t.Fatal("expected Default to be reported as a linked instance")
	}

	if defaultResult.SourceScene != "res://cases/node_overrides/target.tscn" {
		t.Fatalf("unexpected source scene: %s", defaultResult.SourceScene)
	}

	if override, ok := optionalNodeOverride(defaultResult.Root, "speed"); ok {
		t.Fatalf("default instance should not override speed, got %+v", override)
	}

	child := requireChildOverrides(t, defaultResult.Children, "Sprite")
	position := requireNodeOverride(t, child.Properties, "position")

	if !position.Live || !position.OnDisk {
		t.Fatalf("expected child position override in live and disk state: %+v", position)
	}

	assertVector2Value(t, position.Value, 3, 4)
	assertVector2Value(t, position.Effective, 3, 4)
	assertVector2Value(t, position.DiskValue, 3, 4)
	assertVector2Value(t, position.Baseline, 10, 20)

	if position.BaselineSource != "source_scene" {
		t.Fatalf("expected source-scene child baseline, got %s", position.BaselineSource)
	}

	rootResult, err := bridge.Call[node.OverridesParams, node.OverridesResult](
		session.Client,
		node.OverridesName,
		node.OverridesParams{
			Node:          "Override",
			ExpectedScene: path,
		},
	)
	if err != nil {
		t.Fatalf("expected node.overrides to succeed: %v", err)
	}

	speed := requireNodeOverride(t, rootResult.Root, "speed")

	if !speed.Live || !speed.OnDisk {
		t.Fatalf("expected root speed override in live and disk state: %+v", speed)
	}

	assertFloatValue(t, speed.Value, 25)
	assertFloatValue(t, speed.Effective, 25)
	assertFloatValue(t, speed.DiskValue, 25)
	assertFloatValue(t, speed.Baseline, 100)

	if speed.BaselineSource != "default" {
		t.Fatalf("expected script-default speed baseline, got %s", speed.BaselineSource)
	}

	rootPosition := requireNodeOverride(t, rootResult.Root, "position")

	assertVector2Value(t, rootPosition.Value, 7, 8)
	assertVector2Value(t, rootPosition.Effective, 7, 8)

	if rootPosition.BaselineSource != "default" {
		t.Fatalf("expected default position baseline, got %s", rootPosition.BaselineSource)
	}
}

func TestNodeOverridesSourceSceneBaselineVersusScriptDefault(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/node_overrides/main.tscn"

	openFixtureScene(t, session, path)

	result, err := bridge.Call[node.OverridesParams, node.OverridesResult](
		session.Client,
		node.OverridesName,
		node.OverridesParams{
			Node:          "Sourced",
			ExpectedScene: path,
		},
	)
	if err != nil {
		t.Fatalf("expected node.overrides to succeed: %v", err)
	}

	speed := requireNodeOverride(t, result.Root, "speed")

	assertFloatValue(t, speed.Value, 25)
	assertFloatValue(t, speed.Baseline, 40)

	if speed.BaselineSource != "source_scene" {
		t.Fatalf(
			"expected source-scene speed baseline, got %s",
			speed.BaselineSource,
		)
	}

	label := requireNodeOverride(t, result.Root, "label")

	if label.BaselineSource != "default" {
		t.Fatalf("expected script-default label baseline, got %s", label.BaselineSource)
	}

	var baseline string

	if err := json.Unmarshal(label.Baseline, &baseline); err != nil {
		t.Fatalf("decoding label baseline: %v", err)
	}

	if baseline != "default" {
		t.Fatalf("expected label baseline default, got %q", baseline)
	}
}

func TestNodeOverridesUnsavedLiveOverrideDoesNotSave(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/node_overrides_unsaved/main.tscn"

	openMutableFixtureScene(t, session, path)

	diskPath := fixtureFilePath(session, path)
	before, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatalf("reading scene before inspection: %v", err)
	}

	setNodeProperties(
		t,
		session,
		path,
		"Player",
		map[string]json.RawMessage{
			"speed": json.RawMessage(`55`),
		},
	)

	result, err := bridge.Call[node.OverridesParams, node.OverridesResult](
		session.Client,
		node.OverridesName,
		node.OverridesParams{
			Node:          "Player",
			ExpectedScene: path,
		},
	)
	if err != nil {
		t.Fatalf("expected node.overrides to succeed: %v", err)
	}

	if !result.Unsaved {
		t.Fatal("expected unsaved scene state to be reported")
	}

	if result.DiskStatus != "compared" {
		t.Fatalf("expected disk comparison, got %s", result.DiskStatus)
	}

	speed := requireNodeOverride(t, result.Root, "speed")

	if !speed.Live {
		t.Fatal("expected the unsaved override in live packed state")
	}

	if !speed.OnDisk {
		t.Fatal("expected the saved override in disk state")
	}

	assertFloatValue(t, speed.Value, 55)
	assertFloatValue(t, speed.Effective, 55)
	assertFloatValue(t, speed.DiskValue, 25)
	assertFloatValue(t, speed.Baseline, 100)

	after, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatalf("reading scene after inspection: %v", err)
	}

	if !bytes.Equal(before, after) {
		t.Fatal("node.overrides must not save or alter the scene file")
	}

	liveAfter, err := bridge.Call[node.InspectParams, node.InspectResult](
		session.Client,
		node.InspectName,
		node.InspectParams{
			Node:          "Player",
			Properties:    []string{"speed"},
			ExpectedScene: path,
		},
	)
	if err != nil {
		t.Fatalf("inspecting the scene after node.overrides: %v", err)
	}

	if len(liveAfter.Properties) != 1 {
		t.Fatalf("expected one inspected property, got %d", len(liveAfter.Properties))
	}

	assertFloatValue(t, liveAfter.Properties[0].Value, 55)
}

func TestNodeOverridesPropertyFilter(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/node_overrides/main.tscn"

	openFixtureScene(t, session, path)

	result, err := bridge.Call[node.OverridesParams, node.OverridesResult](
		session.Client,
		node.OverridesName,
		node.OverridesParams{
			Node:          "Override",
			Properties:    []string{"speed"},
			ExpectedScene: path,
		},
	)
	if err != nil {
		t.Fatalf("expected node.overrides to succeed: %v", err)
	}

	if len(result.Root) != 1 {
		t.Fatalf("expected one filtered root override, got %d", len(result.Root))
	}

	requireNodeOverride(t, result.Root, "speed")

	if _, ok := optionalNodeOverride(result.Root, "position"); ok {
		t.Fatal("expected the filter to exclude position")
	}
}

func TestNodeOverridesMissingNodeReturnsNotFound(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/node_overrides/main.tscn"

	openFixtureScene(t, session, path)

	_, err := bridge.Call[node.OverridesParams, node.OverridesResult](
		session.Client,
		node.OverridesName,
		node.OverridesParams{
			Node:          "Missing",
			ExpectedScene: path,
		},
	)

	assertAPIErrorCode(t, err, apierr.CodeNotFound)
}

func TestNodeOverridesNonInstanceNodeReturnsFailedPrecondition(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/node_overrides/main.tscn"

	openFixtureScene(t, session, path)

	_, err := bridge.Call[node.OverridesParams, node.OverridesResult](
		session.Client,
		node.OverridesName,
		node.OverridesParams{
			Node:          ".",
			ExpectedScene: path,
		},
	)

	assertAPIErrorCode(t, err, apierr.CodeFailedPrecondition)
}

func TestNodeOverridesEditableChildPersistedUnsavedAndSourceEqual(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/node_overrides_children/main.tscn"

	openMutableFixtureScene(t, session, path)

	diskPath := fixtureFilePath(session, path)
	before := readFixtureFile(t, diskPath)

	assertPersistedChildPositionOverride(
		t,
		callNodeOverridesFor(t, session, path, "Player"),
	)

	// Setting the child back to its source value removes the live override
	// without saving; the disk override remains visible.
	setNodeProperties(
		t,
		session,
		path,
		"Player/Sprite",
		map[string]json.RawMessage{
			"position": json.RawMessage(`{"x":10,"y":20}`),
		},
	)

	sourceEqual := callNodeOverridesFor(t, session, path, "Player")

	assertSourceEqualChildPositionOverride(t, sourceEqual)
	requireCurrentUnsaved(t, session, path, true)

	// A changed persisted value keeps its explicit disk provenance. A new
	// unsaved child value that differs from the source is a live save candidate.
	setNodeProperties(
		t,
		session,
		path,
		"Player/Sprite",
		map[string]json.RawMessage{
			"position": json.RawMessage(`{"x":9,"y":9}`),
			"rotation": json.RawMessage(`0.5`),
		},
	)

	assertUnsavedChildOverrides(
		t,
		callNodeOverridesFor(t, session, path, "Player"),
	)
	requireCurrentUnsaved(t, session, path, true)

	assertFixtureFileUnchanged(t, diskPath, before)
	assertInspectedChildPosition(t, session, path, "Player/Sprite", 9, 9)
}

func callNodeOverridesFor(
	t *testing.T,
	session *harness.Session,
	scenePath string,
	nodePath string,
) node.OverridesResult {
	t.Helper()

	result, err := bridge.Call[node.OverridesParams, node.OverridesResult](
		session.Client,
		node.OverridesName,
		node.OverridesParams{
			Node:          nodePath,
			ExpectedScene: scenePath,
		},
	)
	if err != nil {
		t.Fatalf("expected node.overrides to succeed: %v", err)
	}

	return result
}

func assertPersistedChildPositionOverride(
	t *testing.T,
	result node.OverridesResult,
) {
	t.Helper()

	if result.Truncated {
		t.Fatal("expected a small inventory not to be truncated")
	}

	child := requireChildOverrides(t, result.Children, "Sprite")

	if len(child.Properties) != 1 {
		t.Fatalf(
			"expected only the persisted child override, got %d properties",
			len(child.Properties),
		)
	}

	position := requireNodeOverride(t, child.Properties, "position")

	if !position.Live || !position.OnDisk || position.Inferred {
		t.Fatalf("expected an explicit persisted child override: %+v", position)
	}

	assertVector2Value(t, position.Value, 5, 6)
	assertVector2Value(t, position.Effective, 5, 6)
	assertVector2Value(t, position.DiskValue, 5, 6)
	assertVector2Value(t, position.Baseline, 10, 20)

	if position.BaselineSource != "source_scene" {
		t.Fatalf("expected source-scene child baseline, got %s", position.BaselineSource)
	}
}

func assertSourceEqualChildPositionOverride(
	t *testing.T,
	result node.OverridesResult,
) {
	t.Helper()

	if !result.Unsaved {
		t.Fatal("expected the source-equal child change to remain unsaved")
	}

	child := requireChildOverrides(t, result.Children, "Sprite")
	position := requireNodeOverride(t, child.Properties, "position")

	if position.Live || position.Inferred {
		t.Fatalf("source-equal child value must not be a live override: %+v", position)
	}

	if !position.OnDisk {
		t.Fatalf("expected the disk override to remain visible: %+v", position)
	}

	assertVector2Value(t, position.Effective, 10, 20)
	assertVector2Value(t, position.DiskValue, 5, 6)
	assertVector2Value(t, position.Baseline, 10, 20)
}

func assertUnsavedChildOverrides(
	t *testing.T,
	result node.OverridesResult,
) {
	t.Helper()

	child := requireChildOverrides(t, result.Children, "Sprite")

	if len(child.Properties) != 2 {
		t.Fatalf("expected two changed child properties, got %d", len(child.Properties))
	}

	position := requireNodeOverride(t, child.Properties, "position")

	if !position.Live || position.Inferred {
		t.Fatalf("persisted child override should not be inferred: %+v", position)
	}

	if !position.OnDisk {
		t.Fatalf("expected the persisted child override in disk state: %+v", position)
	}

	assertVector2Value(t, position.Value, 9, 9)
	assertVector2Value(t, position.Effective, 9, 9)
	assertVector2Value(t, position.DiskValue, 5, 6)
	assertVector2Value(t, position.Baseline, 10, 20)

	rotation := requireNodeOverride(t, child.Properties, "rotation")

	if !rotation.Live || rotation.OnDisk {
		t.Fatalf("expected a new unsaved child value: %+v", rotation)
	}

	assertFloatValue(t, rotation.Value, 0.5)
	assertFloatValue(t, rotation.Effective, 0.5)
	assertFloatValue(t, rotation.Baseline, 0)
}

func readFixtureFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture file %s: %v", path, err)
	}

	return data
}

func assertFixtureFileUnchanged(t *testing.T, path string, before []byte) {
	t.Helper()

	after := readFixtureFile(t, path)

	if !bytes.Equal(before, after) {
		t.Fatalf("node.overrides must not save or alter %s", path)
	}
}

func assertInspectedChildPosition(
	t *testing.T,
	session *harness.Session,
	scenePath string,
	nodePath string,
	x float64,
	y float64,
) {
	t.Helper()

	result, err := bridge.Call[node.InspectParams, node.InspectResult](
		session.Client,
		node.InspectName,
		node.InspectParams{
			Node:          nodePath,
			Properties:    []string{"position"},
			ExpectedScene: scenePath,
		},
	)
	if err != nil {
		t.Fatalf("inspecting %s after node.overrides: %v", nodePath, err)
	}

	if len(result.Properties) != 1 {
		t.Fatalf("expected one inspected property, got %d", len(result.Properties))
	}

	assertVector2Value(t, result.Properties[0].Value, x, y)
}

func TestNodeOverridesInheritedSceneBaseline(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/node_overrides/main.tscn"

	openFixtureScene(t, session, path)

	result, err := bridge.Call[node.OverridesParams, node.OverridesResult](
		session.Client,
		node.OverridesName,
		node.OverridesParams{
			Node:          "Inherited",
			ExpectedScene: path,
		},
	)
	if err != nil {
		t.Fatalf("expected node.overrides to succeed: %v", err)
	}

	if result.Truncated {
		t.Fatal("expected a small inventory not to be truncated")
	}

	speed := requireNodeOverride(t, result.Root, "speed")

	assertFloatValue(t, speed.Value, 25)
	assertFloatValue(t, speed.Baseline, 40)

	if speed.BaselineSource != "inherited_scene" {
		t.Fatalf(
			"expected inherited-scene speed baseline, got %s",
			speed.BaselineSource,
		)
	}

	if len(result.Children) != 0 {
		t.Fatalf(
			"expected inherited child values equal to source to be omitted, got %+v",
			result.Children,
		)
	}
}

func TestNodeOverridesNodeTruncationIsExplicit(t *testing.T) {
	session := requireSuite(t)
	const childCount = 260

	caseDir := filepath.Join(session.Root(), "cases", "node_overrides_truncate")

	if err := os.MkdirAll(caseDir, 0o755); err != nil {
		t.Fatalf("creating truncation fixture: %v", err)
	}

	var source strings.Builder

	source.WriteString("[gd_scene format=3]\n\n[node name=\"Source\" type=\"Node2D\"]\n\n")

	for index := 0; index < childCount; index++ {
		fmt.Fprintf(
			&source,
			"[node name=\"Child%d\" type=\"Node2D\" parent=\".\"]\n\n",
			index,
		)
	}

	sourcePath := filepath.Join(caseDir, "source.tscn")

	if err := os.WriteFile(sourcePath, []byte(source.String()), 0o644); err != nil {
		t.Fatalf("writing truncation source scene: %v", err)
	}

	var main strings.Builder

	main.WriteString("[gd_scene load_steps=2 format=3]\n\n")
	main.WriteString("[ext_resource type=\"PackedScene\" path=\"res://cases/node_overrides_truncate/source.tscn\" id=\"1_source\"]\n\n")
	main.WriteString("[node name=\"Main\" type=\"Node2D\"]\n\n")
	main.WriteString("[node name=\"Root\" parent=\".\" instance=ExtResource(\"1_source\")]\n\n")

	for index := 0; index < childCount; index++ {
		fmt.Fprintf(
			&main,
			"[node name=\"Child%d\" parent=\"Root\" index=\"%d\"]\nposition = Vector2(%d, 0)\n\n",
			index,
			index,
			index+1,
		)
	}

	main.WriteString("[editable path=\"Root\"]\n")

	mainPath := filepath.Join(caseDir, "main.tscn")

	if err := os.WriteFile(mainPath, []byte(main.String()), 0o644); err != nil {
		t.Fatalf("writing truncation main scene: %v", err)
	}

	resourcePath := "res://cases/node_overrides_truncate/main.tscn"

	openFixtureScene(t, session, resourcePath)

	result, err := bridge.Call[node.OverridesParams, node.OverridesResult](
		session.Client,
		node.OverridesName,
		node.OverridesParams{
			Node:          "Root",
			ExpectedScene: resourcePath,
		},
	)
	if err != nil {
		t.Fatalf("expected node.overrides to succeed: %v", err)
	}

	if !result.Truncated {
		t.Fatal("expected the inventory to report truncation")
	}

	if len(result.Children) != 256 {
		t.Fatalf("expected 256 bounded child groups, got %d", len(result.Children))
	}
}

func openMutableFixtureScene(
	t *testing.T,
	session *harness.Session,
	path string,
) {
	t.Helper()

	openFixtureScene(t, session, path)

	t.Cleanup(func() {
		_, err := bridge.Call[scene.ReloadParams, scene.ReloadResult](
			session.Client,
			scene.ReloadName,
			scene.ReloadParams{
				ExpectedScene:  path,
				DiscardUnsaved: true,
			},
		)
		if err != nil {
			t.Errorf("reloading mutable fixture %s: %v", path, err)
		}
	})
}

func requireCurrentUnsaved(
	t *testing.T,
	session *harness.Session,
	expectedScene string,
	expected bool,
) {
	t.Helper()

	result, err := bridge.Call[scene.CurrentParams, scene.CurrentResult](
		session.Client,
		scene.CurrentName,
		scene.CurrentParams{ExpectedScene: expectedScene},
	)
	if err != nil {
		t.Fatalf("reading scene current state: %v", err)
	}

	if result.Unsaved != expected {
		t.Fatalf("expected unsaved=%t, got %t", expected, result.Unsaved)
	}
}

func requireNodeOverride(
	t *testing.T,
	overrides []node.PropertyOverride,
	property string,
) node.PropertyOverride {
	t.Helper()

	override, ok := optionalNodeOverride(overrides, property)
	if !ok {
		t.Fatalf("expected override property %s", property)
	}

	return override
}

func optionalNodeOverride(
	overrides []node.PropertyOverride,
	property string,
) (node.PropertyOverride, bool) {
	for _, override := range overrides {
		if override.Property == property {
			return override, true
		}
	}

	return node.PropertyOverride{}, false
}

func requireChildOverrides(
	t *testing.T,
	children []node.NodeOverrides,
	path string,
) node.NodeOverrides {
	t.Helper()

	for _, child := range children {
		if child.Path == path {
			return child
		}
	}

	t.Fatalf("expected child overrides for %s", path)

	return node.NodeOverrides{}
}

func fixtureFilePath(session *harness.Session, resourcePath string) string {
	return filepath.Join(
		session.Root(),
		filepath.FromSlash(strings.TrimPrefix(resourcePath, "res://")),
	)
}

func assertFloatValue(t *testing.T, raw json.RawMessage, expected float64) {
	t.Helper()

	var value float64

	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decoding float value: %v", err)
	}

	if value != expected {
		t.Fatalf("expected %v, got %v", expected, value)
	}
}

func assertVector2Value(
	t *testing.T,
	raw json.RawMessage,
	x float64,
	y float64,
) {
	t.Helper()

	var value struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}

	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decoding vector value: %v", err)
	}

	if value.X != x || value.Y != y {
		t.Fatalf("expected (%v, %v), got (%v, %v)", x, y, value.X, value.Y)
	}
}
