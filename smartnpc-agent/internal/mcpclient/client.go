// Package mcpclient wraps the MCP go-sdk client and a CommandTransport that
// spawns smartnpc-mcp as a subprocess.
package mcpclient

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Options configures Spawn.
type Options struct {
	// Binary is the path to the smartnpc-mcp executable.
	Binary string
	// Args are extra arguments forwarded to smartnpc-mcp.
	Args []string
	// Logger is used for client-side logging (process lifecycle, etc.).
	Logger *slog.Logger
}

// Client is a thin wrapper that owns the MCP session + the underlying process.
type Client struct {
	session *mcp.ClientSession
	logger  *slog.Logger
}

// Spawn launches smartnpc-mcp and establishes an MCP session over its stdio.
// The caller must call Close to terminate the subprocess.
func Spawn(ctx context.Context, opts Options) (*Client, error) {
	if opts.Binary == "" {
		return nil, fmt.Errorf("mcpclient: Binary is required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	cmd := exec.Command(opts.Binary, opts.Args...)
	// Surface the subprocess's stderr (where its slog output lives) so that
	// users can debug protocol issues directly.
	cmd.Stderr = os.Stderr

	transport := &mcp.CommandTransport{Command: cmd}

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "smartnpc-agent",
		Version: "0.1.0",
	}, &mcp.ClientOptions{
		Logger: opts.Logger,
	})

	opts.Logger.Info("spawning smartnpc-mcp", "binary", opts.Binary, "args", opts.Args)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect MCP server: %w", err)
	}

	init := session.InitializeResult()
	if init != nil && init.ServerInfo != nil {
		opts.Logger.Info("connected",
			"server", init.ServerInfo.Name,
			"version", init.ServerInfo.Version,
			"protocol", init.ProtocolVersion,
		)
	}

	return &Client{session: session, logger: opts.Logger}, nil
}

// Session returns the underlying MCP client session.
func (c *Client) Session() *mcp.ClientSession {
	return c.session
}

// Close terminates the MCP session and the underlying subprocess.
func (c *Client) Close() error {
	if c == nil || c.session == nil {
		return nil
	}
	return c.session.Close()
}
