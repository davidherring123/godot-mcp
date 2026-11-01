package input

import (
	"encoding/json"
	"fmt"
)

const InspectName = "input.inspect"

const maxInspectActions = 100

func Inspect(projectRoot string, params InspectParams) (InspectResult, error) {
	if params.Actions != nil {
		if err := validateInspectActions(params); err != nil {
			return InspectResult{}, err
		}
	} else if err := validateAction(params.Action); err != nil {
		return InspectResult{}, err
	}

	return call[InspectParams, InspectResult](projectRoot, InspectName, params)
}

func validateInspectActions(params InspectParams) error {
	if params.Action != "" {
		return validationError("Provide action or actions, not both")
	}

	if len(params.Actions) == 0 {
		return validationError("At least one action name is required")
	}

	if len(params.Actions) > maxInspectActions {
		return validationError(
			fmt.Sprintf("At most %d action names are allowed", maxInspectActions),
		)
	}

	for _, action := range params.Actions {
		if err := validateAction(action); err != nil {
			return err
		}
	}

	return nil
}

// MarshalJSON keeps the single-action contract unchanged and emits the
// per-name form only when the result came from a multi-name lookup.
func (result InspectResult) MarshalJSON() ([]byte, error) {
	if result.Actions != nil {
		return json.Marshal(struct {
			Actions InspectActions `json:"actions"`
		}{Actions: result.Actions})
	}

	events := result.Events
	if events == nil {
		events = []Event{}
	}

	return json.Marshal(Action{
		Name:     result.Name,
		Deadzone: result.Deadzone,
		Events:   events,
	})
}
