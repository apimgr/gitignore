package i2p

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfigValidation(t *testing.T) {
	config := DefaultConfig()
	if err := config.validate(); err != nil {
		t.Fatalf("default config validation: %v", err)
	}
	config.InboundLength = 8
	if err := config.validate(); err == nil {
		t.Fatal("invalid tunnel length accepted")
	}
	cases := []func(*Config){
		func(c *Config) { c.SAMAddress = "" },
		func(c *Config) { c.VirtualPort = 0 },
		func(c *Config) { c.OutboundLength = 8 },
		func(c *Config) { c.InboundQuantity = 0 },
		func(c *Config) { c.OutboundQuantity = 17 },
		func(c *Config) { c.SignatureType = -1 },
		func(c *Config) { c.BootstrapTimeout = 29 * time.Second },
	}
	for index, mutate := range cases {
		candidate := DefaultConfig()
		mutate(&candidate)
		if err := candidate.validate(); err == nil {
			t.Errorf("invalid config case %d accepted", index)
		}
	}
}

func TestPathsValidation(t *testing.T) {
	valid := Paths{ConfigDir: "/config", DataDir: "/data", LogDir: "/logs"}
	if err := valid.validate(); err != nil {
		t.Fatal(err)
	}
	for _, paths := range []Paths{{DataDir: "/data", LogDir: "/logs"}, {ConfigDir: "relative", DataDir: "/data", LogDir: "/logs"}, {ConfigDir: "/config", DataDir: "relative", LogDir: "/logs"}} {
		if err := paths.validate(); err == nil {
			t.Errorf("invalid paths accepted: %+v", paths)
		}
	}
}

func TestSAMSessionLifecycle(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	root := t.TempDir()
	paths := Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), LogDir: filepath.Join(root, "logs")}
	keyDir := filepath.Join(paths.DataDir, "i2p", "site")
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		t.Fatal(err)
	}
	backend, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	private := make([]byte, 400)
	binary.BigEndian.PutUint16(private[385:387], 4)
	privateKey := i2pBase64.EncodeToString(private)
	go serveSAMTest(t, listener, privateKey, 3)
	service := &Service{
		config: DefaultConfig(), paths: paths, samAddress: listener.Addr().String(),
		backend: backend, state: Status{State: StateStarting},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.startSession(ctx); err != nil {
		t.Fatalf("startSession: %v", err)
	}
	if !strings.HasSuffix(service.Address(), ".b32.i2p") || service.Status().State != StateBuilding {
		t.Fatalf("session status = %+v", service.Status())
	}
	keyPath := filepath.Join(keyDir, "site-keys.dat")
	if info, err := os.Stat(keyPath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("destination key mode = %v, err=%v", info, err)
	}
	if err := service.Probe(ctx); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	_ = backend.Close()
}

func serveSAMTest(t *testing.T, listener net.Listener, privateKey string, connections int) {
	t.Helper()
	for range connections {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go func(conn net.Conn) {
			defer func() { _ = conn.Close() }()
			reader := bufio.NewReader(conn)
			line, err := reader.ReadString('\n')
			if err != nil || !strings.HasPrefix(line, "HELLO VERSION") {
				return
			}
			_, _ = fmt.Fprintln(conn, "HELLO REPLY RESULT=OK VERSION=3.3")
			line, err = reader.ReadString('\n')
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "DEST GENERATE"):
				_, _ = fmt.Fprintf(conn, "DEST REPLY PUB=public PRIV=%s\n", privateKey)
				line, err = reader.ReadString('\n')
				if err == nil && strings.HasPrefix(line, "SESSION CREATE") {
					_, _ = fmt.Fprintln(conn, "SESSION STATUS RESULT=OK")
				}
			case strings.HasPrefix(line, "STREAM FORWARD"):
				_, _ = fmt.Fprintln(conn, "STREAM STATUS RESULT=OK")
			case strings.HasPrefix(line, "QUIT"):
				_, _ = fmt.Fprintln(conn, "QUIT REPLY RESULT=OK")
			}
			_, _ = io.Copy(io.Discard, reader)
		}(conn)
	}
}

func TestReadDestinationAndAddress(t *testing.T) {
	destination := make([]byte, 391)
	binary.BigEndian.PutUint16(destination[385:387], 4)
	private := make([]byte, 400)
	copy(private, destination)
	encoded := i2pBase64.EncodeToString(private)
	decoded, err := readDestination(encoded)
	if err != nil {
		t.Fatalf("readDestination: %v", err)
	}
	if len(decoded) != len(destination) {
		t.Fatalf("destination length = %d, want %d", len(decoded), len(destination))
	}
	digest := sha256.Sum256(destination)
	want := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:])) + ".b32.i2p"
	if got := addressFor(decoded); got != want {
		t.Fatalf("addressFor() = %q, want %q", got, want)
	}
}

func TestRouterConfigIsIsolated(t *testing.T) {
	contents := routerConfig(23456, filepath.Join(t.TempDir(), "i2pd.log"))
	for _, required := range []string{"notransit = true", "floodfill = false", "port = 23456", "enabled = false"} {
		if !strings.Contains(contents, required) {
			t.Errorf("router config missing %q", required)
		}
	}
	for _, forbidden := range []string{"port = 7656", "port = 7070", "port = 4444", "address = 0.0.0.0"} {
		if strings.Contains(contents, forbidden) {
			t.Errorf("router config contains forbidden %q", forbidden)
		}
	}
}

func TestStripI2PHeaders(t *testing.T) {
	var got bool
	handler := stripI2PHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-I2P-Peer") == "" && r.Header.Get("X-Test") == "kept"
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-I2P-Peer", "private")
	request.Header.Set("X-Test", "kept")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if !got {
		t.Fatal("I2P headers were not stripped before the handler")
	}
}

func TestDisabledStartCreatesNoDirectories(t *testing.T) {
	root := t.TempDir()
	_, err := Start(context.Background(), DefaultConfig(), Paths{
		ConfigDir: filepath.Join(root, "config"),
		DataDir:   filepath.Join(root, "data"),
		LogDir:    filepath.Join(root, "logs"),
	}, http.NotFoundHandler())
	if err != ErrDisabled {
		t.Fatalf("Start() error = %v, want ErrDisabled", err)
	}
	if _, err := os.Stat(filepath.Join(root, "data")); !os.IsNotExist(err) {
		t.Fatalf("disabled start created data path: %v", err)
	}
}

func TestNoProviderCreatesNoFiles(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	config := DefaultConfig()
	config.Enabled = true
	config.Binary = filepath.Join(root, "missing-i2pd")
	config.SAMAddress = address
	config.BootstrapTimeout = 30 * time.Second
	_, err = Start(context.Background(), config, Paths{
		ConfigDir: filepath.Join(root, "config"),
		DataDir:   filepath.Join(root, "data"),
		LogDir:    filepath.Join(root, "logs"),
	}, http.NotFoundHandler())
	if !errors.Is(err, ErrNoProvider) {
		t.Fatalf("Start() error = %v, want ErrNoProvider", err)
	}
	for _, path := range []string{"config", "data", "logs"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Errorf("no-provider start created %s path: %v", path, err)
		}
	}
}
