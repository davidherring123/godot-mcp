package editor

import (
	"github.com/davidherring123/godot-mcp/internal/protocol/artifact"
)

const CaptureName = "editor.capture"

// MaxCaptureTimeoutSeconds keeps the addon's bounded render wait below the
// bridge request timeout.
const MaxCaptureTimeoutSeconds = 8.0

// Max3DViewportIndex is the highest split-screen viewport index the editor
// exposes through EditorInterface.get_editor_viewport_3d.
const Max3DViewportIndex = 3

// CaptureParams selects an editor viewport and bounds the post-render capture.
type CaptureParams struct {
	Viewport       string  `json:"viewport,omitempty" jsonschema:"enum=2d,enum=3d,description=Editor viewport to capture: 2d for the canvas editor or 3d for the 3D editor. Defaults to 3d."`
	Index          int     `json:"index,omitempty" jsonschema:"minimum=0,maximum=3,description=3D viewport index for split-screen layouts; defaults to 0 and must be 0 for the 2D viewport. Inactive split viewports are rejected."`
	TimeoutSeconds float64 `json:"timeout_seconds,omitempty" jsonschema:"minimum=0,maximum=8,description=Maximum seconds to wait for a rendered frame; defaults to 5 and is capped at 8."`
}

// CaptureResult is a PNG image of the editor's 2D or 3D viewport. The MCP server
// attaches Image as image content when it fits the embedding limit.
type CaptureResult struct {
	Viewport   string `json:"viewport" jsonschema:"description=Captured editor viewport: 2d or 3d."`
	Index      int    `json:"index" jsonschema:"description=3D viewport index that was captured; 0 for the 2D viewport."`
	Artifact   string `json:"artifact" jsonschema:"description=Project-relative res:// path to the PNG under .godot/godot-mcp/editor/captures."`
	MIMEType   string `json:"mime_type" jsonschema:"description=Image MIME type, currently image/png."`
	Width      int    `json:"width" jsonschema:"description=Image width in pixels."`
	Height     int    `json:"height" jsonschema:"description=Image height in pixels."`
	Bytes      int64  `json:"bytes" jsonschema:"description=PNG file size in bytes."`
	CapturedAt string `json:"captured_at" jsonschema:"description=Capture time in RFC 3339 format."`
	Embedded   bool   `json:"embedded" jsonschema:"description=Whether the MCP result includes the image bytes as image content."`

	Image []byte `json:"-"`
}

// Images implements artifact.ImageSource.
func (result CaptureResult) Images() []artifact.Image {
	if len(result.Image) == 0 {
		return nil
	}

	return []artifact.Image{{MIMEType: result.MIMEType, Data: result.Image}}
}
