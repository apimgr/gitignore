package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/apimgr/gitignore/src/common/logging"
	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/mode"
)

var activeApplicationLogs atomic.Pointer[logging.Manager]

func openApplicationLogs(directory string, cfg config.LoggingConfig, appMode mode.Mode, debug bool) (*logging.Manager, error) {
	settings, err := logging.Resolve(cfg, string(appMode), debug)
	if err != nil {
		return nil, err
	}
	manager, err := logging.Open(directory, settings)
	if err != nil {
		return nil, err
	}
	activeApplicationLogs.Store(manager)
	log.SetFlags(0)
	log.SetOutput(io.MultiWriter(applicationConsoleWriter{}, applicationLogWriter{manager: manager}))
	level := slog.LevelInfo
	switch settings.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	serverHandler := makeSlogFileHandler(settings.Kinds["server"], io.MultiWriter(applicationConsoleWriter{}, manager.Writer("server")), level)
	errorHandler := makeSlogFileHandler(settings.Kinds["error"], io.MultiWriter(applicationConsoleWriter{}, manager.Writer("error")), level)
	debugHandler := makeSlogFileHandler(settings.Kinds["debug"], manager.Writer("debug"), slog.LevelDebug)
	slog.SetDefault(slog.New(&applicationSlogHandler{server: serverHandler, errors: errorHandler, debug: debugHandler, level: level}))
	return manager, nil
}

func makeSlogFileHandler(kind logging.Kind, output io.Writer, level slog.Level) slog.Handler {
	options := &slog.HandlerOptions{Level: level}
	if kind.Format == "json" {
		return slog.NewJSONHandler(output, options)
	}
	return slog.NewTextHandler(output, options)
}

type applicationSlogHandler struct {
	server slog.Handler
	errors slog.Handler
	debug  slog.Handler
	level  slog.Level
}

func (h *applicationSlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *applicationSlogHandler) Handle(ctx context.Context, record slog.Record) error {
	var result error
	if h.server != nil && h.server.Enabled(ctx, record.Level) {
		result = errors.Join(result, h.server.Handle(ctx, record))
	}
	if record.Level >= slog.LevelError && h.errors != nil && h.errors.Enabled(ctx, record.Level) {
		result = errors.Join(result, h.errors.Handle(ctx, record))
	}
	if record.Level <= slog.LevelDebug && h.debug != nil && h.debug.Enabled(ctx, record.Level) {
		result = errors.Join(result, h.debug.Handle(ctx, record))
	}
	return result
}

func (h *applicationSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &applicationSlogHandler{server: h.server.WithAttrs(attrs), errors: h.errors.WithAttrs(attrs), debug: h.debug.WithAttrs(attrs), level: h.level}
}

func (h *applicationSlogHandler) WithGroup(name string) slog.Handler {
	return &applicationSlogHandler{server: h.server.WithGroup(name), errors: h.errors.WithGroup(name), debug: h.debug.WithGroup(name), level: h.level}
}

type applicationLogWriter struct {
	manager *logging.Manager
}

type applicationConsoleWriter struct{}

func (applicationConsoleWriter) Write(line []byte) (int, error) {
	message := strings.ToLower(string(line))
	if strings.Contains(message, "warn") || strings.Contains(message, "error") || strings.Contains(message, "fatal") || strings.Contains(message, "failed") || strings.Contains(message, "failure") || strings.Contains(message, "shutdown") {
		return os.Stderr.Write(line)
	}
	return len(line), nil
}

func (w applicationLogWriter) Write(line []byte) (int, error) {
	if w.manager == nil {
		return len(line), nil
	}
	message := strings.TrimSpace(string(line))
	level := "info"
	lower := strings.ToLower(message)
	if strings.Contains(lower, "error") || strings.Contains(lower, "failed") || strings.Contains(lower, "failure") {
		level = "error"
	} else if strings.Contains(lower, "warn") {
		level = "warn"
	}
	event := logging.Entry{Time: time.Now(), Level: level, Message: message}
	if err := w.manager.Write("server", event); err != nil {
		return len(line), fmt.Errorf("write server log: %w", err)
	}
	if level == "error" {
		if err := w.manager.Write("error", event); err != nil {
			return len(line), fmt.Errorf("write error log: %w", err)
		}
	}
	return len(line), nil
}

func rotateApplicationLogs(now time.Time) error {
	if manager := activeApplicationLogs.Load(); manager != nil {
		return manager.Rotate(now)
	}
	return nil
}

func reopenApplicationLogs() error {
	if manager := activeApplicationLogs.Load(); manager != nil {
		return manager.Reopen()
	}
	return nil
}

func closeApplicationLogs() error {
	manager := activeApplicationLogs.Swap(nil)
	if manager == nil {
		return nil
	}
	return manager.Close()
}

func logRotationTask(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return rotateApplicationLogs(time.Now().UTC())
}
