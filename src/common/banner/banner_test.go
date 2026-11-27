package banner

import (
	"github.com/apimgr/gitignore/src/common/terminal"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPrintStartupUsesPlainContentOnDumbTerminal(t *testing.T) {
	t.Setenv("TERM", "dumb")
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = write
	defer func() { os.Stdout = previous }()
	PrintStartup(Config{AppName: "gitignore", Version: "1.2.3", Mode: "production", URLs: []string{"http://localhost:64080"}})
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	output := string(data)
	for _, expected := range []string{"gitignore v1.2.3", "Mode: production", "http://localhost:64080"} {
		if !strings.Contains(output, expected) {
			t.Errorf("startup output missing %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "\x1b") {
		t.Fatalf("dumb-terminal output contains an escape sequence: %q", output)
	}
}

func TestPrintStartupUsesASCIIArtAtWideWidth(t *testing.T) {
	t.Setenv("TERM", "xterm")
	t.Setenv("NO_COLOR", "")
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = write
	PrintStartup(Config{AppName: "gitignore", Version: "1.2.3", Mode: "production"})
	os.Stdout = previous
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "____ ___ _____ ___ ____") {
		t.Fatalf("wide startup banner missing ASCII art: %s", data)
	}
}

func TestCompactMinimalAndMicroLayouts(t *testing.T) {
	cfg := BannerConfig{AppName: "gitignore", Version: "1.2.3", AppMode: "production", ListenURL: "https://example.test:8443/path", URLs: []string{"https://example.test:8443/path"}}
	for _, layout := range []struct {
		name string
		run  func(BannerConfig)
		want string
	}{
		{name: "compact", run: printStartupBannerCompact, want: "Listening on"},
		{name: "minimal", run: printStartupBannerMinimal, want: "gitignore 1.2.3"},
		{name: "micro", run: printStartupBannerMicro, want: "gitignore :8443"},
	} {
		t.Run(layout.name, func(t *testing.T) {
			read, write, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			previous := os.Stdout
			os.Stdout = write
			layout.run(cfg)
			os.Stdout = previous
			if err := write.Close(); err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(read)
			if err != nil {
				t.Fatal(err)
			}
			_ = read.Close()
			if !strings.Contains(string(data), layout.want) {
				t.Fatalf("layout output %q does not contain %q", data, layout.want)
			}
		})
	}
}

func TestHostPort(t *testing.T) {
	for raw, want := range map[string]string{
		"https://example.test:8443/path": "example.test:8443",
		"not a url%":                     "not a url%",
		"/relative/path":                 "/relative/path",
	} {
		if got := hostPort(raw); got != want {
			t.Errorf("hostPort(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestBannerOptionalContentAndPlainLayout(t *testing.T) {
	started := time.Date(2026, 11, 26, 12, 30, 0, 0, time.UTC)
	cfg := BannerConfig{
		AppName: "  ", Version: "2.0.0", AppMode: "development", Debug: true,
		ListenURL: "https://example.test:8443", TorURL: "http://hidden.onion",
		I2PURL: "http://hidden.b32.i2p", SMTPAddress: "mail.example.test", SMTPPort: 2525,
		StartupTime: started, FirstRun: true,
	}
	lines := bannerContent(cfg, "Server", "development")
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"Tor:", "I2P:", "SMTP:", "Listening on", "Server started on", "Setup required"} {
		if !strings.Contains(joined, want) {
			t.Errorf("banner content missing %q: %s", want, joined)
		}
	}
	if name, mode := bannerValues(BannerConfig{}); name != "Server" || mode != "production" {
		t.Errorf("banner defaults = (%q, %q)", name, mode)
	}
	for _, tc := range []struct {
		mode  string
		debug bool
		want  string
	}{
		{"development", false, "🔧"}, {"production", true, "🐛"}, {"dev", false, "🔧"}, {"production", false, "🔒"},
	} {
		if got := modeIcon(tc.mode, tc.debug); got != tc.want {
			t.Errorf("modeIcon(%q, %t) = %q, want %q", tc.mode, tc.debug, got, tc.want)
		}
	}
	if art := asciiArt("Other"); len(art) != 1 || art[0] != "  Other" {
		t.Errorf("custom ascii art = %#v", art)
	}

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = write
	printStartupBannerPlain(cfg, "Server", "development")
	os.Stdout = previous
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	_ = read.Close()
	for _, want := range []string{"Tor: http://hidden.onion", "I2P: http://hidden.b32.i2p", "SMTP: mail.example.test:2525", "Setup:"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("plain banner missing %q: %s", want, data)
		}
	}
}

func TestMinimalAndMicroFirstRunLayouts(t *testing.T) {
	cfg := BannerConfig{AppName: "gitignore", Version: "1.2.3", AppMode: "production", FirstRun: true, ListenURL: "http://localhost:64080"}
	for _, tc := range []struct {
		name string
		run  func(BannerConfig)
		want string
	}{
		{"minimal", printStartupBannerMinimal, "[SETUP]"},
		{"micro", printStartupBannerMicro, "gitignore :64080 [SETUP]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read, write, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			previous := os.Stdout
			os.Stdout = write
			tc.run(cfg)
			os.Stdout = previous
			if err := write.Close(); err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(read)
			if err != nil {
				t.Fatal(err)
			}
			_ = read.Close()
			if !strings.Contains(string(data), tc.want) {
				t.Fatalf("output %q lacks %q", data, tc.want)
			}
		})
	}
}

func TestStartupLayoutDispatcherSelectsResponsiveLayout(t *testing.T) {
	cfg := BannerConfig{AppName: "gitignore", Version: "1.2.3", AppMode: "production", ListenURL: "http://localhost:8080", Plain: true}
	for _, tc := range []struct {
		name string
		mode terminal.SizeMode
		want string
	}{
		{"micro", terminal.SizeModeMicro, "gitignore :8080"},
		{"minimal", terminal.SizeModeMinimal, "gitignore 1.2.3"},
		{"compact", terminal.SizeModeCompact, "Listening:"},
		{"standard", terminal.SizeModeStandard, "Mode: production"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read, write, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			previous := os.Stdout
			os.Stdout = write
			renderStartupBanner(cfg, terminal.TerminalSize{Cols: 80, Rows: 24, Mode: tc.mode})
			os.Stdout = previous
			if err := write.Close(); err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(read)
			if err != nil {
				t.Fatal(err)
			}
			_ = read.Close()
			if !strings.Contains(string(data), tc.want) {
				t.Fatalf("layout output %q lacks %q", data, tc.want)
			}
		})
	}
}

func TestMicroLayoutWithoutURLUsesNameOnly(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = write
	printStartupBannerMicro(BannerConfig{AppName: "Server"})
	os.Stdout = previous
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	_ = read.Close()
	if string(data) != "Server\n" {
		t.Fatalf("micro banner = %q", data)
	}
}
