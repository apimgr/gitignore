// Package update checks and installs CLI builds published by the configured server.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/client/api"
	clipath "github.com/apimgr/gitignore/src/client/path"
	"github.com/apimgr/gitignore/src/common/urlutil"
	serverpath "github.com/apimgr/gitignore/src/path"
	"github.com/apimgr/gitignore/src/updater"
)

const (
	binaryBase    = "gitignore-cli"
	maxBinarySize = 150 << 20
)

var cliLogFile = clipath.LogFile

// Result describes the outcome of an update check.
type Result struct {
	Available bool
	Forced    bool
	Version   string
	Installed bool
}

// Check checks the configured server and optionally installs its published CLI.
// action is empty for automatic startup behavior, "check" for a read-only
// check, and "yes" to explicitly install an available update.
func Check(ctx context.Context, client *api.Client, currentVersion, action string, automatic, interactive bool, input io.Reader, output io.Writer) (Result, error) {
	if action != "" && action != "check" && action != "yes" {
		return Result{}, fmt.Errorf("invalid --update value %q; use check or yes", action)
	}
	discovery, err := client.Discover()
	if err != nil {
		return Result{}, err
	}
	platform := runtime.GOOS + "-" + runtime.GOARCH
	published, available := discovery.CLIVersions[platform]
	result := Result{Available: available && updater.IsNewerVersion(published.Version, currentVersion)}
	if result.Available {
		result.Version = published.Version
	}
	result.Forced = updater.IsNewerVersion(discovery.CLIMinVersion, currentVersion)
	if result.Forced && result.Available && updater.IsNewerVersion(discovery.CLIMinVersion, published.Version) {
		return result, fmt.Errorf("the server's published CLI %s is below its required minimum %s", published.Version, discovery.CLIMinVersion)
	}
	if result.Forced && action == "check" {
		logEvent("cli.update_forced", "current", currentVersion, "minimum", discovery.CLIMinVersion, "os_arch", platform)
		return result, fmt.Errorf("this CLI is too old; the server requires %s — run 'gitignore-cli --update yes' to upgrade", discovery.CLIMinVersion)
	}
	if !result.Available {
		if result.Forced {
			logEvent("cli.update_forced", "current", currentVersion, "minimum", discovery.CLIMinVersion, "os_arch", platform)
			return result, fmt.Errorf("this CLI is too old; the server requires %s — run 'gitignore-cli --update yes' to upgrade", discovery.CLIMinVersion)
		}
		return result, nil
	}
	if action == "check" {
		return result, nil
	}
	install := action == "yes" || (!interactive && automatic)
	if !install && interactive {
		fmt.Fprintf(output, "CLI update %s is available. Install it now? [y/N] ", published.Version)
		answer, readErr := bufio.NewReader(input).ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return result, readErr
		}
		install = strings.EqualFold(strings.TrimSpace(answer), "y") || strings.EqualFold(strings.TrimSpace(answer), "yes")
	}
	if !install {
		if result.Forced {
			return result, fmt.Errorf("this CLI is too old; run 'gitignore-cli --update yes' to upgrade to %s", discovery.CLIMinVersion)
		}
		return result, nil
	}
	if published.SHA256 == "" || !validSHA256(published.SHA256) {
		return result, errors.New("the server published an invalid CLI checksum")
	}
	if err := installBinary(ctx, client, platform, currentVersion, published); err != nil {
		if errors.Is(err, os.ErrPermission) {
			exe, _ := os.Executable()
			return result, fmt.Errorf("you do not have permission to update %s; ask your admin or move the binary to a writable path", exe)
		}
		return result, err
	}
	result.Installed = true
	return result, nil
}

func installBinary(ctx context.Context, client *api.Client, platform, currentVersion string, published api.BinaryVersion) error {
	serverURL, err := url.Parse(client.BaseURL)
	if err != nil || serverURL.Scheme != "https" || serverURL.Host == "" {
		return errors.New("CLI updates require an HTTPS server URL")
	}
	base := filepath.Join(os.TempDir(), serverpath.OrgName)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return err
	}
	tempDir, err := os.MkdirTemp(base, serverpath.ProjectName+"-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()
	filename := binaryBase + "-" + platform
	if runtime.GOOS == "windows" {
		filename += ".exe"
	}
	downloadURL := urlutil.BuildAPIURL(client.BaseURL, "/cli/binaries/{name}", map[string]string{"name": filename}, nil)
	parsedDownloadURL, err := url.Parse(downloadURL)
	if err != nil || parsedDownloadURL.Scheme != "https" || parsedDownloadURL.Host != serverURL.Host {
		return errors.New("CLI update URL must use the configured HTTPS server")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", api.UserAgent())
	req.Header.Set("Accept", "application/octet-stream")
	if client.Token != "" {
		req.Header.Set("Authorization", "Bearer "+client.Token)
	}
	updateHTTP := *client.HTTPClient
	previousRedirect := updateHTTP.CheckRedirect
	updateHTTP.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if request.URL.Scheme != "https" || request.URL.Host != serverURL.Host {
			return errors.New("CLI update redirect left the configured HTTPS server")
		}
		if previousRedirect != nil {
			return previousRedirect(request, via)
		}
		if len(via) >= 10 {
			return errors.New("too many CLI update redirects")
		}
		return nil
	}
	response, err := updateHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("download CLI update: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download CLI update: server returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxBinarySize {
		return fmt.Errorf("downloaded CLI binary is too large: %d bytes", response.ContentLength)
	}
	tempBinary := filepath.Join(tempDir, "cli.update.tmp")
	file, err := os.OpenFile(tempBinary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(response.Body, maxBinarySize+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written == 0 || written > maxBinarySize {
		return fmt.Errorf("downloaded CLI binary has invalid size: %d bytes", written)
	}
	if err := updater.VerifyChecksum(tempBinary, published.SHA256); err != nil {
		logEvent("cli.update_checksum_invalid", "version", published.Version, "os_arch", platform)
		return err
	}
	currentBinary, err := os.Executable()
	if err != nil {
		return err
	}
	currentBinary, err = filepath.EvalSymlinks(currentBinary)
	if err != nil {
		return err
	}
	if err := updater.ReplaceBinary(currentBinary, tempBinary); err != nil {
		return err
	}
	if err := os.RemoveAll(tempDir); err != nil {
		return err
	}
	logEvent("cli.update_completed", "old_version", currentVersion, "new_version", published.Version, "os_arch", platform)
	return updater.RestartSelf()
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	return err == nil && len(decoded) == sha256.Size
}

func logEvent(event string, fields ...string) {
	entry := time.Now().UTC().Format(time.RFC3339Nano) + " event=" + event
	for i := 0; i+1 < len(fields); i += 2 {
		entry += " " + fields[i] + "=" + strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\r", "\\r", "\"", "\\\"").Replace(fields[i+1])
	}
	entry += "\n"
	if file, err := os.OpenFile(cliLogFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not open CLI audit log: %v\n", err)
	} else {
		if err := file.Chmod(0o600); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not secure CLI audit log: %v\n", err)
		}
		if _, err := io.WriteString(file, entry); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not write CLI audit log: %v\n", err)
		}
		if err := file.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not close CLI audit log: %v\n", err)
		}
	}
	_, _ = fmt.Fprint(os.Stderr, entry)
}
