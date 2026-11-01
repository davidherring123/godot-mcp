package core_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davidherring123/godot-mcp/internal/adapter/bridge"
	"github.com/davidherring123/godot-mcp/internal/core/resource"
	"github.com/davidherring123/godot-mcp/internal/core/scene"
	"github.com/davidherring123/godot-mcp/internal/protocol/apierr"
)

// TestResourceApplyRegistrationKeepsEarlierWrites covers a batch whose last
// operation on a path is a no-op set. The file was still written by an earlier
// create (here a delete and recreate too), so it must be registered and
// annotated even though the final operation reports "unchanged".
func TestResourceApplyRegistrationKeepsEarlierWrites(t *testing.T) {
	session := requireSuite(t)
	root := session.Root()
	base := "cases/uid_batch_noop_" + time.Now().Format("150405.000000000")
	path := "res://" + base + "/materials/recreated.tres"
	color := json.RawMessage(`{"r":0.2,"g":0.4,"b":0.6,"a":1}`)

	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(root, filepath.FromSlash(base)))
	})

	result, err := resource.Apply(root, resource.ApplyParams{
		Operations: []resource.ApplyOperation{
			{
				Operation:  "create",
				Resource:   path,
				Type:       "StandardMaterial3D",
				Properties: map[string]json.RawMessage{"albedo_color": color},
			},
			{Operation: "delete", Resource: path},
			{
				Operation:  "create",
				Resource:   path,
				Type:       "StandardMaterial3D",
				Properties: map[string]json.RawMessage{"albedo_color": color},
			},
			{
				Operation:  "set",
				Resource:   path,
				Properties: map[string]json.RawMessage{"albedo_color": color},
			},
		},
	})
	if err != nil {
		t.Fatalf("applying a create/delete/recreate/no-op batch: %v", err)
	}

	if result.IndexPending {
		t.Fatalf("recreated resource was left pending in the index: %+v", result)
	}

	for index, operation := range result.Operations {
		switch operation.Status {
		case "created", "updated":
			if operation.UID == "" || !strings.HasPrefix(operation.UID, "uid://") {
				t.Fatalf("operation %d uid = %q; want a uid:// identifier", index, operation.UID)
			}

			if operation.UIDRegistered == nil || !*operation.UIDRegistered {
				t.Fatalf("operation %d did not register its UID: %+v", index, operation)
			}
		case "unchanged", "deleted":
			// These operations did not write the file, so they must not claim
			// a UID annotation just because another operation wrote the path.
			if operation.UID != "" || operation.UIDRegistered != nil {
				t.Fatalf("operation %d reported writing metadata: %+v", index, operation)
			}
		}
	}

	if _, err := os.Stat(projectFilePath(t, root, path)); err != nil {
		t.Fatalf("expected the recreated resource on disk: %v", err)
	}

	assertCacheContains(t, root, path)
	assertFreshProcessLoad(t, session, path)
}

// TestResourceApplyFinalDeleteSkipsRegistration locks in the complement: a
// path that the batch leaves deleted must not be registered or annotated.
func TestResourceApplyFinalDeleteSkipsRegistration(t *testing.T) {
	session := requireSuite(t)
	root := session.Root()
	base := "cases/uid_batch_delete_" + time.Now().Format("150405.000000000")
	path := "res://" + base + "/materials/removed.tres"

	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(root, filepath.FromSlash(base)))
	})

	result, err := resource.Apply(root, resource.ApplyParams{
		Operations: []resource.ApplyOperation{
			{Operation: "create", Resource: path, Type: "StandardMaterial3D"},
			{
				Operation:  "set",
				Resource:   path,
				Properties: map[string]json.RawMessage{"roughness": json.RawMessage(`0.5`)},
			},
			{Operation: "delete", Resource: path},
		},
	})
	if err != nil {
		t.Fatalf("applying a create/set/delete batch: %v", err)
	}

	if result.IndexPending {
		t.Fatalf("deleted path reported a pending index: %+v", result)
	}

	for index, operation := range result.Operations {
		if operation.UID != "" || operation.UIDRegistered != nil {
			t.Fatalf("deleted path operation %d reported writing metadata: %+v", index, operation)
		}
	}

	if _, err := os.Stat(projectFilePath(t, root, path)); !os.IsNotExist(err) {
		t.Fatalf("expected the final delete to remove %s, stat returned: %v", path, err)
	}
}

func TestResourceCreateReportsNestedIndexedReadback(t *testing.T) {
	session := requireSuite(t)
	root := session.Root()
	base := "cases/resource_create_nested_" + time.Now().Format("150405.000000000")
	path := "res://" + base + "/material.tres"

	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(root, filepath.FromSlash(base)))
	})

	result := createResource(t, session, resource.CreateParams{
		Resource: path,
		Type:     "StandardMaterial3D",
		Properties: map[string]json.RawMessage{
			"albedo_color:g": json.RawMessage(`1.0`),
			"albedo_color:b": json.RawMessage(`0.25`),
		},
	})

	var green float64

	if err := json.Unmarshal(result.Properties["albedo_color:g"], &green); err != nil {
		t.Fatalf("decoding albedo_color:g: %v", err)
	}

	if green != 1 {
		t.Fatalf("albedo_color:g = %v; want 1", green)
	}

	var blue float64

	if err := json.Unmarshal(result.Properties["albedo_color:b"], &blue); err != nil {
		t.Fatalf("decoding albedo_color:b: %v", err)
	}

	if blue != 0.25 {
		t.Fatalf("albedo_color:b = %v; want 0.25", blue)
	}

	text := readResourceFile(t, session, path)

	if !strings.Contains(text, "albedo_color = Color(1, 1, 0.25, 1)") {
		t.Fatalf("expected the nested color components to be saved:\n%s", text)
	}
}

func TestSceneCreateReportsNestedIndexedReadback(t *testing.T) {
	session := requireSuite(t)
	root := session.Root()
	base := "cases/scene_create_nested_" + time.Now().Format("150405.000000000")
	path := "res://" + base + "/main.tscn"

	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(root, filepath.FromSlash(base)))
	})

	created, err := scene.Create(root, scene.CreateParams{
		Scene:    path,
		RootType: "Node2D",
		Name:     "Root",
		Properties: map[string]json.RawMessage{
			"position:x": json.RawMessage(`4.0`),
			"position:y": json.RawMessage(`5.0`),
		},
	})
	if err != nil {
		t.Fatalf("creating a scene with nested indexed properties: %v", err)
	}

	for name, expected := range map[string]float64{
		"position:x": 4,
		"position:y": 5,
	} {
		var value float64

		if err := json.Unmarshal(created.Properties[name], &value); err != nil {
			t.Fatalf("decoding %s: %v", name, err)
		}

		if value != expected {
			t.Fatalf("%s = %v; want %v", name, value, expected)
		}
	}
}

func TestResourceSetReportsRequestedIndexedPaths(t *testing.T) {
	session := requireSuite(t)
	root := session.Root()
	base := "cases/resource_set_nested_" + time.Now().Format("150405.000000000")
	path := "res://" + base + "/material.tres"

	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(root, filepath.FromSlash(base)))
	})

	createResource(t, session, resource.CreateParams{
		Resource: path,
		Type:     "StandardMaterial3D",
	})

	result := setResourceProperties(t, session, path, map[string]json.RawMessage{
		"albedo_color:r": json.RawMessage(`0.25`),
		"roughness":      json.RawMessage(`0.75`),
	})

	if !result.Changed {
		t.Fatal("expected resource.set to report a change")
	}

	var red float64

	if err := json.Unmarshal(result.Properties["albedo_color:r"], &red); err != nil {
		t.Fatalf("decoding albedo_color:r: %v", err)
	}

	if red != 0.25 {
		t.Fatalf("albedo_color:r = %v; want 0.25", red)
	}

	if _, found := result.Properties["albedo_color"]; found {
		t.Fatalf(
			"resource.set reported the collapsed root property instead of the requested key: %v",
			result.Properties,
		)
	}

	var roughness float64

	if err := json.Unmarshal(result.Properties["roughness"], &roughness); err != nil {
		t.Fatalf("decoding roughness: %v", err)
	}

	if roughness != 0.75 {
		t.Fatalf("roughness = %v; want 0.75", roughness)
	}
}

// TestSceneSaveUnchangedReportMentionsDiskNotBuffer covers the unchanged save
// note. The report compares the file on disk before and after the write; it
// cannot know whether the editor buffer matched the file.
func TestSceneSaveUnchangedReportMentionsDiskNotBuffer(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/scene_save_noop/main.tscn"

	openFixtureScene(t, session, path)

	var last scene.SaveResult

	// The first save may normalize the fixture (for example by adding its
	// UID); save until the file is stable, then inspect that no-op report.
	for attempt := 0; attempt < 3; attempt++ {
		result, err := bridge.Call[scene.SaveParams, scene.SaveResult](
			session.Client,
			scene.SaveName,
			scene.SaveParams{ExpectedScene: path},
		)
		if err != nil {
			t.Fatalf("saving %s: %v", path, err)
		}

		if !result.Saved {
			t.Fatalf("expected saved:true for a successful save: %+v", result)
		}

		last = result

		if result.SaveReport != nil && !result.SaveReport.Changed {
			break
		}
	}

	report := requireSaveReport(t, last.SaveReport)

	if !report.Available {
		t.Fatalf("expected the no-op save report to be available: %+v", report)
	}

	if report.Changed {
		t.Fatalf("expected a stable file to report no change: %+v", report)
	}

	if !strings.Contains(report.Note, "on disk") {
		t.Fatalf("expected the unchanged note to describe disk bytes: %q", report.Note)
	}

	if strings.Contains(report.Note, "matched the editor buffer") {
		t.Fatalf("the unchanged note must not claim the editor buffer matched: %q", report.Note)
	}

	if !strings.Contains(report.Note, "does not validate or repair") {
		t.Fatalf("expected the unchanged note to stay diagnostic-only: %q", report.Note)
	}
}

func TestResourcePathsRejectProjectEscapes(t *testing.T) {
	session := requireSuite(t)
	outsideDir := filepath.Dir(session.Root())
	suffix := time.Now().Format("150405.000000000")

	t.Run("create", func(t *testing.T) {
		name := "godot-mcp-escape-create-" + suffix + ".tres"
		outside := filepath.Join(outsideDir, name)

		t.Cleanup(func() { _ = os.Remove(outside) })

		_, err := bridge.Call[resource.CreateParams, resource.CreateResult](
			session.Client,
			resource.CreateName,
			resource.CreateParams{
				Resource: "res://../" + name,
				Type:     "StandardMaterial3D",
			},
		)
		assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)

		if _, statErr := os.Stat(outside); !os.IsNotExist(statErr) {
			t.Fatalf("resource.create escaped the project and created %s: %v", outside, statErr)
		}
	})

	t.Run("delete", func(t *testing.T) {
		name := "godot-mcp-escape-delete-" + suffix + ".tres"
		outside := filepath.Join(outsideDir, name)

		if err := os.WriteFile(outside, []byte("outside"), 0644); err != nil {
			t.Fatalf("creating the outside fixture: %v", err)
		}

		t.Cleanup(func() { _ = os.Remove(outside) })

		_, err := bridge.Call[resource.DeleteParams, resource.DeleteResult](
			session.Client,
			resource.DeleteName,
			resource.DeleteParams{Resource: "res://../" + name},
		)
		assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)

		if _, statErr := os.Stat(outside); statErr != nil {
			t.Fatalf("resource.delete escaped the project and removed %s: %v", outside, statErr)
		}
	})

	t.Run("set", func(t *testing.T) {
		name := "godot-mcp-escape-set-" + suffix + ".tres"
		outside := filepath.Join(outsideDir, name)
		original := "[gd_resource type=\"StandardMaterial3D\" format=3]\n\n[resource]\nroughness = 1.0\n"

		if err := os.WriteFile(outside, []byte(original), 0644); err != nil {
			t.Fatalf("creating the outside fixture: %v", err)
		}

		t.Cleanup(func() { _ = os.Remove(outside) })

		_, err := bridge.Call[resource.SetParams, resource.SetResult](
			session.Client,
			resource.SetName,
			resource.SetParams{
				Resource: "res://../" + name,
				Properties: map[string]json.RawMessage{
					"roughness": json.RawMessage(`0.5`),
				},
			},
		)
		assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)

		content, readErr := os.ReadFile(outside)
		if readErr != nil {
			t.Fatalf("reading the outside fixture after resource.set: %v", readErr)
		}

		if string(content) != original {
			t.Fatalf("resource.set rewrote the outside file: %q", content)
		}
	})

	t.Run("inspect", func(t *testing.T) {
		name := "godot-mcp-escape-read-" + suffix + ".tres"
		outside := filepath.Join(outsideDir, name)

		if err := os.WriteFile(
			outside,
			[]byte("[gd_resource type=\"StandardMaterial3D\" format=3]\n"),
			0644,
		); err != nil {
			t.Fatalf("creating the outside fixture: %v", err)
		}

		t.Cleanup(func() { _ = os.Remove(outside) })

		_, err := bridge.Call[resource.InspectParams, resource.InspectResult](
			session.Client,
			resource.InspectName,
			resource.InspectParams{Resource: "res://../" + name},
		)
		assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)

		// Embedded subresource reads keep working, but their base path is held
		// to the same containment rule.
		_, err = bridge.Call[resource.InspectParams, resource.InspectResult](
			session.Client,
			resource.InspectName,
			resource.InspectParams{Resource: "res://../" + name + "::Embedded"},
		)
		assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)
	})

	t.Run("batch_create", func(t *testing.T) {
		name := "godot-mcp-escape-batch-create-" + suffix + ".tres"
		outside := filepath.Join(outsideDir, name)

		t.Cleanup(func() { _ = os.Remove(outside) })

		_, err := resource.Apply(session.Root(), resource.ApplyParams{
			Operations: []resource.ApplyOperation{
				{
					Operation: "create",
					Resource:  "res://../" + name,
					Type:      "StandardMaterial3D",
				},
			},
		})
		assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)

		if _, statErr := os.Stat(outside); !os.IsNotExist(statErr) {
			t.Fatalf("resource.apply escaped the project and created %s: %v", outside, statErr)
		}
	})

	// A failed batch rolls back the files it snapshotted. The snapshot must not
	// include (and therefore must not rewrite) a path outside the project.
	t.Run("batch_rollback", func(t *testing.T) {
		name := "godot-mcp-escape-batch-delete-" + suffix + ".tres"
		outside := filepath.Join(outsideDir, name)
		inside := "res://cases/resource_escape_batch_" + suffix + "/material.tres"

		if err := os.WriteFile(outside, []byte("outside-original"), 0644); err != nil {
			t.Fatalf("creating the outside fixture: %v", err)
		}

		t.Cleanup(func() {
			_ = os.Remove(outside)
			_ = os.RemoveAll(filepath.Join(session.Root(), "cases", "resource_escape_batch_"+suffix))
		})

		_, err := resource.Apply(session.Root(), resource.ApplyParams{
			Operations: []resource.ApplyOperation{
				{Operation: "create", Resource: inside, Type: "StandardMaterial3D"},
				{Operation: "delete", Resource: "res://../" + name},
			},
		})
		assertAPIErrorCode(t, err, apierr.CodeInvalidArgument)

		content, readErr := os.ReadFile(outside)
		if readErr != nil {
			t.Fatalf("reading the outside fixture after rollback: %v", readErr)
		}

		if string(content) != "outside-original" {
			t.Fatalf("rollback rewrote the outside file: %q", content)
		}

		if _, statErr := os.Stat(projectFilePath(t, session.Root(), inside)); !os.IsNotExist(statErr) {
			t.Fatalf("expected rollback to remove the earlier in-batch create: %v", statErr)
		}
	})
}
