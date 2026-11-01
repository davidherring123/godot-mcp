package release_test

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/davidherring123/godot-mcp/internal/release"
)

func TestBuildUsesRootOutputAndControlledEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Go command uses a POSIX shell script")
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/release\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	addonDir := filepath.Join(root, filepath.FromSlash(release.DefaultAddonDir))
	if err := os.MkdirAll(addonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(addonDir, "plugin.cfg"),
		[]byte("[plugin]\nversion=\"0.0.0\"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	environmentPath := filepath.Join(root, "build-environment.txt")
	argumentsPath := filepath.Join(root, "build-arguments.txt")
	fakeGo := filepath.Join(root, "fake-go")
	script := `#!/bin/sh
env | sort > "` + environmentPath + `"
printf '%s\n' "$@" > "` + argumentsPath + `"
while [ "$#" -gt 0 ]; do
    if [ "$1" = "-o" ]; then
        printf 'fake host binary' > "$2"
        exit 0
    fi
    shift
done
exit 1
`
	if err := os.WriteFile(fakeGo, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GOAMD64", "v3")
	t.Setenv("GOARM64", "v9.5")
	t.Setenv("GOEXPERIMENT", "fieldtrack")
	t.Setenv("GOFLAGS", "-tags=unexpected")

	artifacts, err := release.Build(release.Options{
		Root:      root,
		Version:   "v1.2.3",
		Targets:   []release.Target{{OS: "linux", Arch: "amd64"}},
		GoCommand: fakeGo,
	})
	if err != nil {
		t.Fatalf("building release: %v", err)
	}

	wantDirectory := filepath.Join(root, "dist") + string(filepath.Separator)
	names := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		if !strings.HasPrefix(artifact, wantDirectory) {
			t.Fatalf("artifact %q is outside %q", artifact, wantDirectory)
		}

		names = append(names, filepath.Base(artifact))
	}

	wantNames := []string{
		"SHA256SUMS",
		"godot-mcp-addon-1.2.3-linux-amd64.tar.gz",
		"godot-mcp-addon-1.2.3.zip",
	}
	if !slices.Equal(names, wantNames) {
		t.Fatalf("artifacts = %v, want %v", names, wantNames)
	}

	assertPackagedPluginVersion(t, filepath.Join(root, "dist", "godot-mcp-addon-1.2.3.zip"), "1.2.3")

	environment, err := os.ReadFile(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(environment)
	for _, unwanted := range []string{"GOEXPERIMENT=", "GOFLAGS=", "GOARM64="} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("build inherited %s:\n%s", unwanted, text)
		}
	}
	if !strings.Contains(text, "GOAMD64=v1\n") {
		t.Fatalf("build did not set the amd64 baseline:\n%s", text)
	}

	arguments, err := os.ReadFile(argumentsPath)
	if err != nil {
		t.Fatal(err)
	}

	commandLine := strings.Split(strings.TrimSpace(string(arguments)), "\n")
	if len(commandLine) == 0 || commandLine[len(commandLine)-1] != "./cmd/godot-mcp-host" {
		t.Fatalf("release build command = %v, want ./cmd/godot-mcp-host", commandLine)
	}

	if !strings.Contains(string(arguments), "main.VERSION=1.2.3") {
		t.Fatalf("release build does not embed the normalized version:\n%s", arguments)
	}

	if strings.Contains(string(arguments), "main.VERSION=v1.2.3") {
		t.Fatalf("release build embedded a v-prefixed version:\n%s", arguments)
	}

	if strings.Contains(string(arguments), "./cmd/godot-mcp\n") {
		t.Fatalf("release build still packages the legacy CLI:\n%s", arguments)
	}
}

func TestBuildRejectsNonVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/release\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, version := range []string{"dev", "vdev", "v", "1.2", "0.1.0-rc.1"} {
		t.Run(version, func(t *testing.T) {
			_, err := release.Build(release.Options{
				Root:    root,
				Version: version,
				Targets: []release.Target{{OS: "linux", Arch: "amd64"}},
			})
			if err == nil || !strings.Contains(err.Error(), "invalid version") {
				t.Fatalf("building with version %q returned %v; want an invalid version error", version, err)
			}
		})
	}
}

func assertPackagedPluginVersion(t *testing.T, archivePath string, want string) {
	t.Helper()

	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("opening %s: %v", archivePath, err)
	}
	defer func() {
		_ = reader.Close()
	}()

	for _, file := range reader.File {
		if file.Name != "addons/godot_mcp/plugin.cfg" {
			continue
		}

		entry, err := file.Open()
		if err != nil {
			t.Fatalf("opening %s: %v", file.Name, err)
		}

		data, readErr := io.ReadAll(entry)
		closeErr := entry.Close()
		if readErr != nil {
			t.Fatalf("reading %s: %v", file.Name, readErr)
		}
		if closeErr != nil {
			t.Fatalf("closing %s: %v", file.Name, closeErr)
		}

		if !strings.Contains(string(data), `version="`+want+`"`) {
			t.Fatalf("packaged plugin.cfg version is not %q:\n%s", want, data)
		}

		return
	}

	t.Fatalf("%s has no addons/godot_mcp/plugin.cfg", archivePath)
}
