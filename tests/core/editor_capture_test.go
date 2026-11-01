package core_test

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davidherring123/godot-mcp/internal/core/project/editor"
	"github.com/davidherring123/godot-mcp/internal/core/scene"
	"github.com/davidherring123/godot-mcp/internal/protocol/apierr"
	"github.com/davidherring123/godot-mcp/tests/harness"
)

func TestEditorCaptureRejectsInvalidOptions(t *testing.T) {
	root := t.TempDir()

	cases := []struct {
		name   string
		params editor.CaptureParams
	}{
		{
			name:   "unknown viewport",
			params: editor.CaptureParams{Viewport: "top"},
		},
		{
			name:   "negative index",
			params: editor.CaptureParams{Viewport: "3d", Index: -1},
		},
		{
			name:   "2D index",
			params: editor.CaptureParams{Viewport: "2d", Index: 1},
		},
		{
			name:   "3D index above native range",
			params: editor.CaptureParams{Viewport: "3d", Index: 4},
		},
		{
			name:   "timeout above bridge limit",
			params: editor.CaptureParams{Viewport: "3d", TimeoutSeconds: 9},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := editor.Capture(root, testCase.params)
			var apiError *apierr.Error

			if !errors.As(err, &apiError) || apiError.Code != apierr.CodeInvalidArgument {
				t.Fatalf("capture error = %v; want invalid_argument", err)
			}
		})
	}
}

func TestEditorCaptureRejectsHeadlessEditor(t *testing.T) {
	if graphicalMode() {
		t.Skip("headless rejection requires the default headless test editor")
	}

	session := requireSuite(t)
	_, err := editor.Capture(session.Root(), editor.CaptureParams{
		Viewport:       "3d",
		TimeoutSeconds: 5,
	})

	var apiError *apierr.Error

	if !errors.As(err, &apiError) || apiError.Code != apierr.CodeUnsupported {
		t.Fatalf("capture error = %v; want unsupported in a headless editor", err)
	}

	if !strings.Contains(apiError.Message, "rendering display") {
		t.Fatalf("headless capture message = %q; want a rendering display explanation", apiError.Message)
	}
}

func TestEditorCaptureViewports(t *testing.T) {
	if !graphicalMode() {
		t.Skip("editor viewport capture needs GODOT_MODE=graphical")
	}

	session := requireSuite(t)
	root := session.Root()

	if _, err := scene.Open(root, scene.OpenParams{
		Scene: "res://cases/editor_capture_2d/main.tscn",
	}); err != nil {
		t.Fatalf("opening the 2D capture fixture scene: %v", err)
	}

	twoD, err := editor.Capture(root, editor.CaptureParams{
		Viewport:       "2d",
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("capturing the 2D viewport: %v", err)
	}

	twoDImage := verifyCapture(t, session, twoD, "2d", 0)

	if !isMostlyRed(twoDImage) {
		t.Fatalf("2D capture does not show the @tool-drawn red content: %s", colorText(twoDImage))
	}

	if _, err := scene.Open(root, scene.OpenParams{
		Scene: "res://cases/editor_capture_3d/main.tscn",
	}); err != nil {
		t.Fatalf("opening the 3D capture fixture scene: %v", err)
	}

	threeD, err := editor.Capture(root, editor.CaptureParams{
		Viewport:       "3d",
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("capturing the 3D viewport: %v", err)
	}

	threeDImage := verifyCapture(t, session, threeD, "3d", 0)

	if !isMostlyBlue(threeDImage) {
		t.Fatalf("3D capture does not show the @tool-colored scene: %s", colorText(threeDImage))
	}

	_, err = editor.Capture(root, editor.CaptureParams{
		Viewport:       "3d",
		Index:          1,
		TimeoutSeconds: 5,
	})
	var apiError *apierr.Error

	if !errors.As(err, &apiError) || apiError.Code != apierr.CodeFailedPrecondition {
		t.Fatalf("inactive split viewport error = %v; want failed_precondition", err)
	}

	if !strings.Contains(apiError.Message, "not active") {
		t.Fatalf("inactive split viewport message = %q; want a clear active-viewport explanation", apiError.Message)
	}

	// The rejected capture switched to the 3D screen to inspect viewport 1.
	// A normal capture must still work after that failure path restores it.
	again, err := editor.Capture(root, editor.CaptureParams{
		Viewport:       "3d",
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("capturing 3D after an inactive viewport rejection: %v", err)
	}

	verifyCapture(t, session, again, "3d", 0)
}

func verifyCapture(
	t *testing.T,
	session *harness.Session,
	result editor.CaptureResult,
	viewport string,
	index int,
) image.Image {
	t.Helper()

	if result.Viewport != viewport || result.Index != index {
		t.Fatalf("capture selected %s/%d; want %s/%d",
			result.Viewport, result.Index, viewport, index)
	}

	if result.Width <= 2 || result.Height <= 2 {
		t.Fatalf("captured a collapsed viewport: %dx%d", result.Width, result.Height)
	}

	if result.MIMEType != "image/png" || result.Bytes <= 0 || !result.Embedded {
		t.Fatalf("capture metadata = %+v; want an embedded PNG", result)
	}

	if len(result.Images()) != 1 {
		t.Fatalf("capture exposes %d images; want 1", len(result.Images()))
	}

	data, err := os.ReadFile(captureArtifactFile(t, session.Root(), result.Artifact))
	if err != nil {
		t.Fatalf("reading capture artifact: %v", err)
	}

	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding capture PNG: %v", err)
	}

	if decoded.Bounds().Dx() != result.Width || decoded.Bounds().Dy() != result.Height {
		t.Fatalf("capture reports %dx%d but the PNG is %dx%d",
			result.Width, result.Height, decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}

	if _, err := time.Parse(time.RFC3339, result.CapturedAt); err != nil {
		t.Fatalf("capture timestamp %q is not RFC 3339: %v", result.CapturedAt, err)
	}

	if err := session.LogJSON(editor.CaptureName, result); err != nil {
		t.Fatal(err)
	}

	return decoded
}

func averageColor(source image.Image) (float64, float64, float64) {
	bounds := source.Bounds()
	centerX := bounds.Min.X + bounds.Dx()/2
	centerY := bounds.Min.Y + bounds.Dy()/2
	radius := 20
	var red, green, blue, count float64

	for y := centerY - radius; y <= centerY+radius; y++ {
		for x := centerX - radius; x <= centerX+radius; x++ {
			if !image.Pt(x, y).In(bounds) {
				continue
			}

			r, g, b, _ := source.At(x, y).RGBA()
			red += float64(r >> 8)
			green += float64(g >> 8)
			blue += float64(b >> 8)
			count++
		}
	}

	return red / count, green / count, blue / count
}

func colorText(source image.Image) string {
	red, green, blue := averageColor(source)

	return fmt.Sprintf("rgb(%.1f, %.1f, %.1f)", red, green, blue)
}

func isMostlyRed(source image.Image) bool {
	red, green, blue := averageColor(source)

	return red > 150 && green < 130 && blue < 130
}

func isMostlyBlue(source image.Image) bool {
	red, green, blue := averageColor(source)

	return blue > 150 && red < 130 && green < 130
}

func graphicalMode() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("GODOT_MODE")), "graphical")
}

func captureArtifactFile(t *testing.T, root string, artifact string) string {
	t.Helper()

	prefix := "res://.godot/godot-mcp/editor/captures/"

	if !strings.HasPrefix(artifact, prefix) {
		t.Fatalf("capture artifact %q is outside %s", artifact, prefix)
	}

	absolute := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(artifact, "res://")))
	relative, err := filepath.Rel(root, absolute)

	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatalf("capture artifact %q escapes the project", artifact)
	}

	return absolute
}
