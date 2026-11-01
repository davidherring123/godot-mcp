package core_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davidherring123/godot-mcp/internal/core/resource"
	"github.com/davidherring123/godot-mcp/internal/core/scene"
	"github.com/davidherring123/godot-mcp/tests/harness"
)

// TestCreatedUIDsAreRegisteredForFreshProcessLoads covers the creation flow
// that previously produced "ext_resource, invalid UID" warnings until a rescan:
// a resource and a scene are created in new directories, then the scene is
// referenced by a parent scene. A separate process may only resolve UIDs from
// .godot/uid_cache.bin, so a clean load proves the creation actions persisted
// the UIDs that ResourceSaver embedded in the files.
func TestCreatedUIDsAreRegisteredForFreshProcessLoads(t *testing.T) {
	session := requireSuite(t)
	root := session.Root()
	base := "cases/uid_fresh_" + time.Now().Format("150405.000000000")
	resourcePath := "res://" + base + "/materials/mat.tres"
	childPath := "res://" + base + "/scenes/child.tscn"
	parentPath := "res://" + base + "/parents/parent.tscn"

	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(root, filepath.FromSlash(base)))
	})

	created, err := resource.Create(root, resource.CreateParams{
		Resource: resourcePath,
		Type:     "StandardMaterial3D",
	})
	if err != nil {
		t.Fatalf("creating resource in a new directory: %v", err)
	}

	assertRegisteredUID(t, "resource.create", created.UID, created.UIDRegistered, created.Note)

	child, err := scene.Create(root, scene.CreateParams{
		Scene:    childPath,
		RootType: "MeshInstance3D",
		Properties: map[string]json.RawMessage{
			"material_override": json.RawMessage(
				fmt.Sprintf(`{"resource":%q}`, resourcePath),
			),
		},
	})
	if err != nil {
		t.Fatalf("creating scene in a new directory: %v", err)
	}

	assertRegisteredUID(t, "scene.create", child.UID, child.UIDRegistered, child.Note)

	parent, err := scene.Create(root, scene.CreateParams{
		Scene:    parentPath,
		RootType: "Node",
	})
	if err != nil {
		t.Fatalf("creating parent scene in a new directory: %v", err)
	}

	assertRegisteredUID(t, "scene.create parent", parent.UID, parent.UIDRegistered, parent.Note)

	applied, err := scene.Apply(root, scene.ApplyParams{
		Operations: []scene.ApplyOperation{
			{
				Operation: "node.instantiate",
				Scene:     childPath,
				Parent:    ".",
				Name:      "Child",
			},
		},
		Save: true,
	})
	if err != nil {
		t.Fatalf("instantiating the created scene into its parent: %v", err)
	}

	if applied.DryRun || !applied.Applied {
		t.Fatalf("parent batch was not applied: %+v", applied)
	}

	// The parent scene must actually reference the child by UID; otherwise the
	// fresh-process load would not exercise the ext_resource UID lookup.
	assertFileContains(t, root, childPath, child.UID)
	assertFileContains(t, root, parentPath, child.UID)

	assertCacheContains(t, root, resourcePath, childPath, parentPath)

	for _, path := range []string{resourcePath, childPath, parentPath} {
		assertFreshProcessLoad(t, session, path)
	}
}

// TestResourceApplyRegistersCreatedUIDs covers the batch path: one shared
// bounded registration pass for a new directory, including a resource that
// references a resource created earlier in the same batch.
func TestResourceApplyRegistersCreatedUIDs(t *testing.T) {
	session := requireSuite(t)
	root := session.Root()
	base := "cases/uid_batch_" + time.Now().Format("150405.000000000")
	directory := "res://" + base + "/materials"
	firstPath := directory + "/first.tres"
	secondPath := directory + "/second.tres"

	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(root, filepath.FromSlash(base)))
	})

	result, err := resource.Apply(root, resource.ApplyParams{
		Operations: []resource.ApplyOperation{
			{
				Operation: "create",
				Resource:  firstPath,
				Type:      "StandardMaterial3D",
			},
			{
				Operation: "create",
				Resource:  secondPath,
				Type:      "StandardMaterial3D",
				Properties: map[string]json.RawMessage{
					"next_pass": json.RawMessage(
						fmt.Sprintf(`{"resource":%q}`, firstPath),
					),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("applying a resource batch in a new directory: %v", err)
	}

	if result.IndexPending {
		t.Fatalf("applied batch reported a pending index: %+v", result)
	}

	var firstUID string

	for index, operation := range result.Operations {
		if operation.Status != "created" {
			t.Fatalf("operation %d status = %q; want created", index, operation.Status)
		}

		if operation.UID == "" || !strings.HasPrefix(operation.UID, "uid://") {
			t.Fatalf("operation %d uid = %q; want a uid:// identifier", index, operation.UID)
		}

		if operation.UIDRegistered == nil || !*operation.UIDRegistered {
			t.Fatalf("operation %d did not register its UID", index)
		}

		if index == 0 {
			firstUID = operation.UID
		}
	}

	assertFileContains(t, root, secondPath, firstUID)
	assertCacheContains(t, root, firstPath, secondPath)

	for _, path := range []string{firstPath, secondPath} {
		assertFreshProcessLoad(t, session, path)
	}
}

func assertRegisteredUID(
	t *testing.T,
	action string,
	uid string,
	registered bool,
	note string,
) {
	t.Helper()

	if !strings.HasPrefix(uid, "uid://") {
		t.Fatalf("%s returned uid %q; want a uid:// identifier", action, uid)
	}

	if !registered {
		t.Fatalf("%s did not register uid %s; note: %s", action, uid, note)
	}

	if note != "" {
		t.Fatalf("%s returned an unexpected pending note: %s", action, note)
	}
}

func assertFileContains(t *testing.T, root, resourcePath, needle string) {
	t.Helper()

	data, err := os.ReadFile(projectFilePath(t, root, resourcePath))
	if err != nil {
		t.Fatalf("reading %s: %v", resourcePath, err)
	}

	if !bytes.Contains(data, []byte(needle)) {
		t.Fatalf("%s does not reference %q:\n%s", resourcePath, needle, data)
	}
}

func assertCacheContains(t *testing.T, root string, paths ...string) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(root, ".godot", "uid_cache.bin"))
	if err != nil {
		t.Fatalf("reading UID cache: %v", err)
	}

	for _, path := range paths {
		if !bytes.Contains(data, []byte(path)) {
			t.Fatalf("UID cache does not contain %s", path)
		}
	}
}

func assertFreshProcessLoad(
	t *testing.T,
	session *harness.Session,
	resourcePath string,
) {
	t.Helper()

	output, err := session.RunGameScript(
		"res://scripts/uid_load_probe.gd",
		resourcePath,
	)
	if err != nil {
		t.Fatalf(
			"fresh process load of %s: %v\nstdout:\n%s\nstderr:\n%s",
			resourcePath,
			err,
			output.Stdout,
			output.Stderr,
		)
	}

	if !strings.Contains(output.Stdout, "UID_LOAD_PROBE") ||
		!strings.Contains(output.Stdout, "ok=true") {
		t.Fatalf(
			"fresh process did not load %s\nstdout:\n%s\nstderr:\n%s",
			resourcePath,
			output.Stdout,
			output.Stderr,
		)
	}

	if strings.Contains(output.Stderr, "invalid UID") {
		t.Fatalf(
			"fresh process load of %s reported an invalid UID:\n%s",
			resourcePath,
			output.Stderr,
		)
	}
}

func projectFilePath(t *testing.T, root, resourcePath string) string {
	t.Helper()

	if !strings.HasPrefix(resourcePath, "res://") {
		t.Fatalf("%s is not a res:// path", resourcePath)
	}

	return filepath.Join(
		root,
		filepath.FromSlash(strings.TrimPrefix(resourcePath, "res://")),
	)
}
