package input

import (
	"reflect"

	"github.com/invopop/jsonschema"
)

// InspectActions is the per-name result map for a multi-name input.inspect
// lookup. Missing names are nil and serialize as JSON null.
type InspectActions map[string]*Action

// JSONSchema keeps the per-name map typed while allowing null values for
// actions that do not exist. The Action schema is inlined so the output schema
// does not depend on definitions owned by the caller.
func (InspectActions) JSONSchema() *jsonschema.Schema {
	reflector := schemaReflector()
	action := reflector.ReflectFromType(reflect.TypeFor[Action]())

	action.Version = ""
	action.Definitions = nil

	return &jsonschema.Schema{
		Type:        "object",
		Description: "Per-name results for a multi-name lookup; names that do not exist map to null.",
		AdditionalProperties: &jsonschema.Schema{
			AnyOf: []*jsonschema.Schema{
				action,
				{Type: "null"},
			},
		},
	}
}

type inspectMultiResult struct {
	Actions InspectActions `json:"actions" jsonschema:"description=Per-name results for a multi-name lookup; names that do not exist map to null."`
}

// JSONSchema keeps the single-action payload fully typed while describing the
// multi-name payload as a separate, mutually exclusive shape.
func (InspectResult) JSONSchema() *jsonschema.Schema {
	reflector := schemaReflector()
	single := reflector.ReflectFromType(reflect.TypeFor[Action]())
	multi := reflector.ReflectFromType(reflect.TypeFor[inspectMultiResult]())

	single.Version = ""
	single.Definitions = nil
	multi.Version = ""
	multi.Definitions = nil

	return &jsonschema.Schema{
		Type:        "object",
		Description: "One Input Map action, or per-name results for a multi-name lookup.",
		OneOf: []*jsonschema.Schema{
			single,
			multi,
		},
	}
}

func schemaReflector() jsonschema.Reflector {
	return jsonschema.Reflector{
		Anonymous:      true,
		ExpandedStruct: true,
		DoNotReference: true,
	}
}
