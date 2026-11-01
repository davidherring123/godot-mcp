// Package savereport describes the bounded before/after file comparison
// produced by a scene save. It is shared result metadata for scene and node
// actions that support the save post-effect.
//
// A report is diagnostic only. It covers the entire scene file, including
// unsaved editor edits that were not part of the requesting action, and it
// neither validates scene semantics nor repairs or reverts anything.
package savereport

type Report struct {
	Scene         string `json:"scene" jsonschema:"description=Scene resource path whose on-disk file was compared."`
	Format        string `json:"format" jsonschema:"enum=tscn,enum=scn,description=Scene file format derived from the path extension."`
	Scope         string `json:"scope" jsonschema:"enum=entire_scene_file,description=Scope of the comparison. The report always covers the entire scene file, not only the changes requested by the action."`
	Available     bool   `json:"available" jsonschema:"description=Whether both the before and after file contents could be read for comparison; false when a file was unreadable or exceeded the diagnostic input size limit."`
	Changed       bool   `json:"changed" jsonschema:"description=Whether this save changed the file on disk. Text scenes compare normalized text; binary scenes compare bytes."`
	Binary        bool   `json:"binary" jsonschema:"description=Whether the scene is a binary .scn file, for which no textual diff is produced."`
	BeforeBytes   int    `json:"before_bytes" jsonschema:"description=File size in bytes before the save; 0 when the file was absent or could not be opened."`
	AfterBytes    int    `json:"after_bytes" jsonschema:"description=File size in bytes after the save; 0 when the file was unreadable."`
	Diff          string `json:"diff,omitempty" jsonschema:"description=Bounded line-oriented diff for a .tscn file. Empty for binary scenes, when nothing changed, or when a diagnostic input limit skipped the detailed diff. May include pre-existing unsaved edits and changes not requested by the action."`
	DiffTruncated bool   `json:"diff_truncated" jsonschema:"description=Whether the diagnostic was cut off by the output line limit, line-length limit, or a diagnostic input size/line limit."`
	Note          string `json:"note" jsonschema:"description=Human-readable scope and limitation note. The report is diagnostic only and does not validate or repair the scene."`
}
