class_name SceneSaveDiagnostics
extends RefCounted

# Builds a bounded before/after comparison of the scene file a save writes.
# This is an on-disk diagnostic only: it does not parse scene semantics,
# attribute a change to a particular request, repair, or revert anything.
# The whole-file report deliberately includes unsaved editor edits, including
# changes made outside the action that requested the save.

const SCOPE := "entire_scene_file"
const MAX_INPUT_BYTES := 4 * 1024 * 1024
const MAX_DIFF_INPUT_LINES := 1000
const MAX_DIFF_LINES := 120
const MAX_DIFF_LINE_CHARS := 320
const MAX_DIFF_MATRIX_CELLS := 200000
const CONTEXT_LINES := 3

const NOTE_TEXT_CHANGED := (
    "Reports the entire scene file difference written by this save, including "
    + "unsaved editor edits and changes that were not requested by the calling "
    + "action. Diagnostic only: it does not validate scene semantics, does not "
    + "guarantee the difference is intended, and does not repair or revert the "
    + "scene."
)
const NOTE_TEXT_UNCHANGED := (
    "The scene file's bytes on disk were unchanged by this save. This compares "
    + "the file before and after the write and makes no claim about the editor "
    + "buffer. Diagnostic only: it does not validate or repair the scene."
)
const NOTE_BINARY_CHANGED := (
    "Binary scene file changed on disk; a textual diff is not available. The "
    + "report only records that the bytes differ, including any unsaved editor "
    + "edits that were not requested by the calling action. Diagnostic only: it "
    + "does not validate or repair the scene."
)
const NOTE_BINARY_UNCHANGED := (
    "Binary scene file was already identical on disk, so this save wrote no byte "
    + "change. Diagnostic only: it does not validate or repair the scene."
)
const NOTE_UNAVAILABLE := (
    "The scene file could not be read for a before/after comparison. The save "
    + "result is unchanged; no diagnostic report is available."
)

static func _input_too_large_note() -> String:
    return (
        "The scene file is larger than the %d-byte diagnostic input limit, so "
        + "no before/after comparison was attempted. The save result is "
        + "unchanged; diagnostics do not validate or repair the scene."
    ) % MAX_INPUT_BYTES

static func _diff_line_limit_note() -> String:
    return (
        "The scene text has more than %d lines, so the detailed diff was "
        + "skipped and the report only states that the file changed. This can "
        + "include unsaved editor edits that were not requested by the calling "
        + "action. Diagnostic only: it does not validate or repair the scene."
    ) % MAX_DIFF_INPUT_LINES

# snapshot records the file bytes before editor.save_scene() runs.
static func snapshot(path: String) -> Dictionary:
    var existed := FileAccess.file_exists(path)
    var read := _read_bytes(path)

    return {
        "ok": read["ok"] or not existed,
        "existed": existed,
        "too_large": read["too_large"],
        "size": read["size"],
        "bytes": read["bytes"],
    }

# build reads the file after the save and compares it with the snapshot.
static func build(path: String, before: Dictionary) -> Dictionary:
    var after := _read_bytes(path)
    var format := _format(path)
    var before_ok: bool = before.get("ok", false)
    var report := {
        "scene": path,
        "format": format,
        "scope": SCOPE,
        "available": false,
        "changed": false,
        "binary": format == "scn",
        "before_bytes": before.get("size", 0),
        "after_bytes": after.get("size", 0),
        "diff": "",
        "diff_truncated": false,
        "note": NOTE_UNAVAILABLE,
    }

    if before.get("too_large", false) or after["too_large"]:
        report["diff_truncated"] = true
        report["note"] = _input_too_large_note()

        return report

    if not before_ok or not after["ok"]:
        return report

    var before_bytes: PackedByteArray = before["bytes"]
    var after_bytes: PackedByteArray = after["bytes"]

    report["available"] = true

    if format == "scn":
        report["changed"] = before_bytes != after_bytes
        report["note"] = (
            NOTE_BINARY_CHANGED if report["changed"] else NOTE_BINARY_UNCHANGED
        )

        return report

    var before_text := _normalize_text(before_bytes)
    var after_text := _normalize_text(after_bytes)

    if before_text == after_text:
        report["note"] = NOTE_TEXT_UNCHANGED

        return report

    if (
        _line_count(before_text) > MAX_DIFF_INPUT_LINES
        or _line_count(after_text) > MAX_DIFF_INPUT_LINES
    ):
        report["changed"] = true
        report["diff_truncated"] = true
        report["note"] = _diff_line_limit_note()

        return report

    var diff := _diff(before_text, after_text)

    report["changed"] = true
    report["diff"] = diff["text"]
    report["diff_truncated"] = diff["truncated"]
    report["note"] = NOTE_TEXT_CHANGED

    return report

static func _format(path: String) -> String:
    return "scn" if path.get_extension().to_lower() == "scn" else "tscn"

static func _read_bytes(path: String) -> Dictionary:
    var file := FileAccess.open(path, FileAccess.READ)

    if file == null:
        return {
            "ok": false,
            "too_large": false,
            "size": 0,
            "bytes": PackedByteArray(),
        }

    var length := file.get_length()

    if length > MAX_INPUT_BYTES:
        file.close()

        return {
            "ok": false,
            "too_large": true,
            "size": length,
            "bytes": PackedByteArray(),
        }

    var bytes := file.get_buffer(length)

    file.close()

    return {
        "ok": true,
        "too_large": false,
        "size": length,
        "bytes": bytes,
    }

# Text scene comparison ignores line-ending style so a Windows checkout that
# only changes CRLF to LF is not reported as a content change.
static func _normalize_text(bytes: PackedByteArray) -> String:
    return bytes.get_string_from_utf8().replace("\r\n", "\n").replace("\r", "\n")

static func _lines(text: String) -> PackedStringArray:
    return text.split("\n", true)

static func _line_count(text: String) -> int:
    return text.count("\n") + 1

static func _diff(before_text: String, after_text: String) -> Dictionary:
    var before := _lines(before_text)
    var after := _lines(after_text)
    var prefix := 0

    while (
        prefix < before.size()
        and prefix < after.size()
        and before[prefix] == after[prefix]
    ):
        prefix += 1

    var suffix := 0

    while (
        suffix < before.size() - prefix
        and suffix < after.size() - prefix
        and before[before.size() - 1 - suffix] == after[after.size() - 1 - suffix]
    ):
        suffix += 1

    var before_middle := before.slice(prefix, before.size() - suffix)
    var after_middle := after.slice(prefix, after.size() - suffix)
    var state := {"truncated": false}
    var output: Array[String] = []

    _emit(output, "--- before", state)
    _emit(output, "+++ after", state)

    var context_start: int = max(0, prefix - CONTEXT_LINES)

    for index in range(context_start, prefix):
        _emit(output, " " + before[index], state)

    for line in _middle_ops(before_middle, after_middle):
        _emit(output, line, state)

    var context_end: int = min(suffix, CONTEXT_LINES)

    for index in range(context_end):
        _emit(output, " " + after[after.size() - suffix + index], state)

    return {
        "text": "\n".join(output),
        "truncated": state["truncated"],
    }

static func _middle_ops(
    before: PackedStringArray,
    after: PackedStringArray
) -> Array[String]:
    if before.size() * after.size() > MAX_DIFF_MATRIX_CELLS:
        return _replacement_ops(before, after)

    var width := after.size() + 1
    var table := PackedInt32Array()

    table.resize((before.size() + 1) * width)

    for row in range(before.size() - 1, -1, -1):
        for column in range(after.size() - 1, -1, -1):
            if before[row] == after[column]:
                table[row * width + column] = (
                    table[(row + 1) * width + column + 1] + 1
                )
            else:
                table[row * width + column] = max(
                    table[(row + 1) * width + column],
                    table[row * width + column + 1]
                )

    var ops: Array[String] = []
    var row := 0
    var column := 0

    while row < before.size() and column < after.size():
        if before[row] == after[column]:
            ops.append(" " + before[row])
            row += 1
            column += 1
        elif (
            table[(row + 1) * width + column]
            >= table[row * width + column + 1]
        ):
            ops.append("-" + before[row])
            row += 1
        else:
            ops.append("+" + after[column])
            column += 1

    while row < before.size():
        ops.append("-" + before[row])
        row += 1

    while column < after.size():
        ops.append("+" + after[column])
        column += 1

    return ops

static func _replacement_ops(
    before: PackedStringArray,
    after: PackedStringArray
) -> Array[String]:
    var ops: Array[String] = []

    for line in before:
        ops.append("-" + line)

    for line in after:
        ops.append("+" + line)

    return ops

static func _emit(
    output: Array[String],
    line: String,
    state: Dictionary
) -> void:
    if output.size() >= MAX_DIFF_LINES:
        state["truncated"] = true

        return

    if line.length() > MAX_DIFF_LINE_CHARS:
        line = line.substr(0, MAX_DIFF_LINE_CHARS) + "..."
        state["truncated"] = true

    output.append(line)
