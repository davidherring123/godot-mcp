package scene_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/davidherring123/godot-mcp/internal/core/scene"
)

func TestSceneListAddonFilter(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"main.tscn", "levels/level.tscn", "addons/plugin/demo.tscn", ".addons/cache/demo.tscn", ".godot/ignored.tscn"} {
		file := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("[gd_scene format=3]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, include := range []bool{false, true} {
		result, err := scene.List(root, scene.ListParams{IncludeAddons: include})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"res://levels/level.tscn", "res://main.tscn"}
		if include {
			want = append(want, "res://.addons/cache/demo.tscn", "res://addons/plugin/demo.tscn")
		}
		slices.Sort(want)
		if !slices.Equal(result.Scenes, want) {
			t.Fatalf("include_addons=%v: got %v, want %v", include, result.Scenes, want)
		}
	}
}
