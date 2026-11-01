package runtime_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	gdruntime "github.com/davidherring123/godot-mcp/internal/core/project/runtime"
	"golang.org/x/sys/windows"
)

func TestRunnerRetriesTemporarilyLockedRequest(t *testing.T) {
	root := newInputProject(t)
	registerEditor(t, root, requireGodot(t))
	start := startHeadlessRuntime(t, root)
	t.Cleanup(func() {
		_, _ = gdruntime.Stop(root, gdruntime.StopParams{Session: start.Session, Force: true})
	})
	directory := filepath.Join(root, ".godot", "godot-mcp", "runtime", "sessions", start.Session)
	request := filepath.Join(directory, "requests", "locked.json")
	name, err := windows.UTF16PtrFromString(request)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, nil, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(handle), request)
	t.Cleanup(func() { _ = file.Close() })
	if _, err := file.WriteString(`{"op":"wait","frames":1,"basis":"idle"}`); err != nil {
		t.Fatal(err)
	}
	response := filepath.Join(directory, "responses", "locked.json")
	// Let multiple runner frames encounter a real Windows sharing violation.
	<-time.After(200 * time.Millisecond)
	if _, err := os.Stat(response); !os.IsNotExist(err) {
		t.Fatalf("unreadable request produced a response instead of retrying: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(response)
		if err == nil {
			var result struct {
				OK bool `json:"ok"`
			}
			if err := json.Unmarshal(data, &result); err != nil || !result.OK {
				t.Fatalf("retried request failed: %s (%v)", data, err)
			}
			return
		}
		<-time.After(20 * time.Millisecond)
	}
	t.Fatal("runner did not finish request after the file was unlocked")
}
