package version_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	productversion "github.com/davidherring123/godot-mcp/internal/version"
)

func TestCurrentMatchesAddonPluginConfig(t *testing.T) {
	pluginVersion := readPluginVersion(t, filepath.Join(
		"..",
		"..",
		"internal",
		"addon",
		"godot_mcp",
		"plugin.cfg",
	))

	if got := productversion.Current(); got != pluginVersion {
		t.Fatalf("canonical version = %q, addon plugin.cfg version = %q", got, pluginVersion)
	}
}

func TestCurrentIsCanonicalNumericVersion(t *testing.T) {
	current := productversion.Current()

	normalized, err := productversion.Normalize(current)
	if err != nil {
		t.Fatalf("canonical version %q is invalid: %v", current, err)
	}

	if normalized != current {
		t.Fatalf("canonical version normalized from %q to %q", current, normalized)
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "numeric", input: "1.2.3", want: "1.2.3"},
		{name: "trimmed", input: " 1.2.3 ", want: "1.2.3"},
		{name: "legacy prefix", input: "v1.2.3", want: "1.2.3"},
		{name: "canonical prefix", input: "v" + productversion.Current(), want: productversion.Current()},
		{name: "empty uses current", input: "", want: productversion.Current()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := productversion.Normalize(test.input)
			if err != nil {
				t.Fatalf("normalizing %q: %v", test.input, err)
			}

			if got != test.want {
				t.Fatalf("normalizing %q = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestNormalizeRejectsNonVersions(t *testing.T) {
	for _, input := range []string{
		"dev",
		"vdev",
		"v",
		"1.2",
		"1.2.3.4",
		"0.1.0-rc.1",
		"release-0.1.0",
		"V1.2.3",
	} {
		t.Run(input, func(t *testing.T) {
			if got, err := productversion.Normalize(input); err == nil {
				t.Fatalf("normalizing %q = %q; want an error", input, got)
			}
		})
	}
}

func readPluginVersion(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.TrimSpace(key) != "version" {
			continue
		}

		unquoted, err := strconv.Unquote(strings.TrimSpace(value))
		if err != nil {
			t.Fatalf("reading version from %s: %v", path, err)
		}

		return unquoted
	}

	t.Fatalf("%s has no version entry", path)

	return ""
}
