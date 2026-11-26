// Package tor implements the built-in Tor hidden service (AI.md PART 31).
//
// The server binary owns a dedicated external Tor process. Tor's hidden
// service is configured in torrc and forwards to a dedicated loopback backend.
//
// Tor is optional: a missing binary is logged at INFO and the server continues
// without Tor. All operations are best-effort and never block server startup.
package tor

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cretz/bine/control"
	"golang.org/x/net/proxy"

	"github.com/apimgr/gitignore/src/config"
)

// TorService manages a running Tor hidden service and optional outbound dialer.
// The server binary fully owns and controls the Tor process lifecycle.
type TorService struct {
	cmd            *exec.Cmd
	ctrl           *control.Conn
	done           chan error
	onionAddress   string
	torBackendPort int
	// dialer routes outbound connections through Tor (nil when disabled).
	dialer proxy.ContextDialer
}

// OnionAddress returns the full .onion address.
func (s *TorService) OnionAddress() string {
	return s.onionAddress
}

// OutboundEnabled reports whether Tor outbound connections are available.
func (s *TorService) OutboundEnabled() bool {
	return s.dialer != nil
}

// GetHTTPClient returns an HTTP client, optionally routed through Tor. When
// useTor is false or no dialer is configured, a direct client is returned.
func (s *TorService) GetHTTPClient(useTor bool) *http.Client {
	if !useTor || s.dialer == nil {
		return &http.Client{Timeout: 30 * time.Second}
	}
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			DialContext: s.dialer.DialContext,
		},
	}
}

// Close shuts down the Tor process.
func (s *TorService) Close() error {
	if s.ctrl != nil {
		_ = s.ctrl.Signal("SHUTDOWN")
		_ = s.ctrl.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		select {
		case err := <-s.done:
			if err != nil {
				return err
			}
		case <-time.After(5 * time.Second):
			if err := s.cmd.Process.Kill(); err != nil {
				return err
			}
			if waitErr := <-s.done; waitErr != nil {
				var exitErr *exec.ExitError
				if !errors.As(waitErr, &exitErr) {
					return waitErr
				}
			}
		}
	}
	return nil
}

// commonTorPaths lists well-known tor binary locations checked after PATH.
func commonTorPaths() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{`C:\Program Files\Tor\tor.exe`, `C:\Program Files (x86)\Tor\tor.exe`}
	case "darwin":
		return []string{"/usr/local/bin/tor", "/opt/homebrew/bin/tor"}
	default:
		return []string{"/usr/bin/tor", "/usr/local/bin/tor", "/bin/tor"}
	}
}

// FindBinary resolves the tor executable. It checks the configured path first,
// then PATH, then common install locations. The second return value reports
// whether a usable binary was found.
func FindBinary(configured string) (string, bool) {
	if configured != "" {
		if _, err := os.Stat(configured); err == nil {
			return configured, true
		}
		return "", false
	}
	if p, err := exec.LookPath("tor"); err == nil {
		return p, true
	}
	for _, p := range commonTorPaths() {
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}

func resolveTorBinary(cfg *config.TorConfig) (string, error) {
	if cfg != nil && cfg.Binary != "" {
		if _, err := os.Stat(cfg.Binary); err == nil {
			return cfg.Binary, nil
		}
		return "", fmt.Errorf("configured tor binary not found: %s", cfg.Binary)
	}
	for _, path := range []string{"/usr/bin/tor", "/usr/sbin/tor", "/usr/local/bin/tor", "/opt/homebrew/bin/tor"} {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	if path, err := exec.LookPath("tor"); err == nil {
		return path, nil
	}
	return "", errors.New("tor binary not found; hidden service disabled")
}

// getTorConfig renders the managed Tor configuration and its v3 hidden service.
func getTorConfig(cfg *config.TorConfig, paths ...interface{}) string {
	dataDir, hsDir, backendPort := "tor", filepath.Join("tor", "site"), 1
	if len(paths) > 0 {
		if v, ok := paths[0].(string); ok {
			dataDir = v
		}
	}
	if len(paths) > 1 {
		if v, ok := paths[1].(string); ok {
			hsDir = v
		}
	}
	if len(paths) > 2 {
		if v, ok := paths[2].(int); ok {
			backendPort = v
		}
	}
	controlConfig := fmt.Sprintf("DataDirectory %s\nControlPort 127.0.0.1:auto\nControlPortWriteToFile %s\nCookieAuthentication 1\nDisableNetwork 1\n__OwningControllerProcess %d", dataDir, filepath.Join(dataDir, "control_port"), os.Getpid())

	socksConfig := "SocksPort 0"
	if cfg.UseNetwork {
		socksConfig = "SocksPort 127.0.0.1:auto"
	}

	safeLogging := "1"
	if !cfg.SafeLogging {
		safeLogging = "0"
	}

	var accountingConfig string
	if cfg.MaxMonthlyBandwidth != "" && cfg.MaxMonthlyBandwidth != "unlimited" {
		accountingConfig = fmt.Sprintf(`
# Monthly bandwidth limit
AccountingStart month 1 00:00
AccountingMax %s`, cfg.MaxMonthlyBandwidth)
	}

	return fmt.Sprintf(`
# ============================================================
# Tor Configuration - Generated by server binary
# This file is regenerated on every start from current configuration
# ============================================================

# SOCKS port for outbound connections (0 = disabled, auto = runtime port)
# Never uses the default SOCKS port - runtime detection only
%s

# Control connection
# Never uses the default control port - uses a runtime localhost port on all OSes
%s

# Security Hardening
SafeLogging %s

# Circuit limits
MaxCircuitDirtiness 600

# Bandwidth limits per second (from config)
BandwidthRate %s
BandwidthBurst %s
%s

# Disable unused features - not a relay or exit
ExitRelay 0
ExitPolicy reject *:*
ORPort 0
DirPort 0

# Hidden service optimizations
HiddenServiceSingleHopMode 0
VanguardsLiteEnabled 1
MaxClientCircuitsPending %d
LearnCircuitBuildTimeout 0
CircuitBuildTimeout %d

# Faster startup
FetchDirInfoEarly 1
FetchDirInfoExtraEarly 1

# Reduce memory usage
DisableDebuggerAttachment 1

HiddenServiceDir %s
HiddenServiceVersion 3
HiddenServiceNumIntroductionPoints %d
HiddenServiceMaxStreams %d
HiddenServiceMaxStreamsCloseCircuit %d
HiddenServicePort %d 127.0.0.1:%d
HiddenServiceExportCircuitID haproxy
`, socksConfig, controlConfig, safeLogging, cfg.BandwidthRate, cfg.BandwidthBurst, accountingConfig, cfg.MaxCircuits, cfg.CircuitTimeout, hsDir, cfg.NumIntroPoints, cfg.MaxStreamsPerCircuit, boolInt(cfg.CloseCircuitOnStreamLimit), cfg.VirtualPort, backendPort)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// ensureTorDirs creates the Tor directories with 0700 permissions and enforces
// ownership. configDir holds the torrc; dataDir holds runtime data and keys.
func ensureTorDirs(configDir, dataDir string) error {
	dirs := []string{
		filepath.Join(configDir, "tor"),
		filepath.Join(dataDir, "tor"),
		filepath.Join(dataDir, "tor", "site"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("create tor dir %s: %w", dir, err)
		}
		if err := os.Chmod(dir, 0700); err != nil {
			return fmt.Errorf("chmod tor dir %s: %w", dir, err)
		}
		if runtime.GOOS != "windows" {
			if err := os.Chown(dir, os.Getuid(), os.Getgid()); err != nil {
				return fmt.Errorf("chown tor dir %s: %w", dir, err)
			}
		}
	}
	return nil
}

// ensureTorrc creates torrc only if it does not already exist. It returns true
// when the file was newly created. Existing files are preserved (permissions
// are re-enforced).
func ensureTorrc(path string, content []byte) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return false, fmt.Errorf("create parent dir: %w", err)
	}

	if _, err := os.Stat(path); err == nil {
		if err := os.Chmod(path, 0600); err != nil {
			return false, fmt.Errorf("chmod file: %w", err)
		}
		return false, nil
	}

	if err := os.WriteFile(path, content, 0600); err != nil {
		return false, fmt.Errorf("write file: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chown(path, os.Getuid(), os.Getgid()); err != nil {
			return false, fmt.Errorf("chown file: %w", err)
		}
	}
	return true, nil
}

// updateTorrc overwrites torrc with new content for operator-driven config
// changes.
func updateTorrc(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	if err := os.WriteFile(path, content, 0600); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("chmod file: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chown(path, os.Getuid(), os.Getgid()); err != nil {
			return fmt.Errorf("chown file: %w", err)
		}
	}
	return nil
}

// ensureTorFile writes a generic Tor file (keys, hostname) with 0600
// permissions, overwriting any existing content.
func ensureTorFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	if err := os.WriteFile(path, content, 0600); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("chmod file: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chown(path, os.Getuid(), os.Getgid()); err != nil {
			return fmt.Errorf("chown file: %w", err)
		}
	}
	return nil
}

// saveOnionKey persists the ed25519 secret key blob so the .onion address is
// stable across restarts. The blob is the base64 form returned by bine.
func saveOnionKey(path string, key control.Key) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(key.Blob()), 0600)
}

// loadOnionKey reads a persisted ed25519 secret key blob and reconstructs a
// control.Key. It returns (nil, nil) when no key file exists yet.
func loadOnionKey(path string) (control.Key, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(blob) == 0 {
		return nil, nil
	}
	return control.ED25519KeyFromBlob(string(blob))
}

// startDedicatedTor starts the app-owned Tor process and waits for its control
// port and hidden-service hostname. backendPort is reserved for the caller's
// dedicated PROXY-protocol listener.
func startDedicatedTor(ctx context.Context, backendPort int, configDir, dataDir string, cfg *config.TorConfig) (*TorService, error) {
	torBinary, err := resolveTorBinary(cfg)
	if err != nil {
		return nil, err
	}
	if backendPort == 0 {
		backendPort, err = reserveLoopbackPort()
		if err != nil {
			return nil, err
		}
	}
	if err := ensureTorDirs(configDir, dataDir); err != nil {
		return nil, fmt.Errorf("failed to create tor directories: %w", err)
	}
	torDataDir := filepath.Join(dataDir, "tor")
	hsDir := filepath.Join(torDataDir, "site")
	torrcPath := filepath.Join(configDir, "tor", "torrc")
	controlPortPath := filepath.Join(torDataDir, "control_port")
	if err := os.Remove(controlPortPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := updateTorrc(torrcPath, []byte(getTorConfig(cfg, torDataDir, hsDir, backendPort))); err != nil {
		return nil, fmt.Errorf("write torrc %s: %w", torrcPath, err)
	}
	cmd := exec.CommandContext(ctx, torBinary, "-f", torrcPath)
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, torBinary, "-f", torrcPath)
	}
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start tor: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stopOnError := func(err error, ctrl *control.Conn) (*TorService, error) {
		if ctrl != nil {
			_ = ctrl.Signal("SHUTDOWN")
			_ = ctrl.Close()
		}
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			select {
			case waitErr := <-done:
				if waitErr != nil {
					log.Printf("tor: process exit while cleaning up startup failure: %v", waitErr)
				}
			default:
			}
		}
		return nil, err
	}
	startupTimeout := torStartupTimeout(cfg)
	if startupTimeout <= 0 {
		startupTimeout = 60
	}
	addr, err := waitControlPort(ctx, done, controlPortPath, time.Duration(startupTimeout)*time.Second)
	if err != nil {
		return stopOnError(fmt.Errorf("tor control port (%s): %w", torrcPath, err), nil)
	}
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return stopOnError(fmt.Errorf("connect to tor control port: %w", err), nil)
	}
	ctrl := control.NewConn(textproto.NewConn(conn))
	if err := ctrl.Authenticate(""); err != nil {
		return stopOnError(fmt.Errorf("authenticate to tor control port: %w", err), ctrl)
	}
	if _, err := ctrl.SendRequest("SETCONF DisableNetwork=0"); err != nil {
		return stopOnError(fmt.Errorf("enable tor network: %w", err), ctrl)
	}
	bootstrapTimeout := cfg.BootstrapTimeout
	if bootstrapTimeout <= 0 {
		bootstrapTimeout = 180
	}
	bootCtx, cancel := context.WithTimeout(ctx, time.Duration(bootstrapTimeout)*time.Second)
	defer cancel()
	if err := waitBootstrap(bootCtx, done, ctrl); err != nil {
		return stopOnError(fmt.Errorf("tor bootstrap: %w", err), ctrl)
	}
	hostnamePath := filepath.Join(hsDir, "hostname")
	onion, err := waitHostname(bootCtx, done, hostnamePath)
	if err != nil {
		return stopOnError(fmt.Errorf("tor hostname: %w", err), ctrl)
	}
	svc := &TorService{cmd: cmd, ctrl: ctrl, done: done, onionAddress: onion, torBackendPort: backendPort}
	if cfg.UseNetwork {
		if values, e := ctrl.GetInfo("net/listeners/socks"); e == nil && len(values) > 0 {
			addr := strings.Trim(values[0].Val, "\"[]")
			var d proxy.Dialer
			d, e = proxy.SOCKS5("tcp", addr, nil, proxy.Direct)
			if e == nil {
				svc.dialer, _ = d.(proxy.ContextDialer)
			}
		}
	}
	return svc, nil
}

func torStartupTimeout(cfg *config.TorConfig) int {
	if cfg == nil {
		return 60
	}
	field := reflect.ValueOf(cfg).Elem().FieldByName("StartupTimeout")
	if field.IsValid() && field.Kind() == reflect.Int {
		return int(field.Int())
	}
	return 60
}

func torSecretKeyFile(data []byte) ([]byte, error) {
	const header = "== ed25519v1-secret: type0 ==\x00\x00\x00"
	if strings.HasPrefix(string(data), header) && len(data) == len(header)+64 {
		return data, nil
	}
	key := data
	if decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data))); err == nil {
		key = decoded
	}
	if len(key) != 64 {
		return nil, fmt.Errorf("invalid Tor v3 secret key length: %d", len(key))
	}
	return append([]byte(header), key...), nil
}

func torSecretKeyBlob(data []byte) (string, error) {
	const header = "== ed25519v1-secret: type0 ==\x00\x00\x00"
	if strings.HasPrefix(string(data), header) {
		if len(data) != len(header)+64 {
			return "", fmt.Errorf("invalid Tor v3 secret key length: %d", len(data)-len(header))
		}
		return base64.StdEncoding.EncodeToString(data[len(header):]), nil
	}
	blob := strings.TrimSpace(string(data))
	if _, err := ServiceIDFromBlob(blob); err != nil {
		return "", err
	}
	return blob, nil
}

func reserveLoopbackPort() (port int, resultErr error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() {
		if closeErr := l.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, closeErr)
		}
	}()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func waitControlPort(ctx context.Context, done <-chan error, path string, timeout time.Duration) (string, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if b, err := os.ReadFile(path); err == nil {
			line := strings.TrimSpace(string(b))
			if strings.HasPrefix(line, "PORT=127.0.0.1:") {
				port := strings.TrimPrefix(line, "PORT=127.0.0.1:")
				if n, e := strconv.Atoi(port); e == nil && n > 0 && n <= 65535 {
					return net.JoinHostPort("127.0.0.1", port), nil
				}
			}
		}
		select {
		case err := <-done:
			return "", fmt.Errorf("tor process exited before control port was ready: %v", err)
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", fmt.Errorf("timed out after %s waiting for %s", timeout, path)
		case <-ticker.C:
		}
	}
}

func waitBootstrap(ctx context.Context, done <-chan error, ctrl *control.Conn) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		vals, err := ctrl.GetInfo("status/bootstrap-phase")
		if err == nil && len(vals) > 0 && strings.Contains(vals[0].Val, "PROGRESS=100") {
			return nil
		}
		select {
		case err := <-done:
			return fmt.Errorf("tor process exited: %v", err)
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitHostname(ctx context.Context, done <-chan error, path string) (string, error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if b, err := os.ReadFile(path); err == nil {
			host := strings.TrimSpace(string(b))
			if strings.HasSuffix(host, ".onion") {
				return host, nil
			}
		}
		select {
		case err := <-done:
			return "", fmt.Errorf("tor process exited: %v", err)
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}

// TorManager owns the Tor process lifecycle: start, restart, config updates,
// address regeneration, key import, and shutdown. It is safe for concurrent
// use.
type TorManager struct {
	mu          sync.Mutex
	service     *TorService
	config      *config.TorConfig
	configDir   string
	dataDir     string
	backendPort atomic.Int32
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewTorManager creates a Tor manager. serverPort is retained for API
// compatibility; Tor forwards to its separately allocated backend port.
func NewTorManager(ctx context.Context, serverPort int, configDir, dataDir string, cfg *config.TorConfig) *TorManager {
	mctx, cancel := context.WithCancel(ctx)
	return &TorManager{
		config:    cfg,
		configDir: configDir,
		dataDir:   dataDir,
		ctx:       mctx,
		cancel:    cancel,
	}
}

// Start initializes Tor. Callers should treat a returned error as non-fatal:
// the server continues without Tor.
func (tm *TorManager) Start() error {
	if _, err := resolveTorBinary(tm.config); err != nil {
		log.Printf("INFO: Tor binary not found, hidden service disabled")
		return nil
	}
	go func() {
		tm.mu.Lock()
		defer tm.mu.Unlock()
		if err := tm.startLocked(); err != nil {
			log.Printf("WARN: Tor not started: %v", err)
		}
	}()
	return nil
}

// startLocked starts Tor; the caller must hold tm.mu.
func (tm *TorManager) startLocked() error {
	tm.backendPort.Store(0)
	if _, err := resolveTorBinary(tm.config); err != nil {
		return err
	}
	backendPort, err := reserveLoopbackPort()
	if err != nil {
		return fmt.Errorf("reserve Tor backend port: %w", err)
	}
	tm.backendPort.Store(int32(backendPort))
	service, err := startDedicatedTor(tm.ctx, backendPort, tm.configDir, tm.dataDir, tm.config)
	if err != nil {
		tm.backendPort.Store(0)
		return err
	}
	tm.service = service
	return nil
}

// Restart stops and starts Tor with the current config.
func (tm *TorManager) Restart() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if tm.service != nil {
		if err := tm.service.Close(); err != nil {
			tm.service = nil
			return fmt.Errorf("failed to stop Tor before restart: %w", err)
		}
		tm.service = nil
	}
	return tm.startLocked()
}

// UpdateConfig regenerates torrc from cfg and restarts Tor.
func (tm *TorManager) UpdateConfig(cfg *config.TorConfig) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.config = cfg
	if tm.service != nil {
		if err := tm.service.Close(); err != nil {
			tm.service = nil
			return fmt.Errorf("failed to stop Tor before configuration update: %w", err)
		}
		tm.service = nil
	}

	return tm.startLocked()
}

// RegenerateAddress deletes the persisted keys and starts Tor with a fresh
// random .onion address.
func (tm *TorManager) RegenerateAddress() (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.service != nil {
		if err := tm.service.Close(); err != nil {
			tm.service = nil
			return "", fmt.Errorf("failed to stop Tor before address regeneration: %w", err)
		}
		tm.service = nil
	}

	keysDir := filepath.Join(tm.dataDir, "tor", "site")
	if err := os.RemoveAll(keysDir); err != nil {
		return "", fmt.Errorf("failed to remove old keys: %w", err)
	}

	if err := tm.startLocked(); err != nil {
		return "", err
	}
	return tm.service.OnionAddress(), nil
}

// ApplyKeys replaces the persisted secret key blob and restarts Tor. The blob
// is the base64 ed25519 form produced by ExportKey / bine's Key.Blob().
func (tm *TorManager) ApplyKeys(privateKey []byte) (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.service != nil {
		_ = tm.service.Close()
		tm.service = nil
	}

	keyPath := filepath.Join(tm.dataDir, "tor", "site", "hs_ed25519_secret_key")
	keyFile, err := torSecretKeyFile(privateKey)
	if err != nil {
		return "", err
	}
	if err := ensureTorFile(keyPath, keyFile); err != nil {
		return "", fmt.Errorf("failed to write key: %w", err)
	}

	if err := tm.startLocked(); err != nil {
		return "", err
	}
	return tm.service.OnionAddress(), nil
}

// Close shuts down the Tor process and cancels the manager context.
func (tm *TorManager) Close() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.service != nil {
		err := tm.service.Close()
		tm.service = nil
		tm.backendPort.Store(0)
		if tm.cancel != nil {
			tm.cancel()
		}
		return err
	}
	if tm.cancel != nil {
		tm.cancel()
	}
	tm.backendPort.Store(0)
	return nil
}

// OnionAddress returns the current .onion address, or "" when Tor is not
// running.
func (tm *TorManager) OnionAddress() string {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if tm.service != nil {
		return tm.service.OnionAddress()
	}
	return ""
}

// BackendPort returns the dedicated loopback port for the caller's
// PROXY-protocol listener. It is zero when Tor is unavailable or stopped.
func (tm *TorManager) BackendPort() int {
	return int(tm.backendPort.Load())
}

// HealthCheck probes the active control connection and restarts Tor when it is
// unavailable. It is called by the mode-bound scheduler task.
func (tm *TorManager) HealthCheck(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tm.mu.Lock()
	service := tm.service
	var probeErr error
	if service == nil || service.ctrl == nil {
		probeErr = errors.New("tor service is not running")
	} else {
		_, probeErr = service.ctrl.GetInfo("version")
	}
	tm.mu.Unlock()
	if probeErr == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	log.Printf("Tor health check failed; restarting: %v", probeErr)
	return tm.Restart()
}

// GetHTTPClient returns an HTTP client, optionally routed through Tor. It falls
// back to a direct client when Tor is not running.
func (tm *TorManager) GetHTTPClient(useTor bool) *http.Client {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if tm.service != nil {
		return tm.service.GetHTTPClient(useTor)
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Monitor watches the running Tor process and restarts it via the manager when
// the control connection becomes unresponsive. It returns when ctx is done.
func (tm *TorManager) Monitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tm.mu.Lock()
			svc := tm.service
			tm.mu.Unlock()
			if svc == nil || svc.ctrl == nil {
				continue
			}
			if _, err := svc.ctrl.GetInfo("version"); err != nil {
				log.Printf("Tor: connection lost, reconnecting: %v", err)
				if err := tm.Restart(); err != nil {
					log.Printf("Tor: restart failed: %v", err)
				}
			}
		}
	}
}
