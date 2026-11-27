package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/common/logging"
	"github.com/apimgr/gitignore/src/config"
)

func TestApplicationLogWriterRoutesMessagesAndErrors(t *testing.T) {
	directory := t.TempDir()
	settings, err := logging.Resolve(config.DefaultConfig().Server.Logging, "production", false)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := logging.Open(directory, settings)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Close() }()

	writer := applicationLogWriter{manager: manager}
	if _, err := writer.Write([]byte("server started\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("database connection failed\n")); err != nil {
		t.Fatal(err)
	}
	serverLog, err := os.ReadFile(filepath.Join(directory, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	errorLog, err := os.ReadFile(filepath.Join(directory, "error.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(serverLog), "server started") || !strings.Contains(string(serverLog), "database connection failed") {
		t.Fatalf("server log does not contain both messages: %s", serverLog)
	}
	if !strings.Contains(string(errorLog), "database connection failed") {
		t.Fatalf("error log does not contain the failure: %s", errorLog)
	}
}
