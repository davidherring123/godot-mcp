package runtime

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/davidherring123/godot-mcp/internal/protocol/apierr"
)

const (
	runnerPoll = 25 * time.Millisecond

	// runnerExpiryMargin keeps the runner's own deadline ahead of the host's
	// timeout. A runner failure, including how many input events it delivered,
	// reaches the host instead of racing the host's give-up timer. Because the
	// expiry is fixed at submission, time spent queued behind another request
	// counts against it too.
	runnerExpiryMargin = 400 * time.Millisecond
)

// runnerDeadline is the shared submission deadline for wait, capture, and input
// requests. The runner must stop before expiresAt; the host waits no later than
// hostDeadline. expiresAt is always strictly earlier so the runner can report a
// failure and free the session queue before the caller gives up.
type runnerDeadline struct {
	submission   time.Time
	hostDeadline time.Time
	expiresAt    time.Time
}

// newRunnerDeadline fixes both deadlines at submission. Short host timeouts
// still grant the runner half the budget so it can inspect and reject a queued
// request instead of doing no work at all.
func newRunnerDeadline(now time.Time, timeout time.Duration) runnerDeadline {
	budget := timeout - runnerExpiryMargin

	if budget <= 0 || budget*2 < timeout {
		budget = timeout / 2
	}

	if budget <= 0 {
		budget = time.Millisecond
	}

	return runnerDeadline{
		submission:   now,
		hostDeadline: now.Add(timeout),
		expiresAt:    now.Add(budget),
	}
}

// expiresAtUnixMS is the absolute expiry sent to the runner. The runner rejects
// a request that reaches it after this instant, so queue time is not free.
func (deadline runnerDeadline) expiresAtUnixMS() int64 {
	return deadline.expiresAt.UnixMilli()
}

// activeBudgetMS is the runner's own budget measured from submission. The
// runner clamps it again to the remaining absolute expiry when it dequeues.
func (deadline runnerDeadline) activeBudgetMS() int64 {
	return deadline.expiresAt.Sub(deadline.submission).Milliseconds()
}

// newRunnerRequestID returns one identifier used for both a request and its
// response file.
func newRunnerRequestID() string {
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), randomHex(3))
}

// submitRunnerRequest writes one request into the session's file channel and
// returns the identifier used for its response file. The runtime runner polls
// requests on its own frame loop, so this does not depend on editor bridge
// timing.
func submitRunnerRequest(info *sessionInfo, request any, description string) (string, error) {
	return submitRunnerRequestWithID(
		info,
		newRunnerRequestID(),
		request,
		description,
	)
}

// submitRunnerRequestWithID writes a request whose identifier the caller already
// used, such as a capture path that embeds the identifier.
func submitRunnerRequestWithID(
	info *sessionInfo,
	identifier string,
	request any,
	description string,
) (string, error) {
	requestPath := filepath.Join(info.directory, "requests", identifier+".json")

	if err := writeJSONAtomic(requestPath, request); err != nil {
		return "", newError(
			string(apierr.CodeInternalError),
			"writing %s request: %v",
			description,
			err,
		)
	}

	return identifier, nil
}

// requireRunnerProtocol rejects request features that a session started by an
// older embedded runner cannot honor. The session metadata is written by
// runtime.start, so an absent version means a runner from an older host.
func requireRunnerProtocol(info *sessionInfo, feature string) error {
	if info.metadata.RunnerProtocol >= RunnerProtocolVersion {
		return nil
	}

	return newError(
		string(apierr.CodeFailedPrecondition),
		"runtime session %s was started by an older godot-mcp runner without %s support; stop it with runtime.stop and call runtime.start again to restart the runtime",
		info.metadata.Session,
		feature,
	)
}

// awaitRunnerDeadline waits for the runner's response without outliving the
// host deadline fixed at submission. The wait is intentionally not cancellable:
// tool calls are serialized, so the runner's own expiry is what frees the
// session queue for the next request or runtime.stop.
func awaitRunnerDeadline[T any](
	deadline runnerDeadline,
	info *sessionInfo,
	identifier string,
	description string,
) (T, error) {
	remaining := time.Until(deadline.hostDeadline)

	if remaining < 0 {
		remaining = 0
	}

	return awaitRunnerResponse[T](info, identifier, remaining, description)
}

// awaitRunnerResponse polls for the runner's response to one request while
// respecting the process lifetime and a bounded host-side timeout.
func awaitRunnerResponse[T any](
	info *sessionInfo,
	identifier string,
	timeout time.Duration,
	description string,
) (T, error) {
	var zero T

	responsePath := filepath.Join(info.directory, "responses", identifier+".json")
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	ticker := time.NewTicker(runnerPoll)
	defer ticker.Stop()

	var lastReadError error

	for {
		var response T

		found, err := readJSON(responsePath, &response)
		if err != nil {
			// Responses are renamed into place. On Windows a reader can briefly
			// race a rename or a scanner holding the new file, so retry instead
			// of failing the action on a transient sharing violation.
			lastReadError = err
		} else {
			lastReadError = nil
		}

		if found {
			return response, nil
		}

		if !sameProcess(info.metadata.PID, info.metadata.ProcessIdentity) {
			return zero, newError(
				string(apierr.CodeFailedPrecondition),
				"runtime session %s exited before the %s finished; call runtime.status for errors",
				info.metadata.Session,
				description,
			)
		}

		select {
		case <-deadline.C:
			if lastReadError != nil {
				return zero, newError(
					string(apierr.CodeActionFailed),
					"timed out after %s waiting for the %s to finish in session %s; the response could not be read: %v",
					timeout,
					description,
					info.metadata.Session,
					lastReadError,
				)
			}

			return zero, newError(
				string(apierr.CodeActionFailed),
				"timed out after %s waiting for the %s to finish in session %s; the runtime may be stalled",
				timeout,
				description,
				info.metadata.Session,
			)
		case <-ticker.C:
		}
	}
}
