package config

import (
	"flag"
	"path/filepath"
	"testing"
)

func TestLoadCommandLineBuildsRuntimeConfiguration(t *testing.T) {
	oldFlags := flag.CommandLine
	t.Cleanup(func() { flag.CommandLine = oldFlags })
	configDir := t.TempDir()
	cfg, err := Load([]string{"--config", configDir, "--port", "64591", "--address", "127.0.0.1", "--debug", "--i2p", "tor", "status"})
	if err != nil {
		t.Fatalf("Load(args) error = %v", err)
	}
	if cfg.Runtime.ConfigPath != filepath.Join(configDir, "server.yml") || cfg.Runtime.Port != "64591" || cfg.Runtime.Address != "127.0.0.1" {
		t.Fatalf("runtime settings = %+v", cfg.Runtime)
	}
	if !cfg.Runtime.Debug || !cfg.Runtime.DebugSpecified || !cfg.Runtime.I2P || !cfg.Runtime.I2PSpecified {
		t.Fatalf("boolean runtime settings = %+v", cfg.Runtime)
	}
	if len(cfg.Runtime.Args) != 2 || cfg.Runtime.Args[0] != "tor" || cfg.Runtime.Args[1] != "status" {
		t.Fatalf("positional arguments = %#v", cfg.Runtime.Args)
	}
}
