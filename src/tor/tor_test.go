package tor

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cretz/bine/control"
	"github.com/cretz/bine/torutil/ed25519"

	"github.com/apimgr/gitignore/src/config"
)

// newTestKey builds a valid ed25519 onion key blob and its service ID for tests.
func newTestKey(t *testing.T) (blob, serviceID string) {
	t.Helper()
	kp, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	ek := &control.ED25519Key{KeyPair: kp}
	id, err := ServiceIDFromBlob(ek.Blob())
	if err != nil {
		t.Fatalf("ServiceIDFromBlob: %v", err)
	}
	return ek.Blob(), id
}

func TestDefaultTorConfig(t *testing.T) {
	c := config.DefaultTorConfig()
	if c.MaxCircuits != 32 {
		t.Errorf("MaxCircuits = %d, want 32", c.MaxCircuits)
	}
	if c.BootstrapTimeout != 180 {
		t.Errorf("BootstrapTimeout = %d, want 180", c.BootstrapTimeout)
	}
	if !c.SafeLogging {
		t.Error("SafeLogging = false, want true")
	}
	if c.VirtualPort != 80 {
		t.Errorf("VirtualPort = %d, want 80", c.VirtualPort)
	}
	if c.UseNetwork {
		t.Error("UseNetwork = true, want false (outbound off by default)")
	}
}

func TestGetTorConfig(t *testing.T) {
	c := config.DefaultTorConfig()

	out := getTorConfig(&c)
	if !strings.Contains(out, "ControlPort 127.0.0.1:auto") {
		t.Error("missing auto control port")
	}
	if !strings.Contains(out, "SocksPort 0") {
		t.Error("hidden-service-only config should disable SocksPort")
	}
	if strings.Contains(out, "9050") || strings.Contains(out, "9051") {
		t.Error("torrc must never reference default tor ports")
	}
	if !strings.Contains(out, "SafeLogging 1") {
		t.Error("SafeLogging should be enabled")
	}
	if !strings.Contains(out, "AccountingMax 100 GB") {
		t.Error("monthly bandwidth accounting missing")
	}

	c.UseNetwork = true
	if !strings.Contains(getTorConfig(&c), "SocksPort auto") {
		t.Error("outbound enabled should use SocksPort auto")
	}

	c.SafeLogging = false
	if !strings.Contains(getTorConfig(&c), "SafeLogging 0") {
		t.Error("SafeLogging disable not honored")
	}

	c.MaxMonthlyBandwidth = "unlimited"
	if strings.Contains(getTorConfig(&c), "AccountingMax") {
		t.Error("unlimited bandwidth must not emit accounting limits")
	}
}

func TestEnsureTorDirs(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	dataDir := filepath.Join(dir, "data")

	if err := ensureTorDirs(configDir, dataDir); err != nil {
		t.Fatalf("ensureTorDirs: %v", err)
	}

	for _, p := range []string{
		filepath.Join(configDir, "tor"),
		filepath.Join(dataDir, "tor"),
		filepath.Join(dataDir, "tor", "site"),
	} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
			t.Errorf("%s perm = %o, want 700", p, info.Mode().Perm())
		}
	}
}

func TestEnsureTorrcPersistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tor", "torrc")

	created, err := ensureTorrc(path, []byte("original"))
	if err != nil || !created {
		t.Fatalf("first ensureTorrc: created=%v err=%v", created, err)
	}

	created, err = ensureTorrc(path, []byte("replacement"))
	if err != nil || created {
		t.Fatalf("second ensureTorrc: created=%v err=%v", created, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read torrc: %v", err)
	}
	if string(data) != "original" {
		t.Errorf("torrc overwritten: got %q, want original", data)
	}
}

func TestUpdateTorrcOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tor", "torrc")
	if _, err := ensureTorrc(path, []byte("original")); err != nil {
		t.Fatalf("ensureTorrc: %v", err)
	}
	if err := updateTorrc(path, []byte("updated")); err != nil {
		t.Fatalf("updateTorrc: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "updated" {
		t.Errorf("updateTorrc: got %q, want updated", data)
	}
}

func TestSaveLoadOnionKeyRoundTrip(t *testing.T) {
	blob, wantID := newTestKey(t)
	key, err := control.ED25519KeyFromBlob(blob)
	if err != nil {
		t.Fatalf("ED25519KeyFromBlob: %v", err)
	}

	path := filepath.Join(t.TempDir(), "site", "hs_ed25519_secret_key")
	if err := saveOnionKey(path, key); err != nil {
		t.Fatalf("saveOnionKey: %v", err)
	}

	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Errorf("key perm = %o, want 600", info.Mode().Perm())
		}
	}

	loaded, err := loadOnionKey(path)
	if err != nil {
		t.Fatalf("loadOnionKey: %v", err)
	}
	if loaded == nil {
		t.Fatal("loadOnionKey returned nil for existing key")
	}
	gotID, err := ServiceIDFromBlob(loaded.Blob())
	if err != nil {
		t.Fatalf("ServiceIDFromBlob(loaded): %v", err)
	}
	if gotID != wantID {
		t.Errorf("service ID after round trip = %s, want %s", gotID, wantID)
	}
}

func TestLoadOnionKeyMissing(t *testing.T) {
	key, err := loadOnionKey(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("loadOnionKey(absent): %v", err)
	}
	if key != nil {
		t.Error("expected nil key for missing file")
	}
}
