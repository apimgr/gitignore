package app

import (
	"context"
	"net"

	"github.com/apimgr/gitignore/src/config"
)

func run(ctx context.Context, cfg config.ServerConfig, listener net.Listener, ready chan<- struct{}) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if exitErr, ok := recovered.(ExitError); ok {
				err = exitErr
				return
			}
			panic(recovered)
		}
	}()
	Version = cfg.Build.Version
	CommitID = cfg.Build.CommitID
	BuildEpoch = cfg.Build.BuildEpoch
	OfficialSite = cfg.Build.OfficialSite
	if epoch := buildEpoch(); epoch > 0 {
		BuildDate = buildDate(epoch)
	}
	if listener == nil {
		return runApplication(ctx, &cfg, nil, nil)
	}
	return runApplication(ctx, &cfg, listener, ready)
}
