package scene

import "github.com/davidherring123/godot-mcp/internal/port/invocation"

const ReloadName = "scene.reload"

// ReloadParams controls reloading the active saved scene from disk. Reloading
// discards unsaved editor changes only when the caller explicitly opts in, and
// it always clears that scene's undo/redo history.
type ReloadParams struct {
	ExpectedScene  string `json:"expected_scene,omitempty" jsonschema:"description=Fail if the active scene path differs; does not switch scenes."`
	DiscardUnsaved bool   `json:"discard_unsaved,omitempty" jsonschema:"description=Discard unsaved changes in the active scene before reloading it from disk. Reloading also clears that scene's undo/redo history. Defaults to false, so a scene with unsaved changes is refused; this action never saves implicitly."`
}

type ReloadResult struct {
	Scene              string   `json:"scene" jsonschema:"description=Active scene resource path that was reloaded from disk."`
	Reloaded           bool     `json:"reloaded" jsonschema:"description=Whether the editor recreated the active scene root from the current file."`
	WasUnsaved         bool     `json:"was_unsaved" jsonschema:"description=Whether the active scene had unsaved editor changes before the reload."`
	DiscardedUnsaved   bool     `json:"discarded_unsaved" jsonschema:"description=Whether unsaved changes were discarded because discard_unsaved was true."`
	UndoHistoryCleared bool     `json:"undo_history_cleared" jsonschema:"description=Whether the reload cleared the active scene's undo/redo history. Reloading always clears it."`
	StaleScripts       []string `json:"stale_scripts" jsonschema:"description=Best-effort list of scripts attached to the reloaded scene whose loaded source differs from the current file. An empty array does not guarantee that every external script cache was refreshed."`
	Note               string   `json:"note,omitempty" jsonschema:"description=Human-readable notes about discarded edits, undo/redo history, and the best-effort scope of stale_scripts."`
}

// Reload asks the editor to reload the currently edited saved scene from disk.
func Reload(projectRoot string, params ReloadParams) (ReloadResult, error) {
	return invocation.Call[ReloadParams, ReloadResult](projectRoot, ReloadName, params)
}
