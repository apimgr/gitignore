package banner

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/common/terminal"
)

// BannerConfig contains the public startup details.
type BannerConfig struct {
	AppName     string
	Version     string
	AppMode     string
	Debug       bool
	URLs        []string
	ListenURL   string
	TorURL      string
	I2PURL      string
	SMTPAddress string
	SMTPPort    int
	StartupTime time.Time
	FirstRun    bool
	Plain       bool
}

// Config preserves the original call shape for existing package users.
type Config struct {
	AppName string
	Version string
	Mode    string
	URLs    []string
}

func hostPort(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}
	return parsed.Host
}

// PrintStartup adapts the legacy config to the shared banner renderer.
func PrintStartup(cfg Config) {
	listenURL := ""
	if len(cfg.URLs) > 0 {
		listenURL = cfg.URLs[0]
	}
	PrintStartupBanner(BannerConfig{AppName: cfg.AppName, Version: cfg.Version, AppMode: cfg.Mode, URLs: cfg.URLs, ListenURL: listenURL})
}

// PrintStartupBanner selects a layout using the shared terminal breakpoints.
func PrintStartupBanner(cfg BannerConfig) {
	renderStartupBanner(cfg, terminal.GetTerminalSize())
}

func renderStartupBanner(cfg BannerConfig, size terminal.TerminalSize) {
	switch {
	case size.Mode >= terminal.SizeModeStandard:
		printStartupBannerFull(cfg, size)
	case size.Mode >= terminal.SizeModeCompact:
		printStartupBannerCompact(cfg)
	case size.Mode >= terminal.SizeModeMinimal:
		printStartupBannerMinimal(cfg)
	default:
		printStartupBannerMicro(cfg)
	}
}

func printStartupBannerFull(cfg BannerConfig, size terminal.TerminalSize) {
	name, mode := bannerValues(cfg)
	if cfg.Plain || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		printStartupBannerPlain(cfg, name, mode)
		return
	}
	content := bannerContent(cfg, name, mode)
	for _, line := range asciiArt(name) {
		fmt.Println(line)
	}
	fmt.Println()
	longest := 0
	for _, line := range content {
		if len(line) > longest {
			longest = len(line)
		}
	}
	if longest > size.Cols-4 {
		for _, line := range content {
			fmt.Println(line)
		}
		return
	}
	border := "+" + strings.Repeat("-", longest+2) + "+"
	fmt.Println(border)
	for _, line := range content {
		fmt.Printf("| %-*s |\n", longest, line)
	}
	fmt.Println(border)
}

func printStartupBannerCompact(cfg BannerConfig) {
	name, mode := bannerValues(cfg)
	if cfg.Plain || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		printStartupBannerPlain(cfg, name, mode)
		return
	}
	for _, line := range bannerContent(cfg, name, mode) {
		fmt.Println(line)
	}
}

func printStartupBannerMinimal(cfg BannerConfig) {
	name, mode := bannerValues(cfg)
	fmt.Printf("%s %s\n%s\n", name, cfg.Version, mode)
	if cfg.ListenURL != "" {
		fmt.Println(hostPort(cfg.ListenURL))
	}
	if cfg.FirstRun {
		fmt.Println("[SETUP]")
	}
}

func printStartupBannerMicro(cfg BannerConfig) {
	name, _ := bannerValues(cfg)
	port := ""
	if cfg.ListenURL != "" {
		if parsed, err := url.Parse(cfg.ListenURL); err == nil {
			if _, p, splitErr := net.SplitHostPort(parsed.Host); splitErr == nil {
				port = ":" + p
			}
		}
	}
	if cfg.FirstRun {
		fmt.Printf("%s %s [SETUP]\n", name, port)
		return
	}
	if port == "" {
		fmt.Println(name)
		return
	}
	fmt.Printf("%s %s\n", name, port)
}

func bannerValues(cfg BannerConfig) (string, string) {
	name := strings.TrimSpace(cfg.AppName)
	if name == "" {
		name = "Server"
	}
	mode := strings.TrimSpace(cfg.AppMode)
	if mode == "" {
		mode = "production"
	}
	return name, mode
}

func modeIcon(mode string, debug bool) string {
	if debug {
		return "🐛"
	}
	if strings.Contains(strings.ToLower(mode), "development") || strings.EqualFold(mode, "dev") {
		return "🔧"
	}
	return "🔒"
}

func printStartupBannerPlain(cfg BannerConfig, name, mode string) {
	fmt.Printf("%s v%s\nMode: %s\n", name, cfg.Version, mode)
	if cfg.ListenURL != "" {
		fmt.Printf("Listening: %s\n", cfg.ListenURL)
	}
	if cfg.TorURL != "" {
		fmt.Printf("Tor: %s\n", cfg.TorURL)
	}
	if cfg.I2PURL != "" {
		fmt.Printf("I2P: %s\n", cfg.I2PURL)
	}
	if cfg.SMTPAddress != "" {
		fmt.Printf("SMTP: %s:%d\n", cfg.SMTPAddress, cfg.SMTPPort)
	}
	if !cfg.StartupTime.IsZero() {
		fmt.Printf("Started: %s\n", cfg.StartupTime.Format("January 02, 2006 at 15:04:05 MST"))
	}
	if cfg.FirstRun {
		fmt.Println("Setup: edit server.yml before or after starting the server")
	}
	fmt.Println()
}

func bannerContent(cfg BannerConfig, name, mode string) []string {
	content := []string{"🚀 " + name + " · 📦 " + cfg.Version, modeIcon(mode, cfg.Debug) + " Running in mode: " + mode}
	if cfg.TorURL != "" {
		content = append(content, "🧅 Tor: "+cfg.TorURL)
	}
	if cfg.I2PURL != "" {
		content = append(content, "🔗 I2P: "+cfg.I2PURL)
	}
	if cfg.SMTPAddress != "" {
		content = append(content, fmt.Sprintf("📧 SMTP: %s:%d", cfg.SMTPAddress, cfg.SMTPPort))
	}
	if cfg.ListenURL != "" {
		content = append(content, "📡 Listening on "+cfg.ListenURL)
	}
	if !cfg.StartupTime.IsZero() {
		content = append(content, "✅ Server started on "+cfg.StartupTime.Format("January 02, 2006 at 15:04:05 MST"))
	}
	if cfg.FirstRun {
		content = append(content, "⚠️ Setup required: edit server.yml before or after starting the server")
	}
	return content
}

func asciiArt(name string) []string {
	if strings.EqualFold(name, "gitignore") {
		return []string{
			"  ____ ___ _____ ___ ____ _   _  ___  ____  _____",
			" / ___|_ _|_   _|_ _/ ___| | | |/ _ \\/ ___|| ____|",
			"| |  _ | |  | |  | | |  _| |_| | | | \\___ \\|  _|",
			"| |_| || |  | |  | | |_| |  _  | |_| |___) | |___",
			" \\____|___| |_| |___\\____|_| |_|\\___/|____/|_____|",
		}
	}
	return []string{"  " + name}
}
