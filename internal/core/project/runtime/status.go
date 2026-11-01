package runtime

import (
	"os"
	"path/filepath"
	"time"
)

// Status reports the active runtime for a project, or the most recent runtime
// when none is active.
func Status(projectRoot string, params StatusParams) (StatusResult, error) {
	var (
		info *sessionInfo
		err  error
	)

	if params.Session != "" {
		info, err = inspectSession(projectRoot, params.Session)
	} else {
		info, err = activeSession(projectRoot)

		if err == nil && info == nil {
			info, err = latestSession(projectRoot)
		}
	}

	if err != nil {
		return StatusResult{}, err
	}

	return buildStatus(projectRoot, info), nil
}

func buildStatus(projectRoot string, info *sessionInfo) StatusResult {
	result := StatusResult{
		State:      StateNone,
		Errors:     []RuntimeError{},
		ObservedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if info == nil {
		return result
	}

	metadata := info.metadata

	result.Active = info.state != StateExited
	result.State = info.state
	result.Responsive = info.responsive
	result.Session = metadata.Session
	result.PID = metadata.PID
	result.Scene = metadata.Scene
	result.Headless = metadata.Headless
	result.StartedAt = metadata.StartedAt.UTC().Format(time.RFC3339)

	if directory, err := resPath(projectRoot, info.directory); err == nil {
		result.ArtifactsDir = directory
		result.Logs = &LogPaths{
			Stdout: directory + "/stdout.log",
			Stderr: directory + "/stderr.log",
		}
	}

	if age, ok := fileAgeMS(filepath.Join(info.directory, "ready.json")); ok {
		result.ReadyAgeMS = age
	}

	if age, ok := fileAgeMS(filepath.Join(info.directory, "heartbeat.json")); ok {
		result.HeartbeatAgeMS = age
	}

	ready, hasReady := readReady(info.directory)

	if hasReady {
		if ready.Scene != "" {
			result.Scene = ready.Scene
		}

		result.Headless = ready.Headless
		result.DisplayServer = ready.DisplayServer
		result.Frames = ready.Frames

		if ready.WindowSize.Width > 0 && ready.WindowSize.Height > 0 {
			size := ready.WindowSize
			result.WindowSize = &size
		}
	}

	if result.Active || info.state == StateExited {
		heartbeat := readHeartbeat(info.directory)
		result.Frames = heartbeat.Frames
		result.UptimeMS = heartbeat.UptimeMS
	}

	if info.state == StateExited {
		report, found := readExit(info.directory)

		result.ExitReason = "process_ended"

		if found {
			result.ExitCode = intPointer(report.Code)
			result.ExitReason = report.Reason
			result.Frames = report.Frames
			result.UptimeMS = report.UptimeMS
		}
	}

	result.LastCapture = lastCapture(projectRoot, info.directory)
	result.Errors, result.ErrorCount = readErrors(info.directory)

	if hasReady && !ready.OK && ready.Message != "" {
		result.Errors = append(
			result.Errors,
			RuntimeError{Source: "runtime", Message: ready.Message, Count: 1},
		)
		result.ErrorCount++
	}

	return result
}

// fileAgeMS reports how old a runtime session file is. Missing files are
// reported as unavailable so zero can stay a meaningful age.
func fileAgeMS(path string) (int64, bool) {
	stat, err := os.Stat(path)
	if err != nil {
		return 0, false
	}

	age := time.Since(stat.ModTime())
	if age < 0 {
		age = 0
	}

	return age.Milliseconds(), true
}
