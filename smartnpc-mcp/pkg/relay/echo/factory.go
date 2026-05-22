package echo

import (
	"log/slog"

	"gopkg.in/yaml.v3"

	"github.com/OmniStormX/SmartNPC/pkg/agentbridge"
)

// Register attaches the echo backend factory to the global agentbridge
// relay registry under kind "echo". Idempotent at the package boundary
// because Go calls init() exactly once per package.
//
// The echo backend has no configuration; the yaml `config:` subtree is
// ignored if present. Empty configs are normal for echo.
func init() {
	agentbridge.RegisterRelay("echo", func(_ yaml.Node, logger *slog.Logger) (agentbridge.Backend, error) {
		return &Backend{Logger: logger}, nil
	})
}
