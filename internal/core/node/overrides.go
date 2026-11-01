package node

import (
	"encoding/json"

	"github.com/davidherring123/godot-mcp/internal/port/invocation"
	"github.com/davidherring123/godot-mcp/internal/protocol/apierr"
)

const OverridesName = "node.overrides"

type OverridesParams struct {
	Node          string   `json:"node" jsonschema:"minLength=1,description=Node path relative to the active editor scene root; it must identify a linked PackedScene instance root."`
	Properties    []string `json:"properties,omitempty" jsonschema:"description=Override property names to include; all recorded override properties are returned when omitted."`
	ExpectedScene string   `json:"expected_scene,omitempty" jsonschema:"description=Fail if the active scene path differs; does not switch scenes."`
}

// OverridesResult inventories overrides recorded on a linked instance. Live
// values come from the active editor scene packed in memory; disk values come
// from the saved scene file. Effective values are read from the live node.
type OverridesResult struct {
	Scene       string             `json:"scene"`
	Path        string             `json:"path" jsonschema:"description=Node path relative to the scene root."`
	Name        string             `json:"name"`
	Type        string             `json:"type" jsonschema:"description=Native Godot class of the instance root."`
	SourceScene string             `json:"source_scene" jsonschema:"description=Resource path of the linked PackedScene."`
	Linked      bool               `json:"linked" jsonschema:"description=Whether the target is a linked PackedScene instance root. This action fails instead of returning false for non-instance nodes."`
	Unsaved     bool               `json:"unsaved" jsonschema:"description=Whether the active scene has unsaved editor changes."`
	DiskStatus  string             `json:"disk_status" jsonschema:"enum=compared,enum=unavailable,description=Whether the saved scene file could be read for comparison."`
	Root        []PropertyOverride `json:"root" jsonschema:"description=Properties overridden on the instance root."`
	Children    []NodeOverrides    `json:"children" jsonschema:"description=Editable child properties recorded under the instance root."`
	Truncated   bool               `json:"truncated" jsonschema:"description=Whether property or child-node bounds omitted part of the inventory."`
	Limitations []string           `json:"limitations" jsonschema:"description=Honest coverage limits for native scene-state inspection, baseline inference, and disk comparison."`
}

type PropertyOverride struct {
	Property       string          `json:"property"`
	Value          json.RawMessage `json:"value,omitempty" jsonschema:"description=Serialized value recorded in the live packed scene state; omitted when the override exists only on disk."`
	Effective      json.RawMessage `json:"effective,omitempty" jsonschema:"description=Serialized value read from the live node, when the property is still available."`
	Baseline       json.RawMessage `json:"baseline,omitempty" jsonschema:"description=Serialized source-scene or source-default value, when reliably available."`
	BaselineSource string          `json:"baseline_source,omitempty" jsonschema:"enum=source_scene,enum=inherited_scene,enum=default,description=Where the baseline came from: an explicit source-scene or inherited-scene property, or an in-memory source default that may be a script or engine default."`
	Live           bool            `json:"live" jsonschema:"description=Whether the property is present in live packed state; for editable children this is a live value that differs from the source scene."`
	Inferred       bool            `json:"inferred" jsonschema:"description=Whether a live editable-child value was inferred because it differs from the source scene but was not recorded in the saved child scene state. It is a save candidate, not proof of an intentional override."`
	OnDisk         bool            `json:"on_disk" jsonschema:"description=Whether the property is recorded in the saved scene file."`
	DiskValue      json.RawMessage `json:"disk_value,omitempty" jsonschema:"description=Serialized value recorded in the saved scene file, when available."`
}

type NodeOverrides struct {
	Path       string             `json:"path" jsonschema:"description=Node path relative to the linked instance root."`
	Type       string             `json:"type,omitempty" jsonschema:"description=Native Godot class of the live child, when available."`
	Properties []PropertyOverride `json:"properties"`
}

func Overrides(projectRoot string, params OverridesParams) (OverridesResult, error) {
	if params.Node == "" {
		return OverridesResult{}, &apierr.Error{
			Code:    apierr.CodeInvalidArgument,
			Message: "A node path is required",
		}
	}
	for _, property := range params.Properties {
		if property == "" {
			return OverridesResult{}, &apierr.Error{
				Code:    apierr.CodeInvalidArgument,
				Message: "Property filters must be nonempty",
			}
		}
	}
	return invocation.Call[OverridesParams, OverridesResult](projectRoot, OverridesName, params)
}
