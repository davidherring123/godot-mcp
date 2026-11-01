package host_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	productversion "github.com/davidherring123/godot-mcp/internal/version"
)

// TestMainVersionLdflags verifies the real host entrypoint, not just the
// release build arguments: a -ldflags stamped version must reach the version
// command, and an unstamped binary must fall back to the canonical version.
func TestMainVersionLdflags(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go toolchain unavailable: %v", err)
	}

	root := filepath.Join("..", "..")

	t.Run("stamped override", func(t *testing.T) {
		binary := buildHost(t, root, "-X main.VERSION=0.9.7")

		if got, want := runHostVersion(t, binary), "godot-mcp-host 0.9.7\n"; got != want {
			t.Fatalf("stamped version output = %q, want %q", got, want)
		}
	})

	t.Run("unstamped default", func(t *testing.T) {
		binary := buildHost(t, root, "")

		want := "godot-mcp-host " + productversion.Current() + "\n"
		if got := runHostVersion(t, binary); got != want {
			t.Fatalf("unstamped version output = %q, want %q", got, want)
		}
	})
}

func buildHost(t *testing.T, root, ldflags string) string {
	t.Helper()

	name := "godot-mcp-host"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	binary := filepath.Join(t.TempDir(), name)
	args := []string{"build", "-o", binary}
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	args = append(args, "./cmd/godot-mcp-host")

	command := exec.Command("go", args...)
	command.Dir = root

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("building host with ldflags %q: %v\n%s", ldflags, err, output)
	}

	return binary
}

func runHostVersion(t *testing.T, binary string) string {
	t.Helper()

	output, err := exec.Command(binary, "version").Output()
	if err != nil {
		t.Fatalf("running %s version: %v", binary, err)
	}

	return string(output)
}
