package app

import (
	"context"

	"github.com/apimgr/gitignore/src/config"
)

// Run starts the application with an already loaded server configuration.
func Run(ctx context.Context, cfg config.ServerConfig) error {
	return run(ctx, cfg, nil, nil)
}
