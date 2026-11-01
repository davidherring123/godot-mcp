package core_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davidherring123/godot-mcp/internal/core/project/filesystem"
	"github.com/davidherring123/godot-mcp/internal/protocol/apierr"
	"github.com/davidherring123/godot-mcp/tests/harness"
)

func TestFileSystemScanReportsPathsAndUIDs(t *testing.T) {
	session := requireSuite(t)
	mainScript := "res://scripts/main.gd"
	scene := "res://scenes/player.tscn"
	missing := "res://cases/filesystem_scan_missing.gd"

	result, err := filesystem.Scan(session.Root(), filesystem.ScanParams{
		Paths:          []string{mainScript, scene, missing},
		TimeoutSeconds: 5,
	})

	if err != nil {
		t.Fatalf("scanning the editor filesystem: %v", err)
	}

	if !result.Completed || result.TimedOut || result.Scanning {
		t.Fatalf("scan did not complete cleanly: %+v", result)
	}

	if result.AlreadyScanning != result.FollowUpScan {
		t.Fatalf("scan did not report a consistent follow-up state: %+v", result)
	}

	if result.DurationMS < 0 {
		t.Fatalf("scan duration is negative: %d", result.DurationMS)
	}

	script := scanResultForPath(t, result, mainScript)

	if !script.Found || script.Type != "GDScript" {
		t.Fatalf("script result = %+v; want a found GDScript", script)
	}

	if !strings.HasPrefix(script.UID, "uid://") || !script.UIDValid {
		t.Fatalf("script result = %+v; want a valid existing UID", script)
	}

	sceneResult := scanResultForPath(t, result, scene)

	if !sceneResult.Found || sceneResult.Type != "PackedScene" {
		t.Fatalf("scene result = %+v; want a found PackedScene", sceneResult)
	}

	missingResult := scanResultForPath(t, result, missing)

	if missingResult.Found {
		t.Fatalf("missing path was reported as found: %+v", missingResult)
	}

	if missingResult.UID != "" || missingResult.UIDValid {
		t.Fatalf("missing path reported a UID: %+v", missingResult)
	}

	if err := session.LogJSON(filesystem.ScanName, result); err != nil {
		t.Fatal(err)
	}
}

func TestFileSystemScanDetectsExternalFileWithoutGeneratingUID(t *testing.T) {
	session := requireSuite(t)
	relativeDir := filepath.Join("cases", "filesystem_scan_external_"+time.Now().Format("150405.000000000"))
	absoluteDir := filepath.Join(session.Root(), relativeDir)
	path := "res://" + filepath.ToSlash(filepath.Join(relativeDir, "external.tres"))

	if err := os.MkdirAll(absoluteDir, 0755); err != nil {
		t.Fatalf("creating scan fixture directory: %v", err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(absoluteDir) })

	contents := "[gd_resource type=\"StandardMaterial3D\" format=3]\n\n[resource]\nroughness = 0.5\n"

	if err := os.WriteFile(filepath.Join(absoluteDir, "external.tres"), []byte(contents), 0644); err != nil {
		t.Fatalf("writing scan fixture: %v", err)
	}

	result, err := filesystem.Scan(session.Root(), filesystem.ScanParams{
		Paths:          []string{path},
		TimeoutSeconds: 5,
	})

	if err != nil {
		t.Fatalf("scanning an externally created file: %v", err)
	}

	first := scanResultForPath(t, result, path)

	if !first.Found || first.Type != "StandardMaterial3D" {
		t.Fatalf("external resource result = %+v; want a found StandardMaterial3D", first)
	}

	if first.UID != "" || first.UIDValid {
		t.Fatalf("scan generated a UID for an unowned resource: %+v", first)
	}

	again, err := filesystem.Scan(session.Root(), filesystem.ScanParams{
		Paths:          []string{path},
		TimeoutSeconds: 5,
	})

	if err != nil {
		t.Fatalf("rescanning an externally created file: %v", err)
	}

	second := scanResultForPath(t, again, path)

	if second.UID != "" || second.UIDValid {
		t.Fatalf("rescan generated a UID for an unowned resource: %+v", second)
	}
}

func TestFileSystemScanRestartsAfterAnExistingScan(t *testing.T) {
	session := requireSuite(t)
	root := session.Root()
	contents := "[gd_resource type=\"StandardMaterial3D\" format=3]\n\n[resource]\nroughness = 0.25\n"

	// A full scan of a tiny project finishes before the test can request a
	// scan, so add cheap files to keep the scan thread busy long enough to
	// observe the already_scanning branch deterministically.
	pressureDir := filepath.Join(
		"cases",
		"filesystem_scan_pressure_"+time.Now().Format("150405.000000000"),
	)
	pressureAbsolute := filepath.Join(root, pressureDir)

	if err := os.MkdirAll(pressureAbsolute, 0755); err != nil {
		t.Fatalf("creating scan pressure directory: %v", err)
	}

	for index := 0; index < 250; index++ {
		name := filepath.Join(pressureAbsolute, fmt.Sprintf("p%04d.tres", index))

		if err := os.WriteFile(name, []byte(contents), 0644); err != nil {
			t.Fatalf("writing scan pressure file: %v", err)
		}
	}

	t.Cleanup(func() {
		_ = os.RemoveAll(pressureAbsolute)

		// Drop the removed files from the editor index so later tests in the
		// shared suite do not keep stale filesystem entries.
		if err := triggerEditorScan(session); err == nil {
			_, _ = filesystem.Scan(root, filesystem.ScanParams{
				TimeoutSeconds: 5,
			})
		}
	})

	for attempt := 0; attempt < 6; attempt++ {
		relativeDir := filepath.Join(
			"cases",
			fmt.Sprintf("filesystem_scan_running_%d_%d", time.Now().UnixNano(), attempt),
		)
		absoluteDir := filepath.Join(root, relativeDir)

		if err := os.MkdirAll(absoluteDir, 0755); err != nil {
			t.Fatalf("creating already-running scan fixture directory: %v", err)
		}

		t.Cleanup(func() { _ = os.RemoveAll(absoluteDir) })

		external := "res://" + filepath.ToSlash(filepath.Join(relativeDir, "external.tres"))

		// Start the scan independently of any product action. resource.create
		// now waits for scan completion, so it can no longer leave a scan
		// running for this branch.
		if err := triggerEditorScan(session); err != nil {
			t.Fatalf("starting an editor scan: %v", err)
		}

		// External edit made while the scan is running and before the request.
		if err := os.WriteFile(filepath.Join(absoluteDir, "external.tres"), []byte(contents), 0644); err != nil {
			t.Fatalf("writing external resource during an existing scan: %v", err)
		}

		result, err := filesystem.Scan(root, filesystem.ScanParams{
			Paths:          []string{external},
			TimeoutSeconds: 5,
		})
		if err != nil {
			t.Fatalf("scanning while another scan was running: %v", err)
		}

		if !result.AlreadyScanning {
			continue
		}

		if !result.Completed || result.TimedOut || result.Scanning {
			t.Fatalf("follow-up scan did not complete cleanly: %+v", result)
		}

		if !result.FollowUpScan {
			t.Fatalf("scan waited for an existing scan without starting a fresh one: %+v", result)
		}

		pathResult := scanResultForPath(t, result, external)

		if !pathResult.Found || pathResult.Type != "StandardMaterial3D" {
			t.Fatalf("external resource added before the scan request was missed: %+v", pathResult)
		}

		return
	}

	t.Fatal("could not observe an already-running editor filesystem scan")
}

// triggerEditorScan asks the test-only editor plugin in the core fixture to
// start a filesystem scan. It reports an error instead of failing the test so
// cleanup can use it safely.
func triggerEditorScan(session *harness.Session) error {
	requestPath := filepath.Join(session.Root(), ".test_scan_request")
	startedPath := filepath.Join(session.Root(), ".test_scan_started")

	_ = os.Remove(startedPath)

	if err := os.WriteFile(requestPath, []byte("scan\n"), 0644); err != nil {
		return fmt.Errorf("writing scan request: %w", err)
	}

	deadline := time.Now().Add(15 * time.Second)

	for time.Now().Before(deadline) {
		if _, err := os.Stat(startedPath); err == nil {
			_ = os.Remove(startedPath)

			return nil
		}

		time.Sleep(5 * time.Millisecond)
	}

	return errors.New("editor did not start a filesystem scan")
}

func TestFileSystemScanValidatesArguments(t *testing.T) {
	root := t.TempDir()

	cases := []struct {
		name   string
		params filesystem.ScanParams
	}{
		{
			name:   "escaping path",
			params: filesystem.ScanParams{Paths: []string{"res://../escape.gd"}},
		},
		{
			name:   "non-resource path",
			params: filesystem.ScanParams{Paths: []string{"scripts/main.gd"}},
		},
		{
			name:   "timeout above bridge limit",
			params: filesystem.ScanParams{TimeoutSeconds: 9},
		},
		{
			name:   "too many paths",
			params: filesystem.ScanParams{Paths: make([]string, 101)},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := filesystem.Scan(root, testCase.params)
			var apiError *apierr.Error

			if !errors.As(err, &apiError) || apiError.Code != apierr.CodeInvalidArgument {
				t.Fatalf("scan error = %v; want invalid_argument", err)
			}
		})
	}
}

func scanResultForPath(t *testing.T, result filesystem.ScanResult, path string) filesystem.PathResult {
	t.Helper()

	for _, candidate := range result.Paths {
		if candidate.Path == path {
			return candidate
		}
	}

	t.Fatalf("scan result does not contain %s: %+v", path, result.Paths)

	return filesystem.PathResult{}
}
