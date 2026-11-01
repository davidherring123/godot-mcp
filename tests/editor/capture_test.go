package editor_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/davidherring123/godot-mcp/internal/compose"
	"github.com/davidherring123/godot-mcp/internal/core/project/editor"
	"github.com/davidherring123/godot-mcp/internal/port/invocation"
	"github.com/davidherring123/godot-mcp/internal/protocol/apierr"
)

func TestCaptureLoadsReturnedPNGArtifact(t *testing.T) {
	root := t.TempDir()
	artifact := "res://.godot/godot-mcp/editor/captures/capture-test.png"
	payload := writeCapturePNG(t, root, artifact, 4, 3)

	installCaptureTransport(t, root, artifact)

	result, err := editor.Capture(root, editor.CaptureParams{Viewport: "3d"})
	if err != nil {
		t.Fatalf("capturing fake editor artifact: %v", err)
	}

	if result.Viewport != "3d" || result.Index != 0 || result.Artifact != artifact {
		t.Fatalf("capture metadata = %+v; want 3d/0 at %s", result, artifact)
	}

	if result.MIMEType != "image/png" || result.Width != 4 || result.Height != 3 {
		t.Fatalf("capture image metadata = %+v; want 4x3 image/png", result)
	}

	if result.Bytes != int64(len(payload)) {
		t.Fatalf("capture bytes = %d; want %d", result.Bytes, len(payload))
	}

	if !result.Embedded {
		t.Fatal("small fake capture was not embedded")
	}

	images := result.Images()
	if len(images) != 1 || images[0].MIMEType != "image/png" || !bytes.Equal(images[0].Data, payload) {
		t.Fatalf("capture images = %+v; want the returned PNG payload", images)
	}

	if _, err := time.Parse(time.RFC3339, result.CapturedAt); err != nil {
		t.Fatalf("capture timestamp %q is not RFC 3339: %v", result.CapturedAt, err)
	}
}

func TestCaptureRejectsUnsafeReturnedArtifactPaths(t *testing.T) {
	cases := []string{
		"res://.godot/godot-mcp/editor/captures/../../escape.png",
		`res://.godot/godot-mcp/editor/captures/..\..\escape.png`,
		`res://.godot/godot-mcp/editor/captures/nested\escape.png`,
		"res://elsewhere/capture.png",
	}

	for _, artifact := range cases {
		t.Run(artifact, func(t *testing.T) {
			root := t.TempDir()
			installCaptureTransport(t, root, artifact)

			_, err := editor.Capture(root, editor.CaptureParams{Viewport: "3d"})
			var apiError *apierr.Error

			if !errors.As(err, &apiError) || apiError.Code != apierr.CodeInternalError {
				t.Fatalf("capture error = %v; want internal_error for artifact %q", err, artifact)
			}
		})
	}
}

func installCaptureTransport(t *testing.T, root string, artifact string) {
	t.Helper()
	t.Cleanup(compose.Install)

	invocation.SetTransport(invocation.TransportFunc(func(
		projectRoot string,
		action string,
		_ any,
	) (json.RawMessage, error) {
		if projectRoot != root {
			return nil, fmt.Errorf("unexpected project root %q", projectRoot)
		}

		if action != editor.CaptureName {
			return nil, fmt.Errorf("unexpected action %q", action)
		}

		return json.Marshal(editor.CaptureResult{
			Viewport: "3d",
			Index:    0,
			Artifact: artifact,
		})
	}))
}

func writeCapturePNG(t *testing.T, root string, artifact string, width int, height int) []byte {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(artifact[len("res://"):]))

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("creating capture directory: %v", err)
	}

	source := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			source.Set(x, y, color.RGBA{R: 25, G: 38, B: 242, A: 255})
		}
	}

	var payload bytes.Buffer
	if err := png.Encode(&payload, source); err != nil {
		t.Fatalf("encoding capture PNG: %v", err)
	}

	if err := os.WriteFile(path, payload.Bytes(), 0644); err != nil {
		t.Fatalf("writing capture PNG: %v", err)
	}

	return payload.Bytes()
}
