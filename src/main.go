package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/apimgr/gitignore/src/app"
	"github.com/apimgr/gitignore/src/config"
)

func main() {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		log.Printf("invalid command line or configuration: %v", err)
		os.Exit(78)
	}
	cfg.Build = config.BuildMetadata{
		Version: Version, CommitID: CommitID, BuildEpoch: BuildEpoch, OfficialSite: OfficialSite,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, *cfg); err != nil {
		log.Printf("server stopped with an error: %v", err)
		var exitErr app.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		os.Exit(69)
	}
}
