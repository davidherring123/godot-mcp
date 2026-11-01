package core_test

import (
	"testing"

	"github.com/davidherring123/godot-mcp/internal/core/project/editor"
	productversion "github.com/davidherring123/godot-mcp/internal/version"
)

func TestEditorReportsPackagedVersion(t *testing.T) {
	session := requireSuite(t)
	result, err := editor.Status(session.Root(), editor.StatusParams{})
	if err != nil {
		t.Fatalf("reading live editor version: %v", err)
	}
	if !result.Running || result.Version != productversion.Current() {
		t.Fatalf("editor status = %+v; want running with version %s", result, productversion.Current())
	}
}
