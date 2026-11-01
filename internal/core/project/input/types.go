// Package input inspects and edits the project's Input Map.
package input

const (
	// EventTypeKey is an InputEventKey, identified by keycode or physical_keycode.
	EventTypeKey = "key"
	// EventTypeMouseButton is an InputEventMouseButton.
	EventTypeMouseButton = "mouse_button"
	// EventTypeJoypadButton is an InputEventJoypadButton.
	EventTypeJoypadButton = "joypad_button"
	// EventTypeJoypadMotion is an InputEventJoypadMotion.
	EventTypeJoypadMotion = "joypad_motion"
)

// Event is a single Input Map binding. Type selects which fields apply:
//
//   - key: keycode, physical_keycode, shift, ctrl, alt, meta, echo
//   - mouse_button: button_index
//   - joypad_button: button_index
//   - joypad_motion: axis, axis_value
//
// Device applies to every type. When omitted, the event keeps its native
// Godot default (16 for InputEventKey, 32 for InputEventMouseButton, and 0 for
// joypad events in Godot 4.7). An explicit device is preserved.
type Event struct {
	Type            string   `json:"type" jsonschema:"enum=key,enum=mouse_button,enum=joypad_button,enum=joypad_motion,description=Binding kind."`
	Device          *int     `json:"device,omitempty" jsonschema:"minimum=-1,description=Device ID. Omit the field to use the native Godot default for the event type (16 keyboard / 32 mouse button / 0 joypad). Set an explicit device index to preserve it; -1 is Godot's emulated-input device."`
	Keycode         *int     `json:"keycode,omitempty" jsonschema:"minimum=0,description=Keyboard key constant such as 32 for Space, matched by key label."`
	PhysicalKeycode *int     `json:"physical_keycode,omitempty" jsonschema:"minimum=0,description=Keyboard key constant matched by physical position."`
	Shift           bool     `json:"shift,omitempty" jsonschema:"description=Require Shift."`
	Ctrl            bool     `json:"ctrl,omitempty" jsonschema:"description=Require Ctrl."`
	Alt             bool     `json:"alt,omitempty" jsonschema:"description=Require Alt."`
	Meta            bool     `json:"meta,omitempty" jsonschema:"description=Require Meta."`
	Echo            bool     `json:"echo,omitempty" jsonschema:"description=Match key echo repeats."`
	ButtonIndex     *int     `json:"button_index,omitempty" jsonschema:"minimum=0,description=Mouse button or joypad button constant such as 1 for the left mouse button."`
	Axis            *int     `json:"axis,omitempty" jsonschema:"minimum=0,description=Joypad axis constant such as 0 for the left stick X axis."`
	AxisValue       *float64 `json:"axis_value,omitempty" jsonschema:"minimum=-1,maximum=1,description=Joypad axis direction, typically -1 or 1."`
}

// Action is one Input Map action and its bindings.
type Action struct {
	Name     string  `json:"name" jsonschema:"minLength=1,description=Action name such as jump."`
	Deadzone float64 `json:"deadzone" jsonschema:"minimum=0,maximum=1,description=Analog deadzone between 0 and 1."`
	Events   []Event `json:"events" jsonschema:"description=Bindings in their stored order; the index identifies an event for removal."`
}

type ListParams struct {
	IncludeBuiltin bool `json:"include_builtin,omitempty" jsonschema:"description=Include Godot's built-in input actions. Defaults to false."`
}

type ListResult struct {
	Actions []Action `json:"actions"`
}

type InspectParams struct {
	Action  string   `json:"action,omitempty" jsonschema:"minLength=1,description=Existing action name such as jump. Omit when using actions."`
	Actions []string `json:"actions,omitempty" jsonschema:"minItems=1,maxItems=100,description=Action names to look up together. Missing names map to null. Cannot be combined with action."`
}

// InspectResult is a single action payload for one-name lookups, or a map of
// per-name payloads for multi-name lookups. Missing names map to null in the
// multi-name form.
type InspectResult struct {
	Name     string         `json:"name,omitempty" jsonschema:"minLength=1,description=Action name such as jump."`
	Deadzone float64        `json:"deadzone,omitempty" jsonschema:"minimum=0,maximum=1,description=Analog deadzone between 0 and 1."`
	Events   []Event        `json:"events,omitempty" jsonschema:"description=Bindings in their stored order; the index identifies an event for removal."`
	Actions  InspectActions `json:"actions,omitempty" jsonschema:"description=Per-name results for a multi-name lookup; names that do not exist map to null."`
}

type AddParams struct {
	Action   string   `json:"action" jsonschema:"minLength=1,description=New action name such as jump. Fails if the action already exists."`
	Deadzone *float64 `json:"deadzone,omitempty" jsonschema:"minimum=0,maximum=1,description=Analog deadzone between 0 and 1; defaults to 0.5."`
}

type AddResult struct {
	Action   string  `json:"action"`
	Deadzone float64 `json:"deadzone"`
	Events   []Event `json:"events"`
	Changed  bool    `json:"changed"`
}

type SetParams struct {
	Action   string  `json:"action" jsonschema:"minLength=1,description=Existing action name such as jump."`
	Deadzone float64 `json:"deadzone" jsonschema:"minimum=0,maximum=1,description=New analog deadzone between 0 and 1."`
}

type SetResult struct {
	Action   string  `json:"action"`
	Deadzone float64 `json:"deadzone"`
	Events   []Event `json:"events"`
	Changed  bool    `json:"changed" jsonschema:"description=Whether the action changed; false when the deadzone already matched."`
}

type RenameParams struct {
	Action  string `json:"action" jsonschema:"minLength=1,description=Existing action name to rename."`
	NewName string `json:"new_name" jsonschema:"minLength=1,description=New action name. Fails if an action with that name already exists."`
}

type RenameResult struct {
	Action   string  `json:"action" jsonschema:"description=Action name before the rename."`
	NewName  string  `json:"new_name"`
	Deadzone float64 `json:"deadzone"`
	Events   []Event `json:"events"`
	Changed  bool    `json:"changed"`
}

type DeleteParams struct {
	Action string `json:"action" jsonschema:"minLength=1,description=Existing action name to remove."`
}

type DeleteResult struct {
	Action  string `json:"action"`
	Deleted bool   `json:"deleted"`
}

type EventAddParams struct {
	Action string `json:"action" jsonschema:"minLength=1,description=Existing action name to add a binding to."`
	Event  Event  `json:"event" jsonschema:"description=Binding to add."`
}

type EventAddResult struct {
	Action  string `json:"action"`
	Index   int    `json:"index" jsonschema:"description=Zero-based position of the new binding."`
	Event   Event  `json:"event"`
	Changed bool   `json:"changed"`
}

type EventDeleteParams struct {
	Action string `json:"action" jsonschema:"minLength=1,description=Existing action name to remove a binding from."`
	Index  int    `json:"index" jsonschema:"minimum=0,description=Zero-based position of the binding to remove."`
}

type EventDeleteResult struct {
	Action  string  `json:"action"`
	Index   int     `json:"index" jsonschema:"description=Index that was removed."`
	Events  []Event `json:"events" jsonschema:"description=Remaining bindings."`
	Changed bool    `json:"changed"`
}

// ApplyOperation is one ordered Input Map mutation. Operation accepts the
// canonical short name or its input.* action alias. The fields apply as:
//
//   - add / input.add: action and optional deadzone
//   - set / input.set: action and deadzone
//   - rename / input.rename: action and new_name
//   - delete / input.delete: action
//   - event_add / input.event_add: action and event
//   - event_delete / input.event_delete: action and index
//
// Operation summaries always report the canonical short name.
type ApplyOperation struct {
	Operation string   `json:"operation" jsonschema:"enum=add,enum=input.add,enum=set,enum=input.set,enum=rename,enum=input.rename,enum=delete,enum=input.delete,enum=event_add,enum=input.event_add,enum=event_delete,enum=input.event_delete,description=Operation kind using the short name or its input.* action alias. Summaries report the short name."`
	Action    string   `json:"action" jsonschema:"minLength=1,description=Input Map action name such as jump."`
	Deadzone  *float64 `json:"deadzone,omitempty" jsonschema:"minimum=0,maximum=1,description=Analog deadzone between 0 and 1 for add or set."`
	NewName   string   `json:"new_name,omitempty" jsonschema:"minLength=1,description=New action name for rename."`
	Event     *Event   `json:"event,omitempty" jsonschema:"description=Binding to add for event_add."`
	Index     *int     `json:"index,omitempty" jsonschema:"minimum=0,description=Zero-based binding index for event_delete."`
}

type ApplyParams struct {
	Operations []ApplyOperation `json:"operations" jsonschema:"minItems=1,maxItems=100,description=Ordered Input Map operations to validate and apply."`
	DryRun     bool             `json:"dry_run,omitempty" jsonschema:"description=Validate the full batch without saving project.godot or reloading the Input Map."`
}

type ApplyOperationSummary struct {
	Index        int    `json:"index" jsonschema:"description=Zero-based position of the operation in the batch."`
	Operation    string `json:"operation" jsonschema:"description=Canonical short operation name, even when the request used an input.* alias."`
	Action       string `json:"action"`
	Status       string `json:"status" jsonschema:"description=Outcome such as added, updated, renamed, deleted, binding_added, binding_deleted, or unchanged."`
	Changed      bool   `json:"changed"`
	NewName      string `json:"new_name,omitempty"`
	BindingIndex *int   `json:"binding_index,omitempty" jsonschema:"description=Zero-based binding position for event_add and event_delete."`
	Event        *Event `json:"event,omitempty" jsonschema:"description=Binding saved by event_add, as stored in the Input Map."`
}

type ApplyResult struct {
	DryRun         bool                    `json:"dry_run"`
	Applied        bool                    `json:"applied" jsonschema:"description=Whether the batch ran in apply mode; false for a dry run. An all-no-op batch does not rewrite project.godot."`
	OperationCount int                     `json:"operation_count"`
	Changed        int                     `json:"changed" jsonschema:"description=Number of operations that changed an action or binding."`
	Unchanged      int                     `json:"unchanged"`
	Operations     []ApplyOperationSummary `json:"operations"`
}
