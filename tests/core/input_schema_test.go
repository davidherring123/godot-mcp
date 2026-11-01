package core_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	gojsonschema "github.com/google/jsonschema-go/jsonschema"
	invopop "github.com/invopop/jsonschema"

	"github.com/davidherring123/godot-mcp/internal/core/project/input"
)

func TestInputApplyOperationSchemaIncludesQualifiedAliases(t *testing.T) {
	reflector := invopop.Reflector{Anonymous: true, ExpandedStruct: true}
	schema := reflector.ReflectFromType(reflect.TypeFor[input.ApplyParams]())

	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("encoding the input.map_apply schema: %v", err)
	}

	for _, operation := range []string{
		"add",
		"input.add",
		"set",
		"input.set",
		"rename",
		"input.rename",
		"delete",
		"input.delete",
		"event_add",
		"input.event_add",
		"event_delete",
		"input.event_delete",
	} {
		if !strings.Contains(string(data), `"`+operation+`"`) {
			t.Fatalf("expected %q in the operation enum: %s", operation, data)
		}
	}
}

func TestInputInspectSchemaAllowsNullAndKeepsTypedPayload(t *testing.T) {
	reflector := invopop.Reflector{Anonymous: true, ExpandedStruct: true}
	schemaData, err := json.Marshal(
		reflector.ReflectFromType(reflect.TypeFor[input.InspectResult]()),
	)
	if err != nil {
		t.Fatalf("encoding the input.inspect schema: %v", err)
	}

	var schema gojsonschema.Schema

	if err := json.Unmarshal(schemaData, &schema); err != nil {
		t.Fatalf("decoding the input.inspect schema: %v", err)
	}

	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolving the input.inspect schema: %v", err)
	}

	single := map[string]any{
		"name":     "jump",
		"deadzone": 0.5,
		"events":   []any{},
	}
	if err := resolved.Validate(single); err != nil {
		t.Fatalf("single-action result does not match the schema: %v", err)
	}

	multi := map[string]any{
		"actions": map[string]any{
			"jump": map[string]any{
				"name":     "jump",
				"deadzone": 0.5,
				"events":   []any{},
			},
			"missing": nil,
		},
	}
	if err := resolved.Validate(multi); err != nil {
		t.Fatalf("multi-name result with null does not match the schema: %v", err)
	}

	if err := resolved.Validate(map[string]any{"name": 1}); err == nil {
		t.Fatal("expected the single-action schema to keep typed name validation")
	}

	if err := resolved.Validate(map[string]any{
		"actions": map[string]any{"jump": "not-an-action"},
	}); err == nil {
		t.Fatal("expected the multi-name schema to keep typed action validation")
	}
}
