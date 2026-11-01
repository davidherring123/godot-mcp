package filesystem

// ScanParams configures one bounded editor resource-filesystem scan.
type ScanParams struct {
	Paths          []string `json:"paths,omitempty" jsonschema:"maxItems=100,description=Optional res:// resource paths to verify after the scan. A missing path or UID is reported in the result; UIDs are never created or regenerated."`
	TimeoutSeconds float64  `json:"timeout_seconds,omitempty" jsonschema:"minimum=0,maximum=8,description=Maximum seconds to wait for the scan to finish; defaults to 5 and is capped at 8 so the bridge request cannot time out first."`
}

// PathResult is the editor filesystem state for one requested resource path.
type PathResult struct {
	Path     string `json:"path" jsonschema:"description=Requested res:// resource path."`
	Found    bool   `json:"found" jsonschema:"description=Whether the editor filesystem index contains the path after the scan."`
	Type     string `json:"type,omitempty" jsonschema:"description=Editor filesystem file type reported for the path, such as PackedScene."`
	UID      string `json:"uid,omitempty" jsonschema:"description=Existing uid:// identifier for the path, when the editor already knows one."`
	UIDPath  string `json:"uid_path,omitempty" jsonschema:"description=Path currently registered for the UID, when one exists."`
	UIDValid bool   `json:"uid_valid" jsonschema:"description=Whether the existing UID resolves back to the requested path. This never creates a UID."`
}

// ScanResult reports bounded scan completion and optional path verification.
type ScanResult struct {
	Completed       bool         `json:"completed" jsonschema:"description=Whether the full scan cycle, including any follow-up scan, finished before the deadline."`
	AlreadyScanning bool         `json:"already_scanning" jsonschema:"description=Whether a scan was already running when the action started."`
	FollowUpScan    bool         `json:"follow_up_scan" jsonschema:"description=Whether a fresh scan was started after an already-running scan completed, so external edits made before the request are included."`
	Scanning        bool         `json:"scanning" jsonschema:"description=Whether the editor filesystem is still scanning after this action returned."`
	TimedOut        bool         `json:"timed_out" jsonschema:"description=Whether the bounded wait expired before the scan cycle completed."`
	DurationMS      int64        `json:"duration_ms" jsonschema:"description=Time spent waiting for the scan cycle, in milliseconds."`
	Paths           []PathResult `json:"paths" jsonschema:"description=Requested path verification results; empty when no paths were requested."`
}
