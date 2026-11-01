package core_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davidherring123/godot-mcp/internal/adapter/bridge"
	"github.com/davidherring123/godot-mcp/internal/core/scene"
	"github.com/davidherring123/godot-mcp/internal/model/savereport"
	"github.com/davidherring123/godot-mcp/internal/protocol/apierr"
	"github.com/davidherring123/godot-mcp/tests/harness"
)

func TestSceneApplySaveReportsUnrelatedLiveChanges(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/scene_save_diagnostics/main.tscn"

	openFixtureScene(t, session, path)

	// This edit belongs to an earlier action and is still only in the editor
	// buffer when the unrelated batch below saves the scene.
	setNodeProperties(t, session, path, "World/Marker", map[string]json.RawMessage{
		"position": json.RawMessage(`{"x":5,"y":6}`),
	})

	result := applyScene(t, session, scene.ApplyParams{
		ExpectedScene: path,
		Save:          true,
		Operations: []scene.ApplyOperation{
			{
				Operation: "node.add",
				Type:      "Marker2D",
				Parent:    "World",
				Name:      "Reported",
			},
		},
	})

	if !result.Saved {
		t.Fatal("expected scene.apply to save the scene")
	}

	report := requireSaveReport(t, result.SaveReport)

	if report.Scene != path {
		t.Fatalf("report scene = %q; want %q", report.Scene, path)
	}

	if report.Format != "tscn" || report.Binary {
		t.Fatalf("unexpected text scene format: %+v", report)
	}

	if report.Scope != "entire_scene_file" {
		t.Fatalf("report scope = %q; want entire_scene_file", report.Scope)
	}

	if !report.Available || !report.Changed {
		t.Fatalf("expected an available, changed report: %+v", report)
	}

	if report.DiffTruncated {
		t.Fatalf("expected the small fixture diff not to be truncated: %+v", report)
	}

	for _, expected := range []string{
		"--- before",
		"+++ after",
		"+position = Vector2(5, 6)",
		`+[node name="Reported" type="Marker2D" parent="World"`,
	} {
		if !strings.Contains(report.Diff, expected) {
			t.Fatalf(
				"expected the save report diff to contain %q:\n%s",
				expected,
				report.Diff,
			)
		}
	}

	for _, expected := range []string{
		"not requested by the calling action",
		"does not repair or revert",
	} {
		if !strings.Contains(report.Note, expected) {
			t.Fatalf("expected the report note to contain %q: %s", expected, report.Note)
		}
	}

	saved := readSavedScene(t, session, path)

	for _, expected := range []string{
		"position = Vector2(5, 6)",
		`[node name="Reported" type="Marker2D" parent="World"`,
	} {
		if !strings.Contains(saved, expected) {
			t.Fatalf("expected the saved scene to contain %q:\n%s", expected, saved)
		}
	}

	if err := session.LogJSON(scene.ApplyName, result); err != nil {
		t.Fatal(err)
	}
}

// TestSceneApplySaveReportsExternalSceneOverwrite covers an external write to
// the scene file's stored damping override, not the script's exported default.
// The separate script-default test covers the reported original bug. The editor
// may or may not detect the external scene edit before saving, so the test
// asserts the report describes the transition that actually reached disk.
func TestSceneApplySaveReportsExternalSceneOverwrite(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/scene_save_external_overwrite/main.tscn"

	openFixtureScene(t, session, path)

	sceneFile := filepath.Join(
		session.Root(),
		"cases",
		"scene_save_external_overwrite",
		"main.tscn",
	)
	original, err := os.ReadFile(sceneFile)
	if err != nil {
		t.Fatalf("reading the scene fixture: %v", err)
	}

	externallyEdited := strings.Replace(
		string(original),
		"damping = 1.5",
		"damping = 0.4",
		1,
	)

	if externallyEdited == string(original) {
		t.Fatal("expected the fixture to contain damping = 1.5")
	}

	if err := os.WriteFile(sceneFile, []byte(externallyEdited), 0644); err != nil {
		t.Fatalf("writing the externally edited scene: %v", err)
	}

	result := applyScene(t, session, scene.ApplyParams{
		ExpectedScene: path,
		Save:          true,
		Operations: []scene.ApplyOperation{
			{
				Operation: "node.add",
				Type:      "Marker2D",
				Parent:    "World",
				Name:      "ColdLoad",
			},
		},
	})

	report := requireSaveReport(t, result.SaveReport)

	if !report.Available || !report.Changed {
		t.Fatalf("expected the external-overwrite save to report a change: %+v", report)
	}

	if !strings.Contains(
		report.Diff,
		`+[node name="ColdLoad" type="Marker2D" parent="World"`,
	) {
		t.Fatalf("expected the requested edit in the diff:\n%s", report.Diff)
	}

	saved := readSavedScene(t, session, path)

	switch {
	case strings.Contains(saved, "damping = 1.5"):
		// The editor kept its in-memory override and overwrote the external
		// scene edit. The report must expose that write even though the batch
		// did not touch damping.
		for _, expected := range []string{
			"-damping = 0.4",
			"+damping = 1.5",
		} {
			if !strings.Contains(report.Diff, expected) {
				t.Fatalf(
					"expected the overwritten scene value %q in the diff:\n%s",
					expected,
					report.Diff,
				)
			}
		}
	case strings.Contains(saved, "damping = 0.4"):
		// Godot refreshed the externally edited scene before the save, so no
		// overwrite occurred; the report still shows the requested addition.
		t.Log(
			"the editor refreshed the external scene edit before saving; " +
				"no overwrite was written in this session",
		)
	default:
		t.Fatalf("expected damping to be 1.5 or 0.4 in the saved scene:\n%s", saved)
	}
}

// TestSceneApplySaveAfterExternalScriptDefaultChange covers the reported
// script-default case: the scene relies on the script's exported damping=1.5,
// the script is changed to 0.4 behind the editor, and an unrelated batch then
// saves. Whether Godot reloads the script resource before the save depends on
// editor timing, so the test does not claim the override always reproduces; it
// requires the report to expose the override whenever one is written.
func TestSceneApplySaveAfterExternalScriptDefaultChange(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/scene_save_script_default/main.tscn"

	openFixtureScene(t, session, path)

	scriptFile := filepath.Join(session.Root(), "scripts", "damping_default.gd")
	original, err := os.ReadFile(scriptFile)
	if err != nil {
		t.Fatalf("reading the script fixture: %v", err)
	}

	externallyEdited := strings.Replace(
		string(original),
		"damping: float = 1.5",
		"damping: float = 0.4",
		1,
	)

	if externallyEdited == string(original) {
		t.Fatal("expected the script fixture to export damping = 1.5")
	}

	if err := os.WriteFile(scriptFile, []byte(externallyEdited), 0644); err != nil {
		t.Fatalf("writing the externally edited script: %v", err)
	}

	// Give the editor's filesystem scan time to notice the script change. The
	// save may still see either the old or the reloaded script default.
	time.Sleep(250 * time.Millisecond)

	result := applyScene(t, session, scene.ApplyParams{
		ExpectedScene: path,
		Save:          true,
		Operations: []scene.ApplyOperation{
			{
				Operation: "node.add",
				Type:      "Marker2D",
				Parent:    "World",
				Name:      "ScriptDefaultLoad",
			},
		},
	})

	report := requireSaveReport(t, result.SaveReport)

	if !report.Available || !report.Changed {
		t.Fatalf("expected the script-default save to report a change: %+v", report)
	}

	if !strings.Contains(
		report.Diff,
		`+[node name="ScriptDefaultLoad" type="Marker2D" parent="World"`,
	) {
		t.Fatalf("expected the requested edit in the diff:\n%s", report.Diff)
	}

	saved := readSavedScene(t, session, path)

	if strings.Contains(saved, "damping = 1.5") {
		// The scene now stores the stale 1.5 default as an override, which is
		// the reported bug. The report must show that unrelated write.
		if !strings.Contains(report.Diff, "+damping = 1.5") {
			t.Fatalf(
				"expected the script-default override in the diff:\n%s",
				report.Diff,
			)
		}

		t.Log(
			"the script-default override reproduced: the save wrote " +
				"damping = 1.5 after the script default changed to 0.4",
		)
	} else {
		dampingLine := ""

		for _, line := range strings.Split(saved, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "damping =") {
				dampingLine = strings.TrimSpace(line)
			}
		}

		t.Logf(
			"the script-default override did not reproduce in this session; "+
				"the editor did not retain the old exported default before "+
				"saving (saved damping line: %q)",
			dampingLine,
		)
	}
}

func TestSceneSaveReportsBinarySceneChange(t *testing.T) {
	session := requireSuite(t)
	openPath := "res://cases/scene_save_diagnostics/main.tscn"

	openFixtureScene(t, session, openPath)

	path := "res://cases/scene_save_diagnostics/generated/diagnostics.scn"

	_, err := bridge.Call[scene.CreateParams, scene.CreateResult](
		session.Client,
		scene.CreateName,
		scene.CreateParams{
			Scene:    path,
			RootType: "Node2D",
			Name:     "BinaryRoot",
		},
	)
	if err != nil {
		t.Fatalf("creating the binary scene: %v", err)
	}

	setNodeProperties(t, session, path, ".", map[string]json.RawMessage{
		"position": json.RawMessage(`{"x":7,"y":8}`),
	})

	result, err := bridge.Call[scene.SaveParams, scene.SaveResult](
		session.Client,
		scene.SaveName,
		scene.SaveParams{ExpectedScene: path},
	)
	if err != nil {
		t.Fatalf("saving the binary scene: %v", err)
	}

	report := requireSaveReport(t, result.SaveReport)

	if report.Format != "scn" || !report.Binary {
		t.Fatalf("expected a binary scene report: %+v", report)
	}

	if !report.Available || !report.Changed {
		t.Fatalf("expected the binary save to report a change: %+v", report)
	}

	if report.Diff != "" {
		t.Fatalf("expected no textual diff for a binary scene, got:\n%s", report.Diff)
	}

	if report.AfterBytes == 0 {
		t.Fatalf("expected the saved binary scene to have bytes: %+v", report)
	}

	if !strings.Contains(report.Note, "Binary scene file changed on disk") {
		t.Fatalf("expected a binary change note, got %q", report.Note)
	}
}

func TestSceneSaveReportTruncatesLargeDiff(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/scene_save_truncation/main.tscn"

	openFixtureScene(t, session, path)

	// Two unsaved 100-operation batches produce a diff well past the report's
	// line budget without needing a large fixture.
	for batch := 0; batch < 2; batch++ {
		operations := make([]scene.ApplyOperation, 0, 100)

		for index := 0; index < 100; index++ {
			operations = append(operations, scene.ApplyOperation{
				Operation: "node.add",
				Type:      "Marker2D",
				Parent:    ".",
				Name:      fmt.Sprintf("Batch%dNode%d", batch, index),
			})
		}

		applyScene(t, session, scene.ApplyParams{
			ExpectedScene: path,
			Operations:    operations,
		})
	}

	result, err := bridge.Call[scene.SaveParams, scene.SaveResult](
		session.Client,
		scene.SaveName,
		scene.SaveParams{ExpectedScene: path},
	)
	if err != nil {
		t.Fatalf("saving the large scene: %v", err)
	}

	report := requireSaveReport(t, result.SaveReport)

	if !report.Available || !report.Changed {
		t.Fatalf("expected an available, changed report: %+v", report)
	}

	if !report.DiffTruncated {
		t.Fatal("expected the large diff to be marked truncated")
	}

	if lines := strings.Count(report.Diff, "\n") + 1; lines > 120 {
		t.Fatalf("expected at most 120 diff lines, got %d", lines)
	}

	if !strings.Contains(report.Diff, "Batch0Node0") {
		t.Fatalf("expected the diff to keep the start of the change:\n%s", report.Diff)
	}
}

func TestSceneSaveReportLimitsLargeTextInput(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/scene_save_line_limit/main.tscn"

	openFixtureScene(t, session, path)

	// More than the diagnostic input line limit. The report must skip the
	// detailed diff instead of allocating one for the whole scene.
	for batch := 0; batch < 10; batch++ {
		operations := make([]scene.ApplyOperation, 0, 100)

		for index := 0; index < 100; index++ {
			operations = append(operations, scene.ApplyOperation{
				Operation: "node.add",
				Type:      "Marker2D",
				Parent:    ".",
				Name:      fmt.Sprintf("Limit%dNode%d", batch, index),
			})
		}

		applyScene(t, session, scene.ApplyParams{
			ExpectedScene: path,
			Operations:    operations,
		})
	}

	result, err := bridge.Call[scene.SaveParams, scene.SaveResult](
		session.Client,
		scene.SaveName,
		scene.SaveParams{ExpectedScene: path},
	)
	if err != nil {
		t.Fatalf("saving the line-limit scene: %v", err)
	}

	report := requireSaveReport(t, result.SaveReport)

	if !report.Available || !report.Changed {
		t.Fatalf("expected an available, changed report: %+v", report)
	}

	if !report.DiffTruncated {
		t.Fatal("expected the oversized text input to be marked truncated")
	}

	if report.Diff != "" {
		t.Fatalf("expected no detailed diff beyond the input line limit:\n%s", report.Diff)
	}

	if !strings.Contains(report.Note, "more than 1000 lines") {
		t.Fatalf("expected an input line-limit note, got %q", report.Note)
	}
}

func TestSceneApplyWithoutSaveOmitsSaveReport(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/scene_save_noop/main.tscn"

	openFixtureScene(t, session, path)

	result := applyScene(t, session, scene.ApplyParams{
		ExpectedScene: path,
		Operations: []scene.ApplyOperation{
			{
				Operation: "node.add",
				Type:      "Marker2D",
				Parent:    "World",
				Name:      "NotSaved",
			},
		},
	})

	if result.Saved {
		t.Fatal("expected scene.apply without save to skip the save")
	}

	if result.SaveReport != nil {
		t.Fatalf("expected no save report without save: %+v", result.SaveReport)
	}
}

// TestSceneSaveFailureContractUnchanged covers the reachable save failure
// path: the save preconditions and their error message are unchanged by the
// diagnostic report. A non-OK editor.save_scene() result cannot be forced from
// the live action surface because Godot reports OK even when the target path
// cannot be written; that pre-existing behavior is not changed by this work.
func TestSceneSaveFailureContractUnchanged(t *testing.T) {
	session, err := harness.Start(nil)
	if errors.Is(err, harness.ErrGodotNotFound) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatalf("starting an isolated editor session: %v", err)
	}
	defer func() { _ = session.Close() }()

	_, err = bridge.Call[scene.SaveParams, scene.SaveResult](
		session.Client,
		scene.SaveName,
		scene.SaveParams{},
	)

	assertAPIErrorCode(t, err, apierr.CodeFailedPrecondition)
	assertApplyErrorMessage(t, err, "No scene is currently being edited")
}

func TestSceneApplySaveFailureKeepsActionError(t *testing.T) {
	session := requireSuite(t)
	path := "res://cases/scene_apply_prevalidation/main.tscn"

	openFixtureScene(t, session, path)

	_, err := bridge.Call[scene.ApplyParams, scene.ApplyResult](
		session.Client,
		scene.ApplyName,
		scene.ApplyParams{
			ExpectedScene: path,
			Save:          true,
			Operations: []scene.ApplyOperation{
				{
					Operation: "node.add",
					Type:      "Marker2D",
					Parent:    "World",
					Name:      "Existing",
				},
			},
		},
	)

	assertAPIErrorCode(t, err, apierr.CodeConflict)
	assertApplyErrorMessage(t, err, "Prevalidation failed", "operation 0")

	if strings.Contains(err.Error(), "could not be saved") {
		t.Fatalf("save post-effect replaced the action error: %v", err)
	}
}

func requireSaveReport(t *testing.T, report *savereport.Report) *savereport.Report {
	t.Helper()

	if report == nil {
		t.Fatal("expected the save result to include save_report")
	}

	return report
}
