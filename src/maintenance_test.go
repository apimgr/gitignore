package main

import (
	"flag"
	"path/filepath"
	"testing"

	"github.com/apimgr/gitignore/src/config"
)

func TestMaintenanceModeAndSetup(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "server.yml")
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.Port = "23456"
	cfg.Server.FQDN = "gitignore.example.test"
	cfg.Server.Mode = "development"
	if err := config.Save(); err != nil {
		t.Fatal(err)
	}

	oldFlags := flag.CommandLine
	t.Cleanup(func() { flag.CommandLine = oldFlags })
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	if err := flag.CommandLine.Parse(nil); err != nil {
		t.Fatal(err)
	}
	handleMaintenanceCommand("mode", "", t.TempDir(), t.TempDir(), configPath)

	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	if err := flag.CommandLine.Parse([]string{"prod"}); err != nil {
		t.Fatal(err)
	}
	handleMaintenanceCommand("mode", "", t.TempDir(), t.TempDir(), configPath)
	if got := config.Get().Server.Mode; got != "production" {
		t.Fatalf("maintenance mode set %q", got)
	}

	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	if err := flag.CommandLine.Parse(nil); err != nil {
		t.Fatal(err)
	}
	handleMaintenanceCommand("setup", "", t.TempDir(), t.TempDir(), configPath)
	reset := config.Get()
	if reset.Server.Port != "23456" || reset.Server.FQDN != "gitignore.example.test" || reset.Server.Mode == "development" {
		t.Fatalf("setup reset did not preserve identity and restore defaults: %#v", reset.Server)
	}
}
