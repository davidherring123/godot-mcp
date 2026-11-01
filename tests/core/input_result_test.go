package core_test

import (
	"encoding/json"
	"testing"

	"github.com/davidherring123/godot-mcp/internal/core/project/input"
)

func TestInspectResultKeepsSingleActionShape(t *testing.T) {
	data, err := json.Marshal(input.InspectResult{
		Name:     "jump",
		Deadzone: 0.5,
		Events: []input.Event{
			{Type: input.EventTypeKey, Keycode: intPtr(32)},
		},
	})
	if err != nil {
		t.Fatalf("encoding the single-action result: %v", err)
	}

	var payload map[string]json.RawMessage

	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decoding the single-action result: %v", err)
	}

	if len(payload) != 3 {
		t.Fatalf("expected name, deadzone, and events, got %s", data)
	}

	for _, key := range []string{"name", "deadzone", "events"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected %s in %s", key, data)
		}
	}

	if _, ok := payload["actions"]; ok {
		t.Fatalf("single-action result leaked the multi-name field: %s", data)
	}
}

func TestInspectResultMultiShapeMapsMissingToNull(t *testing.T) {
	data, err := json.Marshal(input.InspectResult{
		Actions: input.InspectActions{
			"jump": {
				Name:     "jump",
				Deadzone: 0.5,
				Events:   []input.Event{},
			},
			"missing": nil,
		},
	})
	if err != nil {
		t.Fatalf("encoding the multi-name result: %v", err)
	}

	var payload map[string]json.RawMessage

	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decoding the multi-name result: %v", err)
	}

	if len(payload) != 1 {
		t.Fatalf("expected only actions, got %s", data)
	}

	var actions map[string]*input.Action

	if err := json.Unmarshal(payload["actions"], &actions); err != nil {
		t.Fatalf("decoding the per-name actions: %v", err)
	}

	if actions["jump"] == nil || actions["jump"].Name != "jump" {
		t.Fatalf("unexpected jump result: %s", data)
	}

	missing, ok := actions["missing"]

	if !ok {
		t.Fatalf("expected a null entry for the missing action: %s", data)
	}

	if missing != nil {
		t.Fatalf("expected a null entry for the missing action: %s", data)
	}
}
