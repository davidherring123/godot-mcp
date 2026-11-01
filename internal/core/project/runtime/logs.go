package runtime

import (
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/davidherring123/godot-mcp/internal/protocol/apierr"
)

const (
	logSourceBoth   = "both"
	logSourceStdout = "stdout"
	logSourceStderr = "stderr"
)

// Logs returns a bounded, cursor-based slice of a session's captured stdout or
// stderr. It reads the logs directly so it also works after the runtime exits.
func Logs(projectRoot string, params LogsParams) (LogsResult, error) {
	source, err := normalizeLogSource(params.Source)
	if err != nil {
		return LogsResult{}, err
	}

	maxBytes, err := normalizeLogBytes(params.MaxBytes)
	if err != nil {
		return LogsResult{}, err
	}

	if params.StdoutCursor < 0 || params.StderrCursor < 0 {
		return LogsResult{}, newError(
			string(apierr.CodeInvalidArgument),
			"stdout_cursor and stderr_cursor must be 0 or greater",
		)
	}

	info, err := logsSession(projectRoot, params.Session)
	if err != nil {
		return LogsResult{}, err
	}

	if info == nil {
		return LogsResult{}, newError(
			string(apierr.CodeFailedPrecondition),
			"no runtime session is available for this project; call runtime.start first",
		)
	}

	result := LogsResult{
		Session: info.metadata.Session,
		Source:  source,
	}

	if source == logSourceStdout || source == logSourceBoth {
		stdout, err := readLogStream(
			projectRoot,
			info.directory,
			logSourceStdout,
			params.StdoutCursor,
			maxBytes,
		)
		if err != nil {
			return LogsResult{}, err
		}

		result.Stdout = stdout
	}

	if source == logSourceStderr || source == logSourceBoth {
		stderr, err := readLogStream(
			projectRoot,
			info.directory,
			logSourceStderr,
			params.StderrCursor,
			maxBytes,
		)
		if err != nil {
			return LogsResult{}, err
		}

		result.Stderr = stderr
	}

	return result, nil
}

func logsSession(projectRoot string, session string) (*sessionInfo, error) {
	if session != "" {
		return inspectSession(projectRoot, session)
	}

	info, err := activeSession(projectRoot)
	if err != nil {
		return nil, err
	}

	if info != nil {
		return info, nil
	}

	return latestSession(projectRoot)
}

func normalizeLogSource(source string) (string, error) {
	switch source {
	case "", logSourceBoth:
		return logSourceBoth, nil
	case logSourceStdout:
		return logSourceStdout, nil
	case logSourceStderr:
		return logSourceStderr, nil
	default:
		return "", newError(
			string(apierr.CodeInvalidArgument),
			`source must be "stdout", "stderr", or "both", got %q`,
			source,
		)
	}
}

func normalizeLogBytes(value int) (int, error) {
	switch {
	case value < 0 || value > maxLogBytes || (value > 0 && value < minLogBytes):
		return 0, newError(
			string(apierr.CodeInvalidArgument),
			"max_bytes must be between %d and %d, or 0 for the default",
			minLogBytes,
			maxLogBytes,
		)
	case value == 0:
		return defaultLogBytes, nil
	default:
		return value, nil
	}
}

func readLogStream(
	projectRoot string,
	directory string,
	source string,
	cursor int64,
	maxBytes int,
) (*LogStream, error) {
	path := filepath.Join(directory, source+".log")
	displayPath, err := resPath(projectRoot, path)
	if err != nil {
		return nil, newError(
			string(apierr.CodeInternalError),
			"resolving %s log path: %v",
			source,
			err,
		)
	}

	stream := &LogStream{Path: displayPath}

	stat, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return stream, nil
		}

		return nil, newError(
			string(apierr.CodeInternalError),
			"reading %s log metadata: %v",
			source,
			err,
		)
	}

	stream.TotalBytes = stat.Size()

	if cursor > stream.TotalBytes {
		cursor = stream.TotalBytes
	}

	stream.Cursor = cursor

	remaining := stream.TotalBytes - cursor
	if remaining <= 0 {
		stream.NextCursor = cursor

		return stream, nil
	}

	length := int64(maxBytes)
	if remaining < length {
		length = remaining
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, newError(
			string(apierr.CodeInternalError),
			"opening %s log: %v",
			source,
			err,
		)
	}
	defer func() { _ = file.Close() }()

	if _, err := file.Seek(cursor, io.SeekStart); err != nil {
		return nil, newError(
			string(apierr.CodeInternalError),
			"seeking %s log: %v",
			source,
			err,
		)
	}

	data := make([]byte, length)
	read, err := io.ReadFull(file, data)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, newError(
			string(apierr.CodeInternalError),
			"reading %s log: %v",
			source,
			err,
		)
	}

	data = data[:read]

	// An explicit cursor may land inside a multi-byte rune, so skip the
	// orphaned continuation bytes instead of returning invalid text.
	start := 0
	for start < len(data) && !utf8.RuneStart(data[start]) {
		start++
	}

	// Hold back a rune that is cut off by the read window or by the writer's
	// current end of file. NextCursor stops before it, so the next read decodes
	// the whole rune once the remaining bytes are appended.
	body := trimIncompleteUTF8Suffix(data[start:])

	stream.NextCursor = cursor + int64(start+len(body))
	stream.Truncated = stream.NextCursor < stream.TotalBytes
	stream.Text = string(body)

	return stream, nil
}

// trimIncompleteUTF8Suffix drops a trailing multi-byte sequence that is a
// valid but incomplete UTF-8 prefix. Malformed bytes are kept rather than
// trimmed away, so an invalid byte inside the window never discards the valid
// text after it; only a rune cut off at the very end is deferred. utf8.FullRune
// reports malformed prefixes (overlong, surrogate, or out-of-range lead bytes)
// as full runes, so impossible encodings are emitted instead of stalling the
// cursor forever.
func trimIncompleteUTF8Suffix(data []byte) []byte {
	limit := len(data) - utf8.UTFMax
	if limit < 0 {
		limit = 0
	}

	for i := len(data) - 1; i >= limit; i-- {
		if data[i] < utf8.RuneSelf {
			// ASCII ends a valid sequence; bytes after it are malformed.
			return data
		}

		if utf8.RuneStart(data[i]) {
			if utf8.FullRune(data[i:]) {
				return data
			}

			return data[:i]
		}
	}

	// No lead byte within the last few bytes: a malformed tail, not a cut rune.
	return data
}
