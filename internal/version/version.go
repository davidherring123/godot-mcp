// Package version owns the canonical released product version.
package version

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"
)

// current is the single canonical product version source. The packaged addon
// plugin.cfg carries the same value for Godot; a contract test keeps them equal.
//
//go:embed VERSION
var current string

var numericVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// Current returns the canonical numeric product version.
func Current() string {
	normalized, err := Normalize(current)
	if err != nil {
		panic("godot-mcp version: embedded VERSION: " + err.Error())
	}

	return normalized
}

// Normalize returns the unprefixed numeric release version. An empty value uses
// Current, and a legacy "v" prefix is removed. Development labels or other
// non-version values are rejected.
func Normalize(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = strings.TrimSpace(current)
	}

	normalized := strings.TrimPrefix(value, "v")
	if !numericVersion.MatchString(normalized) {
		return "", fmt.Errorf(
			"invalid version %q: use an unprefixed numeric version such as X.Y.Z",
			value,
		)
	}

	return normalized, nil
}
