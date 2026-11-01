package runtime

import (
	"time"

	"github.com/davidherring123/godot-mcp/internal/protocol/apierr"
)

const (
	basisIdle    = "idle"
	basisPhysics = "physics"
)

type waitRequest struct {
	Op              string  `json:"op"`
	Basis           string  `json:"basis"`
	Frames          int     `json:"frames,omitempty"`
	Seconds         float64 `json:"seconds,omitempty"`
	ExpiresAtUnixMS int64   `json:"expires_at_unix_ms,omitempty"`
}

type waitResponse struct {
	OK            bool   `json:"ok"`
	Code          string `json:"code"`
	Message       string `json:"message"`
	Basis         string `json:"basis"`
	StartFrame    int    `json:"start_frame"`
	EndFrame      int    `json:"end_frame"`
	FramesElapsed int    `json:"frames_elapsed"`
	ElapsedMS     int64  `json:"elapsed_ms"`
	UptimeMS      int64  `json:"uptime_ms"`
}

// Wait advances a running session by a bounded number of idle or physics
// frames, or by elapsed seconds. The runner performs the wait on its own frame
// loop and writes the result through the session file channel.
func Wait(projectRoot string, params WaitParams) (WaitResult, error) {
	basis, err := normalizeWaitBasis(params.Basis)
	if err != nil {
		return WaitResult{}, err
	}

	if err := validateWaitBounds(params.Frames, params.Seconds); err != nil {
		return WaitResult{}, err
	}

	timeout, err := resolveWaitTimeout(
		params.TimeoutSeconds,
		waitTimeoutBase,
		params.Frames,
		params.Seconds,
	)
	if err != nil {
		return WaitResult{}, err
	}

	info, err := requireRunningSession(projectRoot, params.Session)
	if err != nil {
		return WaitResult{}, err
	}

	if err := requireRunnerProtocol(info, "deadline-aware waits"); err != nil {
		return WaitResult{}, err
	}

	// Fix the runner's expiry at submission. The runner gives up before the
	// host timeout, so an abandoned wait frees the queue instead of blocking
	// later requests until the original wait would have finished.
	deadline := newRunnerDeadline(time.Now(), timeout)

	identifier, err := submitRunnerRequest(info, waitRequest{
		Op:              "wait",
		Basis:           basis,
		Frames:          params.Frames,
		Seconds:         params.Seconds,
		ExpiresAtUnixMS: deadline.expiresAtUnixMS(),
	}, "wait")
	if err != nil {
		return WaitResult{}, err
	}

	response, err := awaitRunnerDeadline[waitResponse](
		deadline,
		info,
		identifier,
		"wait",
	)
	if err != nil {
		return WaitResult{}, err
	}

	if !response.OK {
		return WaitResult{}, waitError(response)
	}

	resultBasis := response.Basis
	if resultBasis == "" {
		resultBasis = basis
	}

	return WaitResult{
		Session:          info.metadata.Session,
		Basis:            resultBasis,
		Frames:           response.EndFrame,
		StartFrames:      response.StartFrame,
		FramesElapsed:    response.FramesElapsed,
		RequestedFrames:  params.Frames,
		RequestedSeconds: params.Seconds,
		ElapsedMS:        response.ElapsedMS,
		UptimeMS:         response.UptimeMS,
	}, nil
}

func waitError(response waitResponse) error {
	code := response.Code

	if code == "" {
		code = string(apierr.CodeActionFailed)
	}

	message := response.Message

	if message == "" {
		message = "the runtime could not complete the wait"
	}

	return newError(code, "%s", message)
}

func normalizeWaitBasis(basis string) (string, error) {
	switch basis {
	case "", basisIdle:
		return basisIdle, nil
	case basisPhysics:
		return basisPhysics, nil
	default:
		return "", newError(
			string(apierr.CodeInvalidArgument),
			`basis must be "idle" or "physics", got %q`,
			basis,
		)
	}
}

func validateWaitBounds(frames int, seconds float64) error {
	if err := validateWaitRanges(frames, seconds); err != nil {
		return err
	}

	if frames == 0 && seconds == 0 {
		return newError(
			string(apierr.CodeInvalidArgument),
			"specify frames, seconds, or both",
		)
	}

	return nil
}

func validateWaitRanges(frames int, seconds float64) error {
	switch {
	case frames < 0 || frames > maxWaitFrames:
		return newError(
			string(apierr.CodeInvalidArgument),
			"frames must be between 0 and %d",
			maxWaitFrames,
		)
	case seconds < 0 || seconds > maxWaitSeconds:
		return newError(
			string(apierr.CodeInvalidArgument),
			"seconds must be between 0 and %.0f",
			maxWaitSeconds,
		)
	}

	return nil
}

func resolveWaitTimeout(
	explicit float64,
	base time.Duration,
	frames int,
	seconds float64,
) (time.Duration, error) {
	if explicit < 0 || explicit > maxWaitTimeout.Seconds() {
		return 0, newError(
			string(apierr.CodeInvalidArgument),
			"timeout_seconds must be between 0 and %.0f; 0 selects the default",
			maxWaitTimeout.Seconds(),
		)
	}

	if explicit > 0 {
		return time.Duration(explicit * float64(time.Second)), nil
	}

	frameWait := time.Duration(float64(frames) / waitFrameAllowance * float64(time.Second))
	timeout := base + time.Duration(seconds*float64(time.Second)) + frameWait

	if timeout > maxWaitTimeout {
		return maxWaitTimeout, nil
	}

	return timeout, nil
}
