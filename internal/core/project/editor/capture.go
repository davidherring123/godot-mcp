package editor

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/davidherring123/godot-mcp/internal/port/invocation"
	"github.com/davidherring123/godot-mcp/internal/protocol/apierr"
)

const (
	captureDir       = ".godot/godot-mcp/editor/captures"
	maxEmbeddedImage = 16 << 20
)

// Capture asks the editor to render its 2D or 3D viewport and returns the PNG
// as a project-scoped artifact. Headless editors are rejected by the addon.
func Capture(projectRoot string, params CaptureParams) (CaptureResult, error) {
	viewport := strings.ToLower(strings.TrimSpace(params.Viewport))
	if viewport == "" {
		viewport = "3d"
	}

	if viewport != "2d" && viewport != "3d" {
		return CaptureResult{}, &apierr.Error{
			Code:    apierr.CodeInvalidArgument,
			Message: "viewport must be 2d or 3d",
		}
	}

	if params.Index < 0 {
		return CaptureResult{}, &apierr.Error{
			Code:    apierr.CodeInvalidArgument,
			Message: "index must be zero or greater",
		}
	}

	if viewport == "2d" && params.Index != 0 {
		return CaptureResult{}, &apierr.Error{
			Code:    apierr.CodeInvalidArgument,
			Message: "the 2D viewport only supports index 0",
		}
	}

	if viewport == "3d" && params.Index > Max3DViewportIndex {
		return CaptureResult{}, &apierr.Error{
			Code:    apierr.CodeInvalidArgument,
			Message: "the 3D viewport index must be between 0 and 3",
		}
	}

	if params.TimeoutSeconds < 0 || params.TimeoutSeconds > MaxCaptureTimeoutSeconds {
		return CaptureResult{}, &apierr.Error{
			Code:    apierr.CodeInvalidArgument,
			Message: "timeout_seconds must be between 0 and 8",
		}
	}

	params.Viewport = viewport

	result, err := invocation.Call[CaptureParams, CaptureResult](
		projectRoot,
		CaptureName,
		params,
	)
	if err != nil {
		return CaptureResult{}, err
	}

	return loadCapture(projectRoot, result)
}

func loadCapture(projectRoot string, result CaptureResult) (CaptureResult, error) {
	absolute, err := capturePath(projectRoot, result.Artifact)
	if err != nil {
		return CaptureResult{}, err
	}

	data, err := os.ReadFile(absolute)
	if err != nil {
		return CaptureResult{}, &apierr.Error{
			Code:    apierr.CodeActionFailed,
			Message: "reading captured editor viewport: " + err.Error(),
		}
	}

	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return CaptureResult{}, &apierr.Error{
			Code:    apierr.CodeActionFailed,
			Message: "the editor wrote a viewport image that is not a readable PNG: " + err.Error(),
		}
	}

	result.MIMEType = "image/png"
	result.Width = config.Width
	result.Height = config.Height
	result.Bytes = int64(len(data))
	result.CapturedAt = time.Now().UTC().Format(time.RFC3339)
	result.Embedded = len(data) <= maxEmbeddedImage

	if result.Embedded {
		result.Image = data
	}

	return result, nil
}

func capturePath(projectRoot string, artifact string) (string, error) {
	prefix := "res://" + captureDir + "/"

	// The addon returns forward-slash res:// paths. Backslashes are a path
	// separator on Windows and must not smuggle traversal past the segment
	// check below.
	if strings.ContainsRune(artifact, '\\') {
		return "", &apierr.Error{
			Code:    apierr.CodeInternalError,
			Message: "editor returned an invalid capture path: " + artifact,
		}
	}

	if !strings.HasPrefix(artifact, prefix) {
		return "", &apierr.Error{
			Code:    apierr.CodeInternalError,
			Message: "editor returned a capture path outside " + prefix,
		}
	}

	relative := strings.TrimPrefix(artifact, "res://")

	for _, segment := range strings.Split(relative, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", &apierr.Error{
				Code:    apierr.CodeInternalError,
				Message: "editor returned an invalid capture path: " + artifact,
			}
		}
	}

	absolute := filepath.Join(projectRoot, filepath.FromSlash(relative))

	within, err := filepath.Rel(projectRoot, absolute)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", &apierr.Error{
			Code:    apierr.CodeInternalError,
			Message: "editor returned a capture path outside the project",
		}
	}

	return absolute, nil
}
