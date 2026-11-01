package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gojsonschema "github.com/google/jsonschema-go/jsonschema"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/davidherring123/godot-mcp/internal/core"
	"github.com/davidherring123/godot-mcp/internal/core/project/input"
)

func TestMCPInputInspectMultiOutputSchemaAllowsNull(t *testing.T) {
	session := startLiveSession(t)
	name := "mcp_test_input_inspect_multi"
	missing := "mcp_test_input_inspect_multi_missing"

	_, _ = input.Delete(session.Root(), input.DeleteParams{Action: name})
	t.Cleanup(func() {
		_, _ = input.Delete(session.Root(), input.DeleteParams{Action: name})
	})

	if _, err := input.Add(session.Root(), input.AddParams{
		Action:   name,
		Deadzone: floatPointer(0.3),
	}); err != nil {
		t.Fatalf("seeding the input action: %v", err)
	}

	mcpSession := connectMCP(t, session.Root(), []core.ActionDefinition{
		core.InputInspect,
	})

	outputSchema := inputInspectOutputSchema(t, mcpSession)
	result, err := mcpSession.CallTool(context.Background(), &mcpgo.CallToolParams{
		Name: "input_inspect",
		Arguments: map[string]any{
			"actions": []any{name, missing},
		},
	})
	if err != nil {
		t.Fatalf("calling input_inspect: %v", err)
	}

	if result.IsError {
		t.Fatalf("input_inspect multi lookup returned an error: %s", mcpText(result))
	}

	assertSchemaValid(t, outputSchema, result.StructuredContent)

	structured, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("encoding the multi lookup result: %v", err)
	}

	var payload map[string]any

	if err := json.Unmarshal(structured, &payload); err != nil {
		t.Fatalf("decoding the multi lookup result: %v", err)
	}

	actions, ok := payload["actions"].(map[string]any)

	if !ok {
		t.Fatalf("expected an actions object: %s", structured)
	}

	if actions[name] == nil {
		t.Fatalf("expected %s in the multi lookup: %s", name, structured)
	}

	value, ok := actions[missing]

	if !ok || value != nil {
		t.Fatalf("expected %s to map to null: %s", missing, structured)
	}

	single, err := mcpSession.CallTool(context.Background(), &mcpgo.CallToolParams{
		Name:      "input_inspect",
		Arguments: map[string]any{"action": name},
	})
	if err != nil {
		t.Fatalf("calling single input_inspect: %v", err)
	}

	if single.IsError {
		t.Fatalf("single input_inspect returned an error: %s", mcpText(single))
	}

	assertSchemaValid(t, outputSchema, single.StructuredContent)
}

func TestMCPInputMapApplySchemaIncludesQualifiedOperationAliases(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(
		filepath.Join(root, "project.godot"),
		[]byte("[application]\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	session := connectMCP(t, root, []core.ActionDefinition{core.InputMapApply})

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}

	for _, tool := range tools.Tools {
		if tool.Name != "input_map_apply" {
			continue
		}

		data, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("encoding the input_map_apply schema: %v", err)
		}

		for _, operation := range []string{
			"input.add",
			"input.set",
			"input.rename",
			"input.delete",
			"input.event_add",
			"input.event_delete",
		} {
			if !strings.Contains(string(data), `"`+operation+`"`) {
				t.Fatalf("expected %q in the exposed operation enum: %s", operation, data)
			}
		}

		return
	}

	t.Fatal("input_map_apply is not exposed")
}

func inputInspectOutputSchema(t *testing.T, session *mcpgo.ClientSession) any {
	t.Helper()

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}

	for _, tool := range tools.Tools {
		if tool.Name == "input_inspect" {
			if tool.OutputSchema == nil {
				t.Fatal("input_inspect has no output schema")
			}

			return tool.OutputSchema
		}
	}

	t.Fatal("input_inspect is not exposed")

	return nil
}

func assertSchemaValid(t *testing.T, schema any, instance any) {
	t.Helper()

	schemaData, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("encoding the exposed output schema: %v", err)
	}

	var decoded gojsonschema.Schema

	if err := json.Unmarshal(schemaData, &decoded); err != nil {
		t.Fatalf("decoding the exposed output schema: %v", err)
	}

	resolved, err := decoded.Resolve(nil)
	if err != nil {
		t.Fatalf("resolving the exposed output schema: %v", err)
	}

	instanceData, err := json.Marshal(instance)
	if err != nil {
		t.Fatalf("encoding the structured content: %v", err)
	}

	var value any

	if err := json.Unmarshal(instanceData, &value); err != nil {
		t.Fatalf("decoding the structured content: %v", err)
	}

	if err := resolved.Validate(value); err != nil {
		t.Fatalf(
			"structured content does not match the exposed output schema: %v\nschema: %s\nvalue: %s",
			err,
			schemaData,
			instanceData,
		)
	}
}

func floatPointer(value float64) *float64 {
	return &value
}
