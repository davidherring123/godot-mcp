package runtime_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	gdruntime "github.com/davidherring123/godot-mcp/internal/core/project/runtime"
)

func TestLogsReadsBoundedCursorSlices(t *testing.T) {
	root := t.TempDir()
	session := "20261023T150405Z-10a5b1"
	directory := writeRuntimeFixture(t, root, session, runtimeFixture{
		Session: session,
		Scene:   "res://main.tscn",
	}, &runtimeExitFixture{
		Reason: "stop",
		Code:   0,
	})

	stdout := "alpha\nbeta\ngamma\n"
	stderr := "warning one\nwarning two\n"
	writeLogFixture(t, directory, "stdout", stdout)
	writeLogFixture(t, directory, "stderr", stderr)

	first, err := gdruntime.Logs(root, gdruntime.LogsParams{
		Session:  session,
		Source:   "both",
		MaxBytes: 4,
	})
	if err != nil {
		t.Fatalf("reading logs: %v", err)
	}

	if first.Session != session || first.Source != "both" {
		t.Fatalf("logs = %+v; want the requested session and source", first)
	}

	assertLogSlice(t, first.Stdout, 0, 4, int64(len(stdout)), true)
	assertLogSlice(t, first.Stderr, 0, 4, int64(len(stderr)), true)

	stdoutText := first.Stdout.Text
	stderrText := first.Stderr.Text
	stdoutCursor := first.Stdout.NextCursor
	stderrCursor := first.Stderr.NextCursor

	for attempt := 0; attempt < 50; attempt++ {
		next, err := gdruntime.Logs(root, gdruntime.LogsParams{
			Session:      session,
			Source:       "both",
			StdoutCursor: stdoutCursor,
			StderrCursor: stderrCursor,
			MaxBytes:     4,
		})
		if err != nil {
			t.Fatalf("continuing logs: %v", err)
		}

		stdoutText += next.Stdout.Text
		stderrText += next.Stderr.Text
		stdoutCursor = next.Stdout.NextCursor
		stderrCursor = next.Stderr.NextCursor

		if !next.Stdout.Truncated && !next.Stderr.Truncated {
			break
		}
	}

	if stdoutText != stdout {
		t.Fatalf("stdout = %q; want %q", stdoutText, stdout)
	}

	if stderrText != stderr {
		t.Fatalf("stderr = %q; want %q", stderrText, stderr)
	}

	if stdoutCursor != int64(len(stdout)) || stderrCursor != int64(len(stderr)) {
		t.Fatalf(
			"cursors = %d/%d; want %d/%d",
			stdoutCursor,
			stderrCursor,
			len(stdout),
			len(stderr),
		)
	}

	if !strings.HasPrefix(first.Stdout.Path, "res://.godot/godot-mcp/runtime/") {
		t.Fatalf("stdout path = %q; want a runtime res:// path", first.Stdout.Path)
	}
}

func TestLogsPreservesUTF8AtSliceBoundaries(t *testing.T) {
	root := t.TempDir()
	session := "20261023T150405Z-10a5b2"
	directory := writeRuntimeFixture(t, root, session, runtimeFixture{
		Session: session,
		Scene:   "res://main.tscn",
	}, &runtimeExitFixture{
		Reason: "stop",
		Code:   0,
	})

	content := "abcédef"
	writeLogFixture(t, directory, "stdout", content)

	var (
		text   string
		cursor int64
	)

	for attempt := 0; attempt < 10; attempt++ {
		result, err := gdruntime.Logs(root, gdruntime.LogsParams{
			Session:      session,
			Source:       "stdout",
			StdoutCursor: cursor,
			MaxBytes:     4,
		})
		if err != nil {
			t.Fatalf("reading logs: %v", err)
		}

		if result.Stdout == nil {
			t.Fatal("stdout slice is missing")
		}

		if !utf8.ValidString(result.Stdout.Text) {
			t.Fatalf("slice = %q; want valid UTF-8", result.Stdout.Text)
		}

		text += result.Stdout.Text
		cursor = result.Stdout.NextCursor

		if !result.Stdout.Truncated {
			break
		}
	}

	if text != content {
		t.Fatalf("stdout = %q; want %q", text, content)
	}
}

func TestLogsHoldsSplitRuneUntilAppended(t *testing.T) {
	root := t.TempDir()
	session := "20261023T150405Z-10a5b3"
	directory := writeRuntimeFixture(t, root, session, runtimeFixture{
		Session: session,
		Scene:   "res://main.tscn",
	}, &runtimeExitFixture{
		Reason: "stop",
		Code:   0,
	})

	// The writer stopped in the middle of "é" (0xC3 0xA9) at end of file.
	writeLogFixture(t, directory, "stdout", "caf\xC3")

	first, err := gdruntime.Logs(root, gdruntime.LogsParams{
		Session:  session,
		Source:   "stdout",
		MaxBytes: 64,
	})
	if err != nil {
		t.Fatalf("reading split logs: %v", err)
	}

	if !utf8.ValidString(first.Stdout.Text) {
		t.Fatalf("first slice = %q; want valid UTF-8", first.Stdout.Text)
	}

	if first.Stdout.Text != "caf" {
		t.Fatalf("first slice = %q; want the cut rune held back", first.Stdout.Text)
	}

	if first.Stdout.NextCursor != 3 {
		t.Fatalf("next cursor = %d; want 3 to retry the split rune", first.Stdout.NextCursor)
	}

	if !first.Stdout.Truncated {
		t.Fatal("first slice is not truncated; want the held-back rune reported")
	}

	appendLogFixture(t, directory, "stdout", "\xA9 ok\n")

	second, err := gdruntime.Logs(root, gdruntime.LogsParams{
		Session:      session,
		Source:       "stdout",
		StdoutCursor: first.Stdout.NextCursor,
		MaxBytes:     64,
	})
	if err != nil {
		t.Fatalf("reading appended logs: %v", err)
	}

	if second.Stdout.Text != "é ok\n" {
		t.Fatalf("second slice = %q; want %q", second.Stdout.Text, "é ok\n")
	}

	if second.Stdout.NextCursor != second.Stdout.TotalBytes || second.Stdout.Truncated {
		t.Fatalf("second slice = %+v; want the whole log consumed", second.Stdout)
	}
}

func TestLogsSkipsMidRuneCursor(t *testing.T) {
	root := t.TempDir()
	session := "20261023T150405Z-10a5b4"
	directory := writeRuntimeFixture(t, root, session, runtimeFixture{
		Session: session,
		Scene:   "res://main.tscn",
	}, &runtimeExitFixture{
		Reason: "stop",
		Code:   0,
	})

	// "a" is byte 0, "é" spans bytes 1-2, then "bc".
	content := "aébc"
	writeLogFixture(t, directory, "stdout", content)

	result, err := gdruntime.Logs(root, gdruntime.LogsParams{
		Session:      session,
		Source:       "stdout",
		StdoutCursor: 2,
		MaxBytes:     64,
	})
	if err != nil {
		t.Fatalf("reading logs: %v", err)
	}

	if result.Stdout.Cursor != 2 {
		t.Fatalf("cursor = %d; want the requested offset 2", result.Stdout.Cursor)
	}

	if !utf8.ValidString(result.Stdout.Text) {
		t.Fatalf("slice = %q; want valid UTF-8", result.Stdout.Text)
	}

	if result.Stdout.Text != "bc" {
		t.Fatalf("slice = %q; want %q after skipping the orphaned continuation byte", result.Stdout.Text, "bc")
	}

	if result.Stdout.NextCursor != int64(len(content)) || result.Stdout.Truncated {
		t.Fatalf("slice = %+v; want the rest of the log consumed", result.Stdout)
	}
}

func TestLogsKeepsValidTailAfterMalformedByte(t *testing.T) {
	root := t.TempDir()
	session := "20261023T150405Z-10a5b5"
	directory := writeRuntimeFixture(t, root, session, runtimeFixture{
		Session: session,
		Scene:   "res://main.tscn",
	}, &runtimeExitFixture{
		Reason: "stop",
		Code:   0,
	})

	content := "ok\xFFtail"
	writeLogFixture(t, directory, "stdout", content)

	first, err := gdruntime.Logs(root, gdruntime.LogsParams{
		Session:  session,
		Source:   "stdout",
		MaxBytes: 4,
	})
	if err != nil {
		t.Fatalf("reading logs: %v", err)
	}

	// The window ends on "t" after the invalid 0xFF. That valid byte must not be
	// trimmed away with the malformed interior byte.
	if first.Stdout.Text != "ok\xFFt" {
		t.Fatalf("first slice = %q; want the window bytes preserved", first.Stdout.Text)
	}

	if first.Stdout.NextCursor != 4 || !first.Stdout.Truncated {
		t.Fatalf("first slice = %+v; want cursor 4 and truncated", first.Stdout)
	}

	second, err := gdruntime.Logs(root, gdruntime.LogsParams{
		Session:      session,
		Source:       "stdout",
		StdoutCursor: first.Stdout.NextCursor,
		MaxBytes:     4,
	})
	if err != nil {
		t.Fatalf("continuing logs: %v", err)
	}

	if second.Stdout.Text != "ail" {
		t.Fatalf("second slice = %q; want %q", second.Stdout.Text, "ail")
	}

	if second.Stdout.NextCursor != int64(len(content)) || second.Stdout.Truncated {
		t.Fatalf("second slice = %+v; want the whole log consumed", second.Stdout)
	}
}

func TestLogsEmitsMalformedIncompleteWidthTail(t *testing.T) {
	cases := []struct {
		name string
		tail string
	}{
		// Overlong lead: 0xE0 requires a 0xA0-0xBF second byte.
		{name: "overlong", tail: "\xE0\x80"},
		// Out-of-range lead: 0xF4 requires a 0x80-0x8F second byte.
		{name: "outOfRange", tail: "\xF4\x90"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			session := "20261023T150405Z-10a5b6"
			directory := writeRuntimeFixture(t, root, session, runtimeFixture{
				Session: session,
				Scene:   "res://main.tscn",
			}, &runtimeExitFixture{
				Reason: "stop",
				Code:   0,
			})

			content := "ok" + tc.tail
			writeLogFixture(t, directory, "stdout", content)

			result, err := gdruntime.Logs(root, gdruntime.LogsParams{
				Session:  session,
				Source:   "stdout",
				MaxBytes: 64,
			})
			if err != nil {
				t.Fatalf("reading logs: %v", err)
			}

			// The tail can never become a valid rune, so it must be emitted
			// instead of being held back and stalling the cursor.
			if result.Stdout.Text != content {
				t.Fatalf("slice = %q; want the malformed tail emitted as %q", result.Stdout.Text, content)
			}

			if result.Stdout.NextCursor != int64(len(content)) || result.Stdout.Truncated {
				t.Fatalf("slice = %+v; want the whole log consumed", result.Stdout)
			}
		})
	}
}

func TestLogsHoldsValidTruncatedRunePrefixes(t *testing.T) {
	cases := []struct {
		name       string
		content    string
		appended   string
		first      string
		second     string
		nextCursor int64
	}{
		{
			// "€" is E2 82 AC; only the first two bytes are written.
			name:       "threeByteRune",
			content:    "ok\xE2\x82",
			appended:   "\xAC done",
			first:      "ok",
			second:     "€ done",
			nextCursor: 2,
		},
		{
			// "😀" is F0 9F 98 80; only the first three bytes are written.
			name:       "fourByteRune",
			content:    "x\xF0\x9F\x98",
			appended:   "\x80!",
			first:      "x",
			second:     "😀!",
			nextCursor: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			session := "20261023T150405Z-10a5b7"
			directory := writeRuntimeFixture(t, root, session, runtimeFixture{
				Session: session,
				Scene:   "res://main.tscn",
			}, &runtimeExitFixture{
				Reason: "stop",
				Code:   0,
			})

			writeLogFixture(t, directory, "stdout", tc.content)

			first, err := gdruntime.Logs(root, gdruntime.LogsParams{
				Session:  session,
				Source:   "stdout",
				MaxBytes: 64,
			})
			if err != nil {
				t.Fatalf("reading split logs: %v", err)
			}

			if !utf8.ValidString(first.Stdout.Text) {
				t.Fatalf("first slice = %q; want valid UTF-8", first.Stdout.Text)
			}

			if first.Stdout.Text != tc.first {
				t.Fatalf("first slice = %q; want %q", first.Stdout.Text, tc.first)
			}

			if first.Stdout.NextCursor != tc.nextCursor || !first.Stdout.Truncated {
				t.Fatalf("first slice = %+v; want cursor %d and truncated", first.Stdout, tc.nextCursor)
			}

			appendLogFixture(t, directory, "stdout", tc.appended)

			second, err := gdruntime.Logs(root, gdruntime.LogsParams{
				Session:      session,
				Source:       "stdout",
				StdoutCursor: first.Stdout.NextCursor,
				MaxBytes:     64,
			})
			if err != nil {
				t.Fatalf("reading appended logs: %v", err)
			}

			if second.Stdout.Text != tc.second {
				t.Fatalf("second slice = %q; want %q", second.Stdout.Text, tc.second)
			}

			if second.Stdout.NextCursor != second.Stdout.TotalBytes || second.Stdout.Truncated {
				t.Fatalf("second slice = %+v; want the whole log consumed", second.Stdout)
			}
		})
	}
}

func TestLogsValidatesArgumentsAndRequiresSession(t *testing.T) {
	root := newProject(t, false)

	_, err := gdruntime.Logs(root, gdruntime.LogsParams{Source: "console"})
	assertRuntimeError(t, err, "invalid_argument")
	assertErrorContains(t, err, `source must be "stdout", "stderr", or "both"`)

	_, err = gdruntime.Logs(root, gdruntime.LogsParams{MaxBytes: 1})
	assertRuntimeError(t, err, "invalid_argument")
	assertErrorContains(t, err, "max_bytes must be between 4 and 65536")

	_, err = gdruntime.Logs(root, gdruntime.LogsParams{MaxBytes: 65537})
	assertRuntimeError(t, err, "invalid_argument")
	assertErrorContains(t, err, "max_bytes must be between 4 and 65536")

	_, err = gdruntime.Logs(root, gdruntime.LogsParams{StdoutCursor: -1})
	assertRuntimeError(t, err, "invalid_argument")
	assertErrorContains(t, err, "must be 0 or greater")

	_, err = gdruntime.Logs(root, gdruntime.LogsParams{})
	assertRuntimeError(t, err, "failed_precondition")
	assertErrorContains(t, err, "no runtime session")
}

func TestHeadlessLogsCursorsExposeErrors(t *testing.T) {
	executable := requireGodot(t)

	root := newProject(t, true)
	registerEditor(t, root, executable)
	start := startHeadlessRuntime(t, root)

	t.Cleanup(func() {
		_, _ = gdruntime.Stop(root, gdruntime.StopParams{
			Session: start.Session,
			Force:   true,
		})
	})

	waitForRuntimeError(t, root, start.Session)

	var (
		stdout strings.Builder
		stderr strings.Builder
	)

	var stdoutCursor, stderrCursor int64

	for attempt := 0; attempt < 2000; attempt++ {
		result, err := gdruntime.Logs(root, gdruntime.LogsParams{
			Session:      start.Session,
			Source:       "both",
			StdoutCursor: stdoutCursor,
			StderrCursor: stderrCursor,
			MaxBytes:     8,
		})
		if err != nil {
			t.Fatalf("reading live logs: %v", err)
		}

		if result.Stdout == nil || result.Stderr == nil {
			t.Fatalf("logs = %+v; want both streams", result)
		}

		if result.Stdout.NextCursor < result.Stdout.Cursor ||
			result.Stderr.NextCursor < result.Stderr.Cursor {
			t.Fatalf("logs moved a cursor backwards: %+v", result)
		}

		stdout.WriteString(result.Stdout.Text)
		stderr.WriteString(result.Stderr.Text)
		stdoutCursor = result.Stdout.NextCursor
		stderrCursor = result.Stderr.NextCursor

		if !result.Stdout.Truncated && !result.Stderr.Truncated {
			break
		}

		if attempt == 1999 {
			t.Fatal("did not reach the end of the runtime logs")
		}
	}

	combined := stdout.String() + stderr.String()

	if !strings.Contains(combined, "godot-mcp runtime test error") {
		t.Fatalf("logs did not contain the scene error:\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
}

func writeLogFixture(t *testing.T, directory string, source string, content string) {
	t.Helper()

	if err := os.WriteFile(
		filepath.Join(directory, source+".log"),
		[]byte(content),
		0644,
	); err != nil {
		t.Fatal(err)
	}
}

func appendLogFixture(t *testing.T, directory string, source string, content string) {
	t.Helper()

	path := filepath.Join(directory, source+".log")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}

	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertLogSlice(
	t *testing.T,
	stream *gdruntime.LogStream,
	cursor int64,
	nextCursor int64,
	totalBytes int64,
	truncated bool,
) {
	t.Helper()

	if stream == nil {
		t.Fatal("log stream is missing")
	}

	if stream.Cursor != cursor ||
		stream.NextCursor != nextCursor ||
		stream.TotalBytes != totalBytes ||
		stream.Truncated != truncated {
		t.Fatalf(
			"log stream = %+v; want cursor=%d next=%d total=%d truncated=%v",
			stream,
			cursor,
			nextCursor,
			totalBytes,
			truncated,
		)
	}
}
