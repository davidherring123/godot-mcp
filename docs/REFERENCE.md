# godot-mcp reference

For quick installation and setup, see the [README](../README.md).

A Godot 4 editor addon that exposes live project automation over the Model Context Protocol (MCP).

`godot-mcp` runs inside Godot. Its **Godot MCP** bottom panel manages a private,
bundled MCP host and configures supported AI clients for the current project.
There is no public command-line interface to install, configure, or drive the
addon: installation, client setup, and day-to-day use all start in the editor.

> [!NOTE]
> `godot-mcp` is currently in early development.

## What it provides

- MCP tools for inspecting and mutating scenes, nodes, resources, project
  settings, and the Input Map.
- Live editor access, including the active scene and unsaved changes.
- Ordered batch operations with best-effort `dry_run` previews and rollback
  limits documented below.
- Scene validation that reports script and serialization errors without saving,
  opening, or running the scene.
- Runtime tools that start the game, report logs and frame counts, and capture
  screenshots from a windowed session.
- Project-scoped client setup for OpenCode, Claude Code, and Codex from the
  editor panel, including the portable workflow skill.

## Requirements

- Godot 4.
- A supported platform with a bundled host: macOS arm64/x86_64, Linux
  x86_64/arm64, or Windows x86_64.

Go is only required to build the addon host from source. End users do not need Go,
a separately installed binary, or any terminal setup.

## Installation

Install the addon with one of these methods:

### GodotEnv

Reference the release ZIP as a `source: "zip"` addon and install it with
GodotEnv:

```jsonc
{
  "addons": {
    "godot_mcp": {
      "url": "https://github.com/davidherring123/godot-mcp/releases/download/<version>/godot-mcp-addon-<version>.zip",
      "source": "zip",
      "subfolder": "addons/godot_mcp"
    }
  }
}
```

```sh
godotenv addons install
```

### Release ZIP

Download `godot-mcp-addon-<version>.zip` from the releases page and extract it
at the project root. Do not use the repository's auto-generated "Source code"
ZIP; it does not contain the host binaries.

### Enable the addon

After installing, enable **Godot MCP** in **Project Settings > Plugins** if it
is not already enabled. The **Godot MCP** panel is added to the bottom panel and
the bundled backend starts automatically. If the panel reports that the backend
is not included, use a release archive rather than a development checkout.

When upgrading from a pre-rename `godot-cli` build, disable that plugin and
remove `addons/godot_cli/` before installing this addon. Then use **Install** or
**Repair** in the Godot MCP panel so client configurations point at the renamed
host path. The new addon uses `addons/godot_mcp/`, `.godot/godot-mcp/`, and
`godot-mcp` skill directories; old generated state and skill directories may be
removed after the migration.

Release archives place the addon at `addons/godot_mcp/` and the matching host at
`addons/godot_mcp/bin/<os>-<arch>/godot-mcp-host` (`godot-mcp-host.exe` on
Windows). See [RELEASING.md](../RELEASING.md) for archive layout and release
instructions.

## Configure AI clients

Open the project in Godot and use the **Godot MCP** bottom panel:

1. Use **MCP Status** for backend health, endpoint details, and lifecycle
   controls. It warns when no AI client integration is configured.
2. Open **Integrations** to manage client registration and workflow skills.
3. **Refresh Status** reads the current MCP registration and workflow-skill
   state for each client.
4. **Install** or **Repair** updates one client; **Install All** updates every
   supported client.
5. **Start**, **Restart**, and **Stop** control the bundled backend. **Copy
   Endpoint** copies the loopback Streamable HTTP endpoint when a client needs
   to connect to it directly.

Installation writes project-scoped configuration and the portable skill. It
preserves unrelated configuration, and repeated installs are no-ops when the
project is already current.

| Client | MCP configuration | Skill |
|---|---|---|
| OpenCode | `mcp.servers` in `opencode.json(c)` or `.opencode/opencode.json(c)` | `.agents/skills/godot-mcp/SKILL.md` |
| Claude Code | `.mcp.json` | `.claude/skills/godot-mcp/SKILL.md` |
| Codex | `.codex/config.toml` | `.agents/skills/godot-mcp/SKILL.md` |

The generated client configuration launches the bundled host with its private
`connect --project <absolute-project-root>` entrypoint over stdio. The host is
an implementation detail of the addon, not a supported user-facing command-line
interface. Its only commands are the internal `serve`, `connect`,
`integrations`, and `version` commands used by the panel and generated client
configuration.

## Editor and runtime lifecycle

Live editor tools require this project to be open in Godot with the addon
enabled. The addon registers the editor instance when it starts; if no editor is
registered, open the project in Godot and retry. There is no command to launch
or inspect the editor.

Runtime tools start a separate game process instead of asking the editor to play
a scene. They use the Godot executable reported by the connected editor and
therefore also require an editor registered for the project. Runtime sessions do
not fall back to `GODOT_BIN` or a `godot` executable on `PATH`.

Runtime state is scoped to one project and one active session at a time.
Sessions, logs, and captures live under
`.godot/godot-mcp/runtime/sessions/<session>/`, and every artifact path stays
inside that directory. Start defaults to a windowed session so screenshots work;
`headless` remains available for logs and error feedback, and capture returns an
`unsupported` error instead of a blank image. Stop is idempotent and refuses to
kill an unresponsive process unless `force` is set.

`runtime_wait` waits for idle or physics frames, elapsed seconds, or both; it
does not pause the game between calls. `runtime_capture` accepts `wait_frames`,
`wait_seconds`, and `basis` and captures after rendering. Waits are bounded to
600 frames / 30 seconds, with a maximum request timeout of 60 seconds. A long
request holds the MCP server's serialization lock until completion or timeout.
Queue time counts toward the request deadline. The runner expires requests
slightly before the host timeout to return failures and partial input counts;
a fully stalled process may still reach the host timeout without a report.
Capture fails if its post-render barrier receives no draw signal within five
seconds. Sessions started by older hosts must be stopped and restarted to use
wait, input, or capture waits.
`runtime_logs` reads bounded stdout/stderr slices with byte cursors, including
after exit. An incomplete UTF-8 character at the current end of a log is held
until more bytes arrive; its cursor does not advance and `truncated` remains
true. A cursor inside a character skips its remaining continuation bytes.
Malformed bytes use JSON's replacement-character behavior.
`runtime_status` reports observation time and heartbeat age so callers
can distinguish cached frame counts from a fresh runtime observation.

`runtime_input` sends short ordered sequences of action, key, mouse, scroll, or
gesture events. Action press/release changes polling state; key/mouse/gesture
events also use Godot's input-event route. Explicit press/release and frame
spacing support holds and drags. A GUI click should start with mouse motion.
The virtual cursor maintains synthetic viewport coordinates across calls; it
does not capture the desktop pointer or prevent real input from interleaving.
Direct `picks` results are diagnostic queries, not proof that gameplay handled
an event. Verify the actual game response with captures, logs, or fixture checks.

## MCP tools

`editor_capture` captures the editor's 2D or 3D viewport rather than the running
game or the full editor UI. It requires a graphical editor.
`filesystem_scan` waits for resource indexing and can verify existing path/UID
mappings. Check `completed`, `timed_out`, and each path's `uid_valid`; scanning
does not generate missing UIDs or guarantee every UID warning is repaired.
Resource and scene creation also wait for bounded UID registration using the
UID already written by Godot. Check `uid_registered`, or `index_pending` on a
resource batch. If indexing times out, the file still exists: run
`filesystem_scan` and retry the dependent operation instead of creating it again.

Tool names use underscores: `editor_status`, `scene_list`, `scene_apply`,
`node_add`, `resource_set`, `input_event_add`, `validate_scene`, `runtime_start`,
`runtime_capture`, and so on. Every successful call returns JSON text plus
structured content. `runtime_capture` also returns an MCP image content block
when the PNG fits the embedding limit; otherwise its structured result keeps the
artifact path, dimensions, and byte size. Tool failures set `isError` and
preserve the core error code (`invalid_argument`, `not_found`,
`failed_precondition`, `unsupported`, and others) so the client can recover.
Errors include `code` and `message` as structured fields, with JSON text retained
for clients that only display text content. For tool discovery, use targeted
searches or the installed skill's `references/actions.md` index before loading
individual schemas.

### Scene editing

`scene_reload` reloads the active saved scene from disk and clears its undo/redo
history. It refuses unsaved changes unless `discard_unsaved: true` is explicit;
use `expected_scene` to guard the target. Its `stale_scripts` list is a best-effort
comparison of attached script sources, not a guarantee that every dependency
cache has refreshed. Reloading does not remove already-saved overrides.

`node_set` accepts nested indexed paths using Godot NodePath subname syntax,
for example `position:x` or `material:shader_parameter/curvature`. The same
property shapes work in `node_add`, `node_instantiate`, and `scene_apply`
operations. Each segment resolves against the node, a resource, or a built-in
vector-like value; unknown, read-only, or type-mismatched leaves fail without
changing the scene. A nested write to a resource duplicates the resource root
and intermediate resources first, so shared embedded and external resources are
not mutated, and undo restores the original resource identity. Setting a
property together with one of its nested paths in the same properties object is
rejected as ambiguous; separate batch operations apply in order.

`scene_apply` is the batch tool. It accepts 1–100 ordered `operations` of node
mutations (`node.add`, `node.set`, `node.rename`, `node.duplicate`, `node.move`,
`node.delete`, `node.instantiate`), validates the whole batch on a temporary
copy of the active scene, and commits it as one editor undo step. Each operation
uses the `operation` field and only the fields valid for that kind; operations
can carry an `id`, and later operations can target the resulting node with
`@id`. Set `dry_run` to return best-effort planned paths and summaries without
editing.

The preview never writes remapped paths into live shared subresources, so
dry-run and failed prevalidation do not alter animation tracks or
NodePath-bearing resources. Prevalidation still uses a packed copy, so
editor-only and unowned runtime nodes are not modeled; the live pass validates
and rolls back on such drift. If any operation fails during the live pass,
earlier operations are reversed directly without adding the failed batch to undo
history, so a previously clean scene stays clean. Nodes created by the failed
batch are freed, and an incomplete rollback is reported as `internal_error` with
restored/total counts and failure details.

Result paths are snapshots at the time each operation completed, not final paths
after later renames, moves, or deletes, and an id cannot be referenced after its
node is deleted. `save` runs once after the batch succeeds; if the save fails,
the batch remains applied and undoable, so retry with `scene_save` rather than
re-running the batch. The result names the exact operation index, `operation`,
and id on failure. The granular node tools remain available and behave as
before.

Successful scene saves include `save_report`, a bounded before/after disk diff
covering the whole scene file, including unrelated earlier unsaved edits.
Inspect `available` and `diff_truncated`; a truncated report is not a complete
change inventory. Binary `.scn` files report byte changes rather than a text diff.
After changing exported script defaults, check for stale serialized overrides.
The report diagnoses what was written; it does not determine whether an override
was intentional or automatically revert it.

`node_overrides` inventories a linked scene instance's overrides, separating
live packed values, saved values, effective values, and source-scene baselines.
Use its `properties` filter for focused output and check `truncated`. Child
differences marked `inferred` are save candidates, not proof of explicit saved
overrides. Inspection does not infer intent or automatically revert values.

### Resources, project settings, and Input Map

`input_list` excludes built-in actions by default; set `include_builtin: true`
for the complete list. `input_inspect` accepts either one `action` or an `actions`
array; multi-name results map missing names to `null`. Input Map batch binding
additions return the stored event, including its device. Omitted devices retain
Godot's native defaults (Godot 4.7: keyboard `16`, mouse `32`, joypad `0`);
explicit devices are preserved.

Resource properties accept references such as `{"resource":"res://mesh.tres"}`
or inline values such as `{"type":"BoxMesh","properties":{"size":{"x":1,"y":1,"z":1}}}`.
Anonymous resources are inspected with bounded type/property graphs; repeated
references and cycles have identity markers. Use `properties` filters on
`node_inspect` and `resource_inspect` to keep output focused. Embedded resource
addresses such as `res://scenes/main.tscn::ShaderMaterial_crt` are inspectable;
the result distinguishes active-scene values from disk-loaded values. File
mutation tools still require standalone `.tres` or `.res` paths.

Batch tools accept qualified action names consistently: `node.set`,
`resource.set`, `project.settings.set`, and `input.event_add`. Resource,
project-setting, and Input Map batches retain their existing short aliases and
return short operation names in summaries.

For ordered multi-step edits within one domain, use `resource_apply`,
`project_settings_apply`, and `input_map_apply` (the underlying action names are
`resource.apply`, `project.settings.apply`, and `input.map_apply`). Each accepts
1–100 operations, applies them in order, returns a compact per-operation
summary, and supports `dry_run` to validate the full batch without writing.
Later resource create/set operations may reference resources created earlier in
the same batch. Batch failures identify the exact operation.

Resource batches attempt a best-effort restore of every affected file written by
earlier operations; project-setting and Input Map batches apply in memory and
save `project.godot` once, skipping the save when every operation is a no-op.
Rollback is best-effort: if it fails, the error code is `internal_error` because
the editor may be partially changed. `resource_apply` tracks references
introduced earlier in a batch conservatively, so removing a reference earlier in
the same batch can still block a later delete.

Wherever a node or resource property accepts a `{"resource": "res://..."}`
reference, it also accepts an inline anonymous resource such as
`{"type": "BoxMesh", "properties": {"size": {"x": 1, "y": 1, "z": 1}}}`.
Inline values are created through the same resource factory as `resource_create`,
checked against the target property's resource-type hint, and saved as embedded
subresources rather than extra `.tres` files. Custom Resource scripts run their
constructors and setters while inline values are decoded, including during
validation and `dry_run`.
Property edits cannot reassign `resource_path`; use resource actions to manage
file paths.

Typed Resource arrays support whole-array replacement with saved references,
inline resources, and `null` elements. For example, an `Array[Mesh]` accepts
`[{"resource":"res://meshes/shared.tres"},{"type":"BoxMesh"},null]`.
An empty array clears the contents while preserving its declared element type.
Each element must match the native or custom Resource class declared by the
property; incompatible elements reject the whole edit before assignment.
Custom script types and inherited Resource types are supported. Top-level
`null` does not clear an array; use `[]`. Nested arrays are not decoded.

Inspection returns saved references in the existing
`{"resource": "res://...", "type": "..."}` shape. Anonymous resources return a
bounded `type`/`properties` graph with identity `id` values; repeated references
use `ref`, and cyclic references use `ref` plus `cycle: true` instead of
recursing forever. Use the `properties` name filter on `node_inspect` or
`resource_inspect` to avoid dumping a large embedded graph.
Graphs containing `truncated`, `ref`, or `cycle` markers are inspection-only:
writes reject them rather than reconstructing missing data as defaults. Complete
inline graphs may retain their `id` annotation. Saved references accept the
inspection `type` annotation as advisory.

`resource_inspect` also accepts read-only embedded addresses such as
`res://scenes/main.tscn::BoxMesh_abc`. Its `source` reports `disk` for resource
files and saved embedded values, or `live` when the named value is resolved from
the open editor scene; `unsaved_changes` indicates whether that scene has
unsaved edits. Embedded addresses are not accepted by `resource_create`,
`resource_set`, or `resource_delete`. `resource_apply` operation names accept
either the short `create`/`set`/`delete` names or the qualified
`resource.create`/`resource.set`/`resource.delete` aliases, and summaries always
use the short names.

A resource `dry_run` can still instantiate resources and run custom Resource
scripts and property setters in memory, so scripts with external side effects
are not side-effect free. Atomicity is per domain only: these tools never make a
resource, project-setting, or Input Map batch atomic with each other, and they
do not promise crash recovery mid-write.

### Validation

`validate_scene` checks the active editor scene, including unsaved changes,
without saving, opening, switching, or running it. It reports script and
serialization errors but does not prove gameplay correctness. Use the runtime
tools when behavior matters.

## Project layout

- **Godot addon** — provides the editor plugin, TCP bridge, live editor
  operations, the **Godot MCP** panel, and the backend supervisor.
- **Private Go host** — bundles the MCP server, core actions, editor/runtime
  adapters, client integration installer, and the portable skill. It is shipped
  inside the addon and launched by the panel or generated client configuration.

See [ARCHITECTURE.md](../ARCHITECTURE.md) for package boundaries, request flow, and
the action extension recipe.

## Contributing

Development, test, and release commands are documented in
[CONTRIBUTING.md](../CONTRIBUTING.md) and [RELEASING.md](../RELEASING.md). The
contributor-only release flow uses `make release-addon`; it is not a user
installation step.

## License

This project is licensed under the MIT License. See [LICENSE](../LICENSE).
