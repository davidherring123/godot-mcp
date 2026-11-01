---
name: godot-mcp
description: Optional workflow guidance for building and editing Godot projects with the Godot MCP server.
---

# Godot MCP workflow

Use the Godot MCP tools for supported semantic inspection and mutation of scenes,
nodes, resources, project settings, and the Input Map. Tool descriptions and
schemas are authoritative for exact names, arguments, and results.

Use file tools for scripts, shaders, documentation, repository search, exact
serialization, and line references. Use Git for diffs and history. Prefer MCP
over directly editing `.tscn`, `.tres`, or `project.godot`: text edits bypass
Godot validation and can conflict with an open editor.

## Work deliberately

- Call the targeted tool directly when the target and desired values are known.
  Use the narrowest inspection needed only when information is missing.
  For discovery, use a targeted search or `references/actions.md`, then load
  only the relevant action schema. Avoid listing every full tool schema.
- Pass the known scene as `expected_scene` for active-scene work. This guards
  against editing the wrong tab. Use `scene_tree` when the active scene or node
  path is unknown.
- Treat mutation results as confirmation of applied values. Do not re-inspect
  solely to confirm them; inspect again only to answer a new question. A successful
  save writes the whole live scene, including unrelated unsaved changes.
- If an operation reports that no editor is available, open this project in
  Godot with the addon enabled, then retry. `validate_scene` checks the active
  editor scene and therefore also requires the editor.

Batch related scene and node edits. They are editor undo steps; save once on the
last mutation or with `scene_save`, rather than after every call. Save intended
changes before reviewing Git because live editor state may differ from disk.
Resource, project-setting, and Input Map mutations persist immediately and are
not editor undo steps.

After changing a script's exported default, review the saved `.tscn` diff for
stale values written as overrides by an already-open editor. Successful mutation
and validation do not prove that only the requested properties were saved. Do
not remove overrides automatically: they may be intentional instance settings.

For several ordered edits, prefer `scene_apply`, `resource_apply`,
`project_settings_apply`, or `input_map_apply` over many granular mutations.
Each batch accepts up to 100 operations and supports best-effort `dry_run`;
scene batches are one undo step and can reference earlier results with `@id`,
while the other batches save and roll back only within their own domain.

When instancing a scene, save its source first. Keep linked instances reusable:
put shared structure and behavior in the source scene and reserve per-instance
overrides for genuine variation.

Use inline resource values for scene-local meshes, materials, and shapes instead
of creating a separate file solely to assign a resource. Nested property paths
such as `position:x` and `material:shader_parameter/curvature` are supported.
Nested resource edits make a local copy so previews and undo remain isolated;
use `resource_set` when intentionally editing a shared resource file. Put parent
and child-path edits in separate batch operations rather than one properties
object. Use `node_overrides` to inspect instance values before changing them.

Typed Resource arrays accept whole-array replacements containing saved resource
references, inline resources, and `null` elements. Elements must match the
declared Resource class, including custom script types. Use `[]` to clear an
array; nested arrays and inspection-only `ref`/`cycle` graphs are not decoded.

## Godot structure

Match architecture to the request and the existing project. For gameplay work,
prefer Godot's native composition where appropriate: reusable scenes, standard
body and collision nodes, resources or exported properties for configuration,
and signals for decoupled communication. Put behavior and procedural generation
in scripts rather than constructing every reusable node tree at runtime.

“Small” or “minimal” usually means less scope, not forcing everything into one
script or node. Avoid hand-rolled physics, a single owner for unrelated systems,
or custom drawing as a substitute for ordinary scene structure unless the task
or project calls for it. Do not restructure an intentional existing design just
to impose these defaults.

## Validate the right claim

Before reporting a tool limitation, reproduce the exact failing case using the
documented value shape. Packed vector arrays use objects, for example
`[{"x": 0, "y": 1}, {"x": 2, "y": 3}]` for `PackedVector2Array`.

Use `validate_scene` to check the active editor scene, including unsaved edits,
for script and serialization errors. It does not run the game or prove that
gameplay is correct. Use the runtime tools when behavior matters, and report
that distinction clearly.

Use `runtime_start`, `runtime_status`, and `runtime_capture` when visual or
runtime behavior matters, then call `runtime_stop`. Capture requires a windowed
runtime; headless sessions still report frames, logs, and parsed runtime errors.
Use `runtime_wait` or capture's `wait_frames` / `wait_seconds` to observe progress
without polling status in a loop. Choose idle or physics frames explicitly when
the distinction matters. These waits do not pause or deterministically step the
game. `runtime_status` includes heartbeat age; its frame counts can lag. Use
`runtime_logs` with byte cursors to read prints and errors directly.
Use `runtime_input` to exercise the real interaction path instead of temporarily
patching gameplay scripts. Send motion before a GUI click and explicit releases
after holds. Action events affect polling; key/mouse events also reach input
callbacks. Virtual cursor coordinates do not isolate real OS input, and a
diagnostic pick result does not prove the game's handler ran.
