// Command godot-mcp-host serves MCP for a Godot project on behalf of the
// Godot MCP editor addon. It is bundled in release addons and is the only
// shipped Go entrypoint besides the contributor-only release packager.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/davidherring123/godot-mcp/internal/compose"
	"github.com/davidherring123/godot-mcp/internal/hostcmd"
	productversion "github.com/davidherring123/godot-mcp/internal/version"
)

// VERSION is replaced with -ldflags "-X main.VERSION=..." in release builds.
// It must stay uninitialized so the linker can stamp it; an unstamped build
// resolves the embedded canonical version at startup instead.
var VERSION string

func main() {
	compose.Install()

	version, err := productversion.Normalize(VERSION)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	root := hostcmd.NewRootCommand(version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
