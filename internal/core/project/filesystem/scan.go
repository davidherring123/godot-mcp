// Package filesystem exposes the live editor's resource filesystem state.
package filesystem

import (
	"strings"

	"github.com/davidherring123/godot-mcp/internal/port/invocation"
	"github.com/davidherring123/godot-mcp/internal/protocol/apierr"
)

const ScanName = "filesystem.scan"

// MaxTimeoutSeconds keeps the addon's bounded scan wait below the bridge's
// request timeout so the caller receives the scan state instead of a transport
// timeout.
const MaxTimeoutSeconds = 8.0

// Scan asks the editor to refresh its resource filesystem and waits for the
// scan to finish within a bounded time. Requested paths are checked against the
// editor's filesystem index and their existing UIDs; missing UIDs are reported
// without creating or regenerating them.
func Scan(projectRoot string, params ScanParams) (ScanResult, error) {
	if err := validatePaths(params.Paths); err != nil {
		return ScanResult{}, err
	}

	if params.TimeoutSeconds < 0 || params.TimeoutSeconds > MaxTimeoutSeconds {
		return ScanResult{}, &apierr.Error{
			Code:    apierr.CodeInvalidArgument,
			Message: "timeout_seconds must be between 0 and 8",
		}
	}

	return invocation.Call[ScanParams, ScanResult](projectRoot, ScanName, params)
}

func validatePaths(paths []string) error {
	if len(paths) > 100 {
		return &apierr.Error{
			Code:    apierr.CodeInvalidArgument,
			Message: "At most 100 resource paths can be verified per scan",
		}
	}

	for _, path := range paths {
		if !isResourcePath(path) {
			return &apierr.Error{
				Code:    apierr.CodeInvalidArgument,
				Message: "Resource paths must be nonempty res:// paths inside the project",
			}
		}
	}

	return nil
}

func isResourcePath(path string) bool {
	if !strings.HasPrefix(path, "res://") || len(path) == len("res://") {
		return false
	}

	if strings.ContainsRune(path, '\\') {
		return false
	}

	for _, segment := range strings.Split(strings.TrimPrefix(path, "res://"), "/") {
		if segment == ".." {
			return false
		}
	}

	return true
}
