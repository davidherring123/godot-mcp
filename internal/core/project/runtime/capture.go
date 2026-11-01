package runtime

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/davidherring123/godot-mcp/internal/protocol/apierr"
)

type captureRequest struct {
	Op              string  `json:"op"`
	Path            string  `json:"path"`
	Basis           string  `json:"basis"`
	Frames          int     `json:"frames,omitempty"`
	Seconds         float64 `json:"seconds,omitempty"`
	ExpiresAtUnixMS int64   `json:"expires_at_unix_ms,omitempty"`
}

// Capture asks the running game to save a screenshot and returns it as a
// project-scoped artifact. The PNG bytes are attached to the MCP result when
// they fit the embedding limit. Optional frame or time waits share the runtime's
// wait path; the runner always captures after the frame post-draw barrier.
func Capture(projectRoot string, params CaptureParams) (CaptureResult, error) {
	basis, err := normalizeWaitBasis(params.Basis)
	if err != nil {
		return CaptureResult{}, err
	}

	if err := validateWaitRanges(params.WaitFrames, params.WaitSeconds); err != nil {
		return CaptureResult{}, err
	}

	timeout, err := resolveWaitTimeout(
		params.TimeoutSeconds,
		captureWaitBase,
		params.WaitFrames,
		params.WaitSeconds,
	)
	if err != nil {
		return CaptureResult{}, err
	}

	info, err := requireRunningSession(projectRoot, params.Session)
	if err != nil {
		return CaptureResult{}, err
	}

	// A plain capture is understood by older runners. Waiting before the
	// capture needs the runner's deadline protocol, because an abandoned wait
	// would otherwise hold the session's single active request open.
	if params.WaitFrames != 0 || params.WaitSeconds != 0 || basis != basisIdle {
		if err := requireRunnerProtocol(info, "capture waits"); err != nil {
			return CaptureResult{}, err
		}
	}

	if info.metadata.Headless {
		return CaptureResult{}, newError(
			string(apierr.CodeUnsupported),
			"screenshots require a rendering display, but session %s was started with headless=true; stop it and start a windowed session",
			info.metadata.Session,
		)
	}

	// The runner owns the expiry; the host stops at the same submission plus
	// the requested timeout. If the runner cannot reach a post-render frame it
	// now fails on this deadline instead of holding the queue forever.
	deadline := newRunnerDeadline(time.Now(), timeout)
	identifier := newRunnerRequestID()
	relative := filepath.ToSlash(filepath.Join(
		runtimeDirName,
		sessionsDirName,
		info.metadata.Session,
		"captures",
		"capture-"+identifier+".png",
	))
	output := "res://" + relative
	absoluteOutput := filepath.Join(projectRoot, filepath.FromSlash(relative))

	// Send res:// rather than an absolute path so Godot resolves the project
	// root consistently even when the host path contains symbolic links.
	if _, err := submitRunnerRequestWithID(
		info,
		identifier,
		captureRequest{
			Op:              "capture",
			Path:            output,
			Basis:           basis,
			Frames:          params.WaitFrames,
			Seconds:         params.WaitSeconds,
			ExpiresAtUnixMS: deadline.expiresAtUnixMS(),
		},
		"capture",
	); err != nil {
		return CaptureResult{}, err
	}

	return waitForCapture(projectRoot, info, absoluteOutput, identifier, deadline)
}

func waitForCapture(
	projectRoot string,
	info *sessionInfo,
	absoluteOutput string,
	identifier string,
	deadline runnerDeadline,
) (CaptureResult, error) {
	response, err := awaitRunnerDeadline[captureResponse](
		deadline,
		info,
		identifier,
		"screenshot",
	)
	if err != nil {
		return CaptureResult{}, err
	}

	if !response.OK {
		return CaptureResult{}, captureError(response)
	}

	return captureResult(
		projectRoot,
		info.metadata.Session,
		absoluteOutput,
		response,
	)
}

func captureError(response captureResponse) error {
	code := response.Code

	if code == "" {
		code = string(apierr.CodeActionFailed)
	}

	message := response.Message

	if message == "" {
		message = "the runtime could not capture a screenshot"
	}

	return newError(code, "%s", message)
}

func captureResult(
	projectRoot string,
	session string,
	absoluteOutput string,
	response captureResponse,
) (CaptureResult, error) {
	data, err := os.ReadFile(absoluteOutput)
	if err != nil {
		return CaptureResult{}, newError(
			string(apierr.CodeActionFailed),
			"reading captured screenshot: %v",
			err,
		)
	}

	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return CaptureResult{}, newError(
			string(apierr.CodeActionFailed),
			"Godot wrote a screenshot that is not a readable PNG: %v",
			err,
		)
	}

	artifactPath, err := resPath(projectRoot, absoluteOutput)
	if err != nil {
		return CaptureResult{}, newError(
			string(apierr.CodeInternalError),
			"resolving screenshot artifact path: %v",
			err,
		)
	}

	result := CaptureResult{
		Session:      session,
		Artifact:     artifactPath,
		MIMEType:     "image/png",
		Width:        config.Width,
		Height:       config.Height,
		Bytes:        int64(len(data)),
		Frame:        response.Frame,
		CapturedAt:   time.Now().UTC().Format(time.RFC3339),
		Embedded:     len(data) <= maxEmbeddedImage,
		WaitedFrames: response.WaitedFrames,
		WaitedMS:     response.WaitedMS,
	}

	if result.Embedded {
		result.Image = data
	}

	return result, nil
}

func requireRunningSession(projectRoot string, session string) (*sessionInfo, error) {
	var (
		info *sessionInfo
		err  error
	)

	if session != "" {
		info, err = inspectSession(projectRoot, session)
	} else {
		info, err = activeSession(projectRoot)
	}

	if err != nil {
		return nil, err
	}

	if info == nil {
		return nil, newError(
			string(apierr.CodeFailedPrecondition),
			"no runtime session is active for this project; call runtime.start first",
		)
	}

	switch info.state {
	case StateExited:
		return nil, newError(
			string(apierr.CodeFailedPrecondition),
			"runtime session %s has exited; call runtime.status for errors or runtime.start for a new session",
			info.metadata.Session,
		)
	case StateStarting:
		return nil, newError(
			string(apierr.CodeFailedPrecondition),
			"runtime session %s is still starting; retry after runtime.start returns",
			info.metadata.Session,
		)
	case StateUnresponsive:
		return nil, newError(
			string(apierr.CodeFailedPrecondition),
			"runtime session %s is alive but not responding; stop it with force=true and start a new session",
			info.metadata.Session,
		)
	}

	if !info.responsive {
		return nil, newError(
			string(apierr.CodeFailedPrecondition),
			"runtime session %s has not reported a recent heartbeat; stop it with force=true and start a new session",
			info.metadata.Session,
		)
	}

	return info, nil
}
