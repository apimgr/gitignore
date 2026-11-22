package i2p

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrDisabled   = errors.New("I2P eepsite disabled")
	ErrNoProvider = errors.New("I2P provider unavailable")
)

type Provider string

const (
	ProviderNone Provider = "none"
	ProviderI2PD Provider = "i2pd"
	ProviderSAM  Provider = "sam"
)

type State string

const (
	StateDisabled   State = "disabled"
	StateNoProvider State = "no_provider"
	StateStarting   State = "starting"
	StateBuilding   State = "building"
	StateRunning    State = "running"
	StateError      State = "error"
)

type Config struct {
	Enabled          bool
	Binary           string
	SAMAddress       string
	VirtualPort      int
	InboundLength    int
	OutboundLength   int
	InboundQuantity  int
	OutboundQuantity int
	SignatureType    int
	BootstrapTimeout time.Duration
}

func DefaultConfig() Config {
	return Config{
		SAMAddress:       "127.0.0.1:7656",
		VirtualPort:      80,
		InboundLength:    3,
		OutboundLength:   3,
		InboundQuantity:  5,
		OutboundQuantity: 5,
		SignatureType:    7,
		BootstrapTimeout: 300 * time.Second,
	}
}

type Paths struct {
	ConfigDir string
	DataDir   string
	LogDir    string
}

type Status struct {
	State     State
	Provider  Provider
	Address   string
	Backend   string
	UpdatedAt time.Time
	Error     string
}

type Service struct {
	mu          sync.RWMutex
	closeOnce   sync.Once
	config      Config
	paths       Paths
	state       Status
	backend     net.Listener
	web         *http.Server
	provider    Provider
	samAddress  string
	samConn     net.Conn
	forwardConn net.Conn
	process     *exec.Cmd
	processDone chan struct{}
	samDone     chan struct{}
	forwardDone chan struct{}
	closeErr    error
}

// Start starts an isolated I2P eepsite and serves the supplied handler through
// a dedicated loopback HTTP listener. A missing provider returns an error and
// leaves no listener, key file, or generated configuration behind.
func Start(ctx context.Context, config Config, paths Paths, handler http.Handler) (*Service, error) {
	if !config.Enabled {
		return nil, ErrDisabled
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	if err := paths.validate(); err != nil {
		return nil, err
	}
	provider, binary, err := resolveProvider(config)
	if err != nil {
		return nil, err
	}
	service := &Service{
		config:   config,
		paths:    paths,
		provider: provider,
		state:    Status{State: StateStarting, Provider: provider, UpdatedAt: time.Now().UTC()},
	}
	if err := service.ensureDirectories(provider); err != nil {
		return nil, err
	}
	if provider == ProviderI2PD {
		service.samAddress, err = service.startRouter(ctx, binary)
		if err != nil {
			_ = service.Close()
			return nil, err
		}
	} else {
		service.samAddress = config.SAMAddress
	}
	service.backend, err = net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		_ = service.Close()
		return nil, fmt.Errorf("open I2P backend listener: %w", err)
	}
	if handler == nil {
		handler = http.NotFoundHandler()
	}
	handler = stripI2PHeaders(handler)
	service.web = &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := service.web.Serve(service.backend); err != nil && !errors.Is(err, http.ErrServerClosed) {
			service.setError(err)
		}
	}()
	if err := service.startSession(ctx); err != nil {
		_ = service.Close()
		return nil, err
	}
	service.setStatus(StateRunning, "")
	log.Printf("I2P eepsite started (%s): %s:%d → %s", service.provider, service.Address(), config.VirtualPort, service.backend.Addr())
	go service.closeOnContext(ctx)
	return service, nil
}

func stripI2PHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for key := range r.Header {
			if strings.HasPrefix(strings.ToLower(key), "x-i2p-") {
				delete(r.Header, key)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (p Paths) validate() error {
	for label, path := range map[string]string{"config": p.ConfigDir, "data": p.DataDir, "log": p.LogDir} {
		if path == "" || !filepath.IsAbs(path) {
			return fmt.Errorf("I2P %s directory must be an absolute path", label)
		}
	}
	return nil
}

func (c Config) validate() error {
	if c.SAMAddress == "" {
		return errors.New("I2P SAM address is empty")
	}
	if c.VirtualPort < 1 || c.VirtualPort > 65535 {
		return errors.New("I2P virtual port must be between 1 and 65535")
	}
	if c.InboundLength < 0 || c.InboundLength > 7 || c.OutboundLength < 0 || c.OutboundLength > 7 {
		return errors.New("I2P tunnel lengths must be between 0 and 7")
	}
	if c.InboundQuantity < 1 || c.InboundQuantity > 16 || c.OutboundQuantity < 1 || c.OutboundQuantity > 16 {
		return errors.New("I2P tunnel quantities must be between 1 and 16")
	}
	if c.SignatureType < 0 || c.BootstrapTimeout < 30*time.Second || c.BootstrapTimeout > 600*time.Second {
		return errors.New("I2P signature type or bootstrap timeout is invalid")
	}
	return nil
}

func resolveProvider(config Config) (Provider, string, error) {
	if config.Binary != "" {
		path, err := exec.LookPath(config.Binary)
		if err == nil {
			return ProviderI2PD, path, nil
		}
		if reachable(config.SAMAddress) {
			return ProviderSAM, "", nil
		}
		return ProviderNone, "", fmt.Errorf("%w: configured i2pd binary is unavailable and SAM at %s is unreachable", ErrNoProvider, config.SAMAddress)
	}
	for _, candidate := range []string{"/usr/bin/i2pd", "/usr/sbin/i2pd", "/usr/local/bin/i2pd", "/opt/homebrew/bin/i2pd"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return ProviderI2PD, candidate, nil
		}
	}
	if path, err := exec.LookPath("i2pd"); err == nil {
		return ProviderI2PD, path, nil
	}
	if reachable(config.SAMAddress) {
		return ProviderSAM, "", nil
	}
	return ProviderNone, "", fmt.Errorf("%w: SAM at %s is unreachable", ErrNoProvider, config.SAMAddress)
}

// DetectProvider reports whether an i2pd executable or reachable SAM bridge is
// available without creating files, allocating the backend port, or starting
// an I2P session.
func DetectProvider(config Config) (Provider, error) {
	provider, _, err := resolveProvider(config)
	return provider, err
}

func reachable(address string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := dialSAM(ctx, address, 5*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (s *Service) ensureDirectories(provider Provider) error {
	dirs := []string{
		filepath.Join(s.paths.DataDir, "i2p"),
		filepath.Join(s.paths.DataDir, "i2p", "site"),
	}
	if provider == ProviderI2PD {
		dirs = append(dirs, filepath.Join(s.paths.ConfigDir, "i2p"))
	}
	for _, path := range dirs {
		if err := os.MkdirAll(path, 0700); err != nil {
			return fmt.Errorf("create I2P directory %s: %w", path, err)
		}
		if err := os.Chmod(path, 0700); err != nil {
			return fmt.Errorf("secure I2P directory %s: %w", path, err)
		}
	}
	if provider == ProviderI2PD {
		if err := os.MkdirAll(s.paths.LogDir, 0700); err != nil {
			return fmt.Errorf("create I2P log directory: %w", err)
		}
	}
	return nil
}

func (s *Service) startRouter(ctx context.Context, binary string) (string, error) {
	configDir := filepath.Join(s.paths.ConfigDir, "i2p")
	dataDir := filepath.Join(s.paths.DataDir, "i2p")
	samPort, err := unusedLoopbackPort()
	if err != nil {
		return "", err
	}
	samAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(samPort))
	configPath := filepath.Join(configDir, "i2pd.conf")
	if err := writePrivateFile(configPath, []byte(routerConfig(samPort, filepath.Join(s.paths.LogDir, "i2pd.log")))); err != nil {
		return "", fmt.Errorf("write I2P router config: %w", err)
	}
	tunnelConfig := filepath.Join(configDir, "tunnels.conf")
	if err := writePrivateFile(tunnelConfig, nil); err != nil {
		return "", fmt.Errorf("write empty I2P tunnel config: %w", err)
	}
	tunnelDir := filepath.Join(configDir, "tunnels.d")
	if err := os.MkdirAll(tunnelDir, 0700); err != nil {
		return "", fmt.Errorf("create empty I2P tunnel directory: %w", err)
	}
	if err := os.Chmod(tunnelDir, 0700); err != nil {
		return "", fmt.Errorf("secure I2P tunnel directory: %w", err)
	}
	pidPath := filepath.Join(dataDir, "i2pd.pid")
	command := exec.Command(binary,
		"--conf", configPath,
		"--tunconf", tunnelConfig,
		"--tunnelsdir", tunnelDir,
		"--datadir", dataDir,
		"--pidfile", pidPath,
	)
	setParentDeathSignal(command)
	if err := command.Start(); err != nil {
		return "", fmt.Errorf("start dedicated i2pd: %w", err)
	}
	s.process = command
	s.processDone = make(chan struct{})
	go func() {
		_ = command.Wait()
		close(s.processDone)
	}()
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	for {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		conn, _, probeErr := dialSAM(probeCtx, samAddress, 5*time.Second)
		cancel()
		if probeErr == nil {
			_ = conn.Close()
			return samAddress, nil
		}
		select {
		case <-s.processDone:
			return "", fmt.Errorf("i2pd exited before its SAM port opened; check %s", filepath.Join(s.paths.LogDir, "i2pd.log"))
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", fmt.Errorf("i2pd SAM port did not open within 60 seconds; check %s", filepath.Join(s.paths.LogDir, "i2pd.log"))
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (s *Service) startSession(ctx context.Context) error {
	control, reader, err := dialSAM(ctx, s.samAddress, 10*time.Second)
	if err != nil {
		return fmt.Errorf("I2P SAM control connection failed: %w", err)
	}
	s.samConn = control
	keyPath := filepath.Join(s.paths.DataDir, "i2p", "site", "site-keys.dat")
	priv, err := s.loadOrCreateKey(control, reader, keyPath)
	if err != nil {
		return err
	}
	destination, err := readDestination(priv)
	if err != nil {
		return fmt.Errorf("invalid I2P key file %s: %w", keyPath, err)
	}
	address := addressFor(destination)
	s.setStatus(StateBuilding, "")
	sessionID := fmt.Sprintf("site-%d", os.Getpid())
	reply, err := samCommand(control, reader, s.config.BootstrapTimeout,
		"SESSION CREATE STYLE=STREAM ID=%s DESTINATION=%s i2cp.leaseSetEncType=4 inbound.length=%d outbound.length=%d inbound.quantity=%d outbound.quantity=%d",
		sessionID, priv, s.config.InboundLength, s.config.OutboundLength, s.config.InboundQuantity, s.config.OutboundQuantity)
	if err != nil {
		return fmt.Errorf("I2P SESSION CREATE failed: %w", err)
	}
	if reply.values["RESULT"] == "DUPLICATED_ID" {
		sessionID = fmt.Sprintf("%s-%d", sessionID, time.Now().UnixNano())
		reply, err = samCommand(control, reader, s.config.BootstrapTimeout,
			"SESSION CREATE STYLE=STREAM ID=%s DESTINATION=%s i2cp.leaseSetEncType=4 inbound.length=%d outbound.length=%d inbound.quantity=%d outbound.quantity=%d",
			sessionID, priv, s.config.InboundLength, s.config.OutboundLength, s.config.InboundQuantity, s.config.OutboundQuantity)
		if err != nil {
			return fmt.Errorf("I2P SESSION CREATE retry failed: %w", err)
		}
	}
	if reply.values["RESULT"] != "OK" {
		return fmt.Errorf("I2P SESSION CREATE rejected: %s", safeResult(reply.values["RESULT"]))
	}
	forward, forwardReader, err := dialSAM(ctx, s.samAddress, 10*time.Second)
	if err != nil {
		return fmt.Errorf("I2P SAM forward connection failed: %w", err)
	}
	backendPort := s.backend.Addr().(*net.TCPAddr).Port
	forwardReply, err := samCommand(forward, forwardReader, 10*time.Second,
		"STREAM FORWARD ID=%s PORT=%d HOST=127.0.0.1 SILENT=true", sessionID, backendPort)
	if err != nil || forwardReply.values["RESULT"] != "OK" {
		_ = forward.Close()
		return errors.New("I2P STREAM FORWARD was rejected")
	}
	s.forwardConn = forward
	s.samDone = make(chan struct{})
	s.forwardDone = make(chan struct{})
	go watchSAMConnection(control, s.samDone)
	go watchSAMConnection(forward, s.forwardDone)
	s.mu.Lock()
	s.state.Address = address
	s.state.Backend = s.backend.Addr().String()
	s.state.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()
	return nil
}

func (s *Service) loadOrCreateKey(conn net.Conn, reader *bufio.Reader, path string) (string, error) {
	info, statErr := os.Lstat(path)
	if statErr == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return "", fmt.Errorf("I2P key path %s is not a regular file", path)
	}
	if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
		return "", fmt.Errorf("inspect I2P key file %s: %w", path, statErr)
	}
	contents, err := os.ReadFile(path)
	if err == nil {
		priv := strings.TrimSpace(string(contents))
		if strings.ContainsAny(priv, "\r\n") {
			return "", fmt.Errorf("corrupt I2P key file %s: expected one key line", path)
		}
		if _, err := readDestination(priv); err != nil {
			return "", fmt.Errorf("corrupt I2P key file %s: %w", path, err)
		}
		if err := os.Chmod(path, 0600); err != nil {
			return "", fmt.Errorf("secure I2P key file %s: %w", path, err)
		}
		return priv, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("read I2P key file %s: %w", path, err)
	}
	reply, err := samCommand(conn, reader, 10*time.Second, "DEST GENERATE SIGNATURE_TYPE=%d", s.config.SignatureType)
	if err != nil {
		return "", fmt.Errorf("I2P DEST GENERATE failed: %w", err)
	}
	priv := reply.values["PRIV"]
	if reply.topic != "DEST" || reply.action != "REPLY" || priv == "" {
		return "", errors.New("I2P DEST GENERATE returned no private key")
	}
	if _, err := readDestination(priv); err != nil {
		return "", errors.New("I2P DEST GENERATE returned an invalid key")
	}
	if err := writePrivateFile(path, []byte(priv+"\n")); err != nil {
		return "", fmt.Errorf("persist I2P destination key %s: %w", path, err)
	}
	return priv, nil
}

func writePrivateFile(path string, contents []byte) error {
	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, ".i2p-key-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	if err := directory.Sync(); err != nil {
		return err
	}
	return nil
}

func unusedLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func routerConfig(samPort int, logPath string) string {
	logPath = strings.NewReplacer("\\", "\\\\", "\n", "", "\r", "").Replace(logPath)
	return fmt.Sprintf(`daemon = false
ipv4 = true
ipv6 = false
notransit = true
floodfill = false
log = file
logfile = %s
loglevel = warn

[http]
enabled = false
[httpproxy]
enabled = false
[socksproxy]
enabled = false
[sam]
enabled = true
address = 127.0.0.1
port = %d
[bob]
enabled = false
[i2cp]
enabled = false
[i2pcontrol]
enabled = false
[upnp]
enabled = false
`, logPath, samPort)
}

func safeResult(result string) string {
	if result == "" {
		return "unknown result"
	}
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '_' {
			return r
		}
		return -1
	}, result)
}

func watchSAMConnection(conn net.Conn, done chan<- struct{}) {
	_, _ = io.Copy(io.Discard, conn)
	close(done)
}

func (s *Service) setStatus(state State, message string) {
	s.mu.Lock()
	s.state.State = state
	s.state.UpdatedAt = time.Now().UTC()
	s.state.Error = message
	s.mu.Unlock()
}

func (s *Service) setError(err error) {
	if err == nil {
		return
	}
	s.setStatus(StateError, err.Error())
	log.Printf("I2P provider error: %v", err)
}

func (s *Service) closeOnContext(ctx context.Context) {
	<-ctx.Done()
	_ = s.Close()
}

// Status returns the current provider state without exposing the destination key.
func (s *Service) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// Address returns the persistent .b32.i2p address, or an empty string if startup failed.
func (s *Service) Address() string { return s.Status().Address }

// BackendAddr returns the dedicated loopback listener address.
func (s *Service) BackendAddr() string {
	if s.backend == nil {
		return ""
	}
	return s.backend.Addr().String()
}

// Probe checks the SAM bridge over a short-lived independent socket. It never
// sends a command on the control socket that owns the active session.
func (s *Service) Probe(ctx context.Context) error {
	conn, reader, err := dialSAM(ctx, s.samAddress, 5*time.Second)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	reply, err := samCommand(conn, reader, 5*time.Second, "QUIT")
	if err != nil {
		return err
	}
	if reply.topic != "QUIT" || reply.action != "REPLY" || reply.values["RESULT"] != "OK" {
		return errors.New("SAM liveness probe failed")
	}
	return nil
}

// Monitor probes SAM every 30 seconds on a separate connection. Three
// consecutive failures mark the service unhealthy; callers may use the state
// change to recreate the provider with the same persisted destination key.
func (s *Service) Monitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		select {
		case <-s.samDone:
			s.setError(errors.New("I2P SAM session connection closed"))
			return
		case <-s.forwardDone:
			s.setError(errors.New("I2P SAM forward connection closed"))
			return
		default:
		}
		if err := s.Probe(ctx); err != nil {
			failures++
			if failures >= 3 {
				s.setError(fmt.Errorf("I2P SAM liveness probe failed three times: %w", err))
				return
			}
			continue
		}
		failures = 0
	}
}

// Close stops HTTP forwarding, closes SAM forward/control sockets, then stops
// a managed i2pd process. An external SAM router is never signaled.
func (s *Service) Close() error {
	s.closeOnce.Do(func() {
		if s.web != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s.closeErr = s.web.Shutdown(ctx)
		}
		if s.forwardConn != nil {
			if err := s.forwardConn.Close(); err != nil && s.closeErr == nil {
				s.closeErr = err
			}
		}
		if s.samConn != nil {
			if err := s.samConn.Close(); err != nil && s.closeErr == nil {
				s.closeErr = err
			}
		}
		if s.process != nil {
			stopRouter(s.process, s.processDone)
		}
		s.setStatus(StateDisabled, "")
	})
	return s.closeErr
}

func stopRouter(command *exec.Cmd, done <-chan struct{}) {
	if command == nil || command.Process == nil {
		return
	}
	_ = requestRouterStop(command)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = command.Process.Kill()
		<-done
	}
}
