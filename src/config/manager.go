package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultConfigWatchInterval = 5 * time.Second

// ConfigChanges identifies the settings affected by a successful reload.
// Values are omitted so change reports can be logged without disclosing secrets.
type ConfigChanges struct {
	HotReload       []string
	GracefulReload  []string
	RestartRequired []string
}

// ConfigManager polls the file-backed configuration sources and applies a
// successfully loaded configuration. Loader or apply errors leave the previous
// configuration and file snapshot active, so the same change is retried later.
type ConfigManager struct {
	configPath string
	assetsDir  string
	interval   time.Duration
	load       func() (*ServerConfig, error)
	apply      func(*ServerConfig, ConfigChanges) error
	onError    func(error)

	checkMu sync.Mutex
	mu      sync.RWMutex
	current *ServerConfig
	state   string
	pending []string

	started bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// ConfigManagerOptions supplies the configuration loader and apply callback.
// The loader must merge server.yml, yaml.d, and secrets.yml. The apply callback
// receives all changes and applies hot-reloadable values while arranging any
// graceful reload behavior required by the application. OnError is called by
// the watcher loop; its logging should omit configuration values because a
// parser or loader error may contain an operator-provided secret.
type ConfigManagerOptions struct {
	ConfigPath string
	AssetsDir  string
	Interval   time.Duration
	Initial    *ServerConfig
	Load       func() (*ServerConfig, error)
	Apply      func(*ServerConfig, ConfigChanges) error
	OnError    func(error)
}

// NewConfigManager creates a watcher for server.yml, yaml.d, secrets.yml and
// the assets directory. It snapshots existing files so subsequent add, edit
// and remove operations are detected.
func NewConfigManager(options ConfigManagerOptions) (*ConfigManager, error) {
	if strings.TrimSpace(options.ConfigPath) == "" {
		return nil, errors.New("configuration path is required")
	}
	if options.Initial == nil {
		return nil, errors.New("initial configuration is required")
	}
	if options.Load == nil {
		return nil, errors.New("configuration loader is required")
	}
	configPath, err := filepath.Abs(options.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("resolve configuration path: %w", err)
	}
	assetsDir := options.AssetsDir
	if assetsDir == "" {
		assetsDir = filepath.Join(filepath.Dir(configPath), "assets")
	} else if assetsDir, err = filepath.Abs(assetsDir); err != nil {
		return nil, fmt.Errorf("resolve assets path: %w", err)
	}
	interval := options.Interval
	if interval <= 0 {
		interval = defaultConfigWatchInterval
	}
	manager := &ConfigManager{
		configPath: configPath,
		assetsDir:  assetsDir,
		interval:   interval,
		load:       options.Load,
		apply:      options.Apply,
		onError:    options.OnError,
		current:    options.Initial,
		done:       make(chan struct{}),
	}
	manager.state, err = manager.fileState()
	if err != nil {
		return nil, fmt.Errorf("snapshot configuration files: %w", err)
	}
	return manager, nil
}

// Start begins the polling loop. Calling Start more than once has no effect.
func (m *ConfigManager) Start(parent context.Context) {
	if parent == nil {
		parent = context.Background()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	m.cancel = cancel
	m.started = true
	go m.run(ctx)
}

// Stop cancels the polling loop and waits for it to exit. It is safe to call
// repeatedly and before Start.
func (m *ConfigManager) Stop() {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return
	}
	cancel := m.cancel
	done := m.done
	m.mu.Unlock()
	cancel()
	<-done
}

func (m *ConfigManager) run(ctx context.Context) {
	defer close(m.done)
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.CheckNow(); err != nil {
				if m.onError != nil {
					m.onError(err)
				}
			}
		}
	}
}

// CheckNow checks all watched inputs once. A successful reload updates the
// active snapshot and calls apply; an error leaves both unchanged for retry.
func (m *ConfigManager) CheckNow() error {
	m.checkMu.Lock()
	defer m.checkMu.Unlock()

	state, err := m.fileState()
	if err != nil {
		return fmt.Errorf("inspect configuration files: %w", err)
	}
	m.mu.RLock()
	unchanged := state == m.state
	m.mu.RUnlock()
	if unchanged {
		return nil
	}

	next, err := m.load()
	if err != nil {
		return fmt.Errorf("reload configuration: %w", err)
	}
	if next == nil {
		return errors.New("reload configuration: loader returned nil configuration")
	}

	m.mu.RLock()
	previous := m.current
	m.mu.RUnlock()
	changes, err := compareConfigChanges(previous, next)
	if err != nil {
		return fmt.Errorf("compare configuration: %w", err)
	}
	active := retainRestartRequiredSettings(previous, next, changes.RestartRequired)
	if m.apply != nil {
		if err := m.apply(active, changes); err != nil {
			return fmt.Errorf("apply configuration: %w", err)
		}
	}

	m.mu.Lock()
	m.current = active
	m.state = state
	m.pending = mergeSorted(m.pending, changes.RestartRequired)
	m.mu.Unlock()
	return nil
}

func retainRestartRequiredSettings(previous, next *ServerConfig, paths []string) *ServerConfig {
	if len(paths) == 0 {
		return next
	}
	active := *next
	server := next.Server
	for _, path := range paths {
		switch {
		case path == "server.port":
			server.Port = previous.Server.Port
		case path == "server.address":
			server.Address = previous.Server.Address
		case path == "server.daemonize":
			server.Daemonize = previous.Server.Daemonize
		case strings.HasPrefix(path, "server.ssl."):
			server.SSL = previous.Server.SSL
		case strings.HasPrefix(path, "server.database."):
			server.Database = previous.Server.Database
		case strings.HasPrefix(path, "server.tor."):
			server.Tor = previous.Server.Tor
		case strings.HasPrefix(path, "server.i2p."):
			server.I2P = previous.Server.I2P
		}
	}
	active.Server = server
	return &active
}

// Current returns the active configuration pointer. Callers must treat it as
// immutable; subsequent reloads publish a different pointer.
func (m *ConfigManager) Current() *ServerConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// PendingRestart reports whether any accepted file edit needs a process restart.
func (m *ConfigManager) PendingRestart() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.pending) > 0
}

// RestartSettings returns a sorted copy of restart-required setting paths.
func (m *ConfigManager) RestartSettings() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]string(nil), m.pending...)
}

// ClearPendingRestart clears the accumulated restart-required settings.
func (m *ConfigManager) ClearPendingRestart() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = nil
}

func (m *ConfigManager) fileState() (string, error) {
	files := make(map[string]string)
	if err := addWatchedFile(files, m.configPath); err != nil {
		return "", err
	}
	configDir := filepath.Dir(m.configPath)
	if err := addWatchedFile(files, filepath.Join(configDir, "secrets.yml")); err != nil {
		return "", err
	}
	if err := collectWatchedDirectory(files, filepath.Join(configDir, "yaml.d"), true); err != nil {
		return "", err
	}
	if err := collectWatchedDirectory(files, m.assetsDir, false); err != nil {
		return "", err
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		_, _ = fmt.Fprintf(hash, "%s\x00%s\x00", path, files[path])
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func addWatchedFile(files map[string]string, path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	files[path] = hex.EncodeToString(digest[:])
	return nil
}

func collectWatchedDirectory(files map[string]string, directory string, configDropins bool) error {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if ignoredWatchedName(name, configDropins) {
			continue
		}
		path := filepath.Join(directory, name)
		if entry.IsDir() {
			if configDropins {
				continue
			}
			if err := collectAssets(files, path); err != nil {
				return err
			}
			continue
		}
		if configDropins && !isYAMLDropin(name) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if !configDropins {
				continue
			}
			if err := addDropinSymlink(files, directory, path); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := addWatchedFile(files, path); err != nil {
			return err
		}
	}
	return nil
}

func collectAssets(files map[string]string, directory string) error {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		if entry.IsDir() {
			if err := collectAssets(files, path); err != nil {
				return err
			}
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			if err := addWatchedFile(files, path); err != nil {
				return err
			}
		}
	}
	return nil
}

func addDropinSymlink(files map[string]string, directory, path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	base, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(base, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil
	}
	info, err := os.Stat(resolved)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	target := make(map[string]string, 1)
	if err := addWatchedFile(target, resolved); err != nil {
		return err
	}
	files[path] = target[resolved]
	return nil
}

func ignoredWatchedName(name string, configDropins bool) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	if !configDropins {
		return false
	}
	return strings.HasSuffix(name, ".bak") || strings.HasSuffix(name, "~") || strings.HasSuffix(name, ".orig")
}

func isYAMLDropin(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml")
}

func compareConfigChanges(previous, next *ServerConfig) (ConfigChanges, error) {
	oldMap, err := configMap(previous)
	if err != nil {
		return ConfigChanges{}, err
	}
	newMap, err := configMap(next)
	if err != nil {
		return ConfigChanges{}, err
	}
	paths := make([]string, 0)
	compareYAMLValues(oldMap, newMap, "", &paths)
	if previous.Server.Token != next.Server.Token {
		paths = append(paths, "server.token")
	}
	sort.Strings(paths)
	changes := ConfigChanges{}
	for _, path := range paths {
		switch classifyConfigPath(path) {
		case "restart":
			changes.RestartRequired = append(changes.RestartRequired, path)
		case "graceful":
			changes.GracefulReload = append(changes.GracefulReload, path)
		default:
			changes.HotReload = append(changes.HotReload, path)
		}
	}
	return changes, nil
}

func configMap(cfg *ServerConfig) (map[string]interface{}, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var values map[string]interface{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func compareYAMLValues(previous, next interface{}, prefix string, changes *[]string) {
	oldMap, oldOK := previous.(map[string]interface{})
	newMap, newOK := next.(map[string]interface{})
	if oldOK && newOK {
		keys := make(map[string]struct{}, len(oldMap)+len(newMap))
		for key := range oldMap {
			keys[key] = struct{}{}
		}
		for key := range newMap {
			keys[key] = struct{}{}
		}
		ordered := make([]string, 0, len(keys))
		for key := range keys {
			ordered = append(ordered, key)
		}
		sort.Strings(ordered)
		for _, key := range ordered {
			oldValue, oldExists := oldMap[key]
			newValue, newExists := newMap[key]
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			if !oldExists || !newExists {
				*changes = append(*changes, path)
				continue
			}
			compareYAMLValues(oldValue, newValue, path, changes)
		}
		return
	}
	if !reflect.DeepEqual(previous, next) {
		*changes = append(*changes, prefix)
	}
}

func classifyConfigPath(path string) string {
	switch {
	case path == "server.port", path == "server.address", path == "server.daemonize":
		return "restart"
	case strings.HasPrefix(path, "server.ssl."), strings.HasPrefix(path, "server.database."), strings.HasPrefix(path, "server.tor."), strings.HasPrefix(path, "server.i2p."):
		return "restart"
	case path == "server.baseurl", strings.HasPrefix(path, "server.timeouts."):
		return "graceful"
	case strings.HasPrefix(path, "server.logs.") && path != "server.logs.level":
		return "graceful"
	default:
		return "hot"
	}
}

func mergeSorted(existing, added []string) []string {
	values := make(map[string]struct{}, len(existing)+len(added))
	for _, value := range existing {
		values[value] = struct{}{}
	}
	for _, value := range added {
		values[value] = struct{}{}
	}
	merged := make([]string, 0, len(values))
	for value := range values {
		merged = append(merged, value)
	}
	sort.Strings(merged)
	return merged
}
