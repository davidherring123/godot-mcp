# Changelog

## 0.2.0 — 2026-11-01

Changes since `v0.1.0`. Release tags, archive names, and reported product versions
now use numeric versions without a `v` prefix.

### Added

- Inline anonymous resources for node and resource properties, including meshes,
  materials, and shapes, using the shared resource factory and type checks.
- Whole-array assignment for typed Resource arrays: saved references, inline
  resources, null elements, native subclasses, and custom Resource script types.
- Nested indexed property editing, such as `position:x` and material properties,
  with local copies for nested resource edits and editor undo/redo support.
- `node_overrides` for inspecting scene-instance overrides.
- `scene_reload`, with an unsaved-change guard and explicit discard option.
- Save diagnostics reporting bounded text diffs for `.tscn` files and changed-byte
  information for binary scenes. Reports include other unsaved edits in the scene.
- `filesystem_scan` for waiting on editor indexing and checking resource UIDs.
- `editor_capture` for rendered editor viewports.
- `runtime_input` for bounded action, keyboard, mouse, scroll, and gesture sequences;
  `runtime_wait` for idle/physics frames or elapsed time; and `runtime_logs` for
  incremental stdout/stderr reads using byte cursors.
- Runtime capture wait options and post-render synchronization, plus status
  observation timestamps and heartbeat-age metadata.

### Improved

- Input Map listing can filter built-in actions; inspection supports multiple
  action names and returns null for missing names. Event results reflect stored
  Godot values.
- Resource inspection supports embedded subresource addresses and identifies
  disk versus live editor sources.
- Resource, Input Map, and project-settings batches accept qualified action-name aliases.
- Scene listing excludes addon scenes by default, with an option to include them.
- Control layout-property handling, ShaderMaterial shader/parameter assignment
  order, and packed-array validation diagnostics.
- MCP failures provide structured error codes/messages alongside text content.
- Documentation and generated client guidance describe the expanded workflows.

### Reliability and safety

- Resource/scene writes register UIDs with the editor filesystem; batch write
  tracking preserves registration after trailing no-op operations.
- Property edits reject resource-path reassignment that could cause a later save
  to overwrite another resource file. Resource file actions reject lexical paths
  escaping the project, including rollback snapshot paths.
- Saved resource references retain inspect-to-write compatibility. Incomplete
  resource graphs carrying `truncated`, `ref`, or `cycle` markers are rejected
  rather than reconstructed with missing values or incorrect identity.
- Nested creation/set results report the requested indexed values correctly.
  Unchanged save reports describe observed disk bytes without claiming the
  editor buffer matched or the write necessarily occurred.
- Runtime request deadlines include queue time and leave a response margin for
  partial-input delivery reports. A capture watchdog releases the queue when no
  post-render signal arrives. Protocol checks give older sessions a restart
  instruction for new automation features.
- Windows request-file sharing failures are retried without consuming the request
  or misreporting it as malformed JSON.
- Log cursors retain incomplete UTF-8 characters until appended and handle
  mid-character cursors and malformed bytes without dropping the valid tail.

### Packaging and development

- Removed addon `.uid` sidecars from source and excluded them from release
  archives and development installs. Godot generates them in the destination
  project; embedded scene/resource UIDs are retained.
- Added a canonical product version and corrected bundled-host version stamping.
- Enforced LF checkouts for Go sources so formatting checks work on Windows.
- Bounded `make test` to two minutes per package and documented timeout limits
  for agent commands, including delegated work.

### Upgrade notes and limitations

- Disable the plugin and replace the old `addons/godot_mcp` directory with the
  new archive instead of overlaying it, so previously shipped sidecars are not
  retained. Re-enable the plugin and repair client integrations if needed.
- Stop and restart existing runtime sessions before using wait/input/capture-wait
  features from the new host.
- Typed Resource arrays support whole-array replacement; nested arrays and
  `ref`/`cycle` graph reconstruction remain unsupported.
- Virtual cursor coordinates do not isolate physical keyboard/mouse input.
  A fully stalled game cannot return its runner-side deadline report.
- Resource path containment does not resolve symlinks. Custom Resource
  constructors/setters can run during validation and dry runs; rollback remains
  best-effort within the documented per-domain limits.
- Editor/runtime screenshots require graphical rendering. Host binaries remain
  unsigned and unnotarized.

## v0.1.0 — 2026-10-28

- Initial addon-first release with a bundled MCP host, editor panel, OpenCode /
  Claude Code / Codex onboarding, scene/node/resource editing, domain batches,
  validation, and standalone runtime lifecycle/capture support.
