package app

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/common/audit"
	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/pgp"
	"github.com/apimgr/gitignore/src/service"
	"golang.org/x/term"
)

const (
	pgpPublicFilename  = "pgp.pub.asc"
	pgpPrivateFilename = "pgp.priv.asc.enc"
)

type pgpPublishRecord struct {
	Fingerprint string    `json:"fingerprint"`
	PublishedAt string    `json:"published_at,omitempty"`
	NextAttempt string    `json:"next_attempt,omitempty"`
	RetryAt     string    `json:"retry_at,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	Attempts    int       `json:"attempts,omitempty"`
	Permanent   bool      `json:"permanent,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type pgpKeyserverHTTPError struct {
	status int
}

func (e pgpKeyserverHTTPError) Error() string {
	return cliText("cli.pgp.http_status", "status", e.status)
}

func handlePGPMaintenance(configDir, configPath, dataDir, logsDir string, args []string) error {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return fmt.Errorf("%s: %w", cliText("cli.pgp.load_config_failed"), err)
	}
	if !service.IsElevated() && !authenticateSecurityMaintenance(cfg.Server.Token) {
		return errors.New(cliText("cli.pgp.operator_required"))
	}
	if err := db.Init(dataDir); err != nil {
		return fmt.Errorf("%s: %w", cliText("cli.pgp.database_init_failed"), err)
	}
	defer func() { _ = db.Close() }()
	if len(args) == 0 {
		return errors.New(cliText("cli.pgp.usage"))
	}
	dir := filepath.Join(configDir, "security")
	switch args[0] {
	case "generate":
		if len(args) != 1 {
			return errors.New(cliText("cli.pgp.usage_generate"))
		}
		return generateProjectPGP(cfg, dir, logsDir)
	case "rotate":
		if len(args) != 1 {
			return errors.New(cliText("cli.pgp.usage_rotate"))
		}
		return rotateProjectPGP(cfg, dir, logsDir)
	case "publish":
		if len(args) != 1 {
			return errors.New(cliText("cli.pgp.usage_publish"))
		}
		return publishProjectPGP(cfg, dir)
	case "export":
		return exportProjectPGP(cfg, dir, logsDir, args[1:])
	case "import":
		if len(args) != 2 {
			return errors.New(cliText("cli.pgp.usage_import"))
		}
		return importProjectPGP(cfg, dir, logsDir, args[1])
	case "delete":
		if len(args) != 1 {
			return errors.New(cliText("cli.pgp.usage_delete"))
		}
		return deleteProjectPGP(cfg, dir, logsDir)
	default:
		return errors.New(cliText("cli.pgp.usage"))
	}
}

func generateProjectPGP(cfg *config.ServerConfig, dir, logsDir string) error {
	if _, err := os.Stat(filepath.Join(dir, pgpPublicFilename)); err == nil {
		return errors.New(cliText("cli.pgp.keypair_exists"))
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	secret, err := db.InstallationSecret()
	if err != nil {
		return err
	}
	pair, err := pgp.Generate(projectPGPIdentity(cfg), secret)
	if err != nil {
		return err
	}
	fingerprint, err := pgp.Fingerprint(pair.PublicKey)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Second)
	metadata := db.PGPKeyMetadata{Fingerprint: fingerprint, CreatedAt: now.Format(time.RFC3339), ExpiresAt: now.AddDate(2, 0, 0).Format(time.RFC3339), KeyserversPublished: "[]"}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	rollback, err := installPGPFiles(dir, []byte(pair.PublicKey), pair.EncryptedPrivateKey)
	if err != nil {
		return err
	}
	if err := db.SavePGPKeyMetadata(metadata); err != nil {
		rollback()
		return err
	}
	if err := config.Update(func(current *config.ServerConfig) { current.Web.Security.PublishPGPKey = true }); err != nil {
		rollback()
		return err
	}
	if err := publishProjectPGP(cfg, dir); err != nil {
		fmt.Fprintln(os.Stderr, cliText("cli.pgp.publication_pending_generated", "error", err))
	}
	if err := appendPGPAudit(logsDir, "security.pgp_generated", fingerprint, ""); err != nil {
		return fmt.Errorf("%s: %w", cliText("cli.pgp.audit_failed_generated"), err)
	}
	fmt.Println(cliText("cli.pgp.generated", "fingerprint", fingerprint))
	return nil
}

func rotateProjectPGP(cfg *config.ServerConfig, dir, logsDir string) error {
	oldEncrypted, err := os.ReadFile(filepath.Join(dir, pgpPrivateFilename))
	if err != nil {
		return fmt.Errorf("%s: %w", cliText("cli.pgp.load_private_failed"), err)
	}
	secret, err := db.InstallationSecret()
	if err != nil {
		return err
	}
	_, err = pgp.UnprotectPrivateKey(oldEncrypted, secret)
	if err != nil {
		return err
	}
	pair, err := pgp.Generate(projectPGPIdentity(cfg), secret)
	if err != nil {
		return err
	}
	fingerprint, err := pgp.Fingerprint(pair.PublicKey)
	if err != nil {
		return err
	}
	signature, err := pgp.SignRotation(oldEncrypted, secret, pair.PublicKey)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Second)
	metadata := db.PGPKeyMetadata{
		Fingerprint: fingerprint, CreatedAt: now.Format(time.RFC3339),
		ExpiresAt:           now.AddDate(2, 0, 0).Format(time.RFC3339),
		LastRotatedAt:       sql.NullString{String: now.Format(time.RFC3339), Valid: true},
		KeyserversPublished: "[]",
	}
	oldPrivatePath := filepath.Join(dir, "pgp.priv.old.asc.enc")
	if err := atomicPGPWrite(oldPrivatePath, oldEncrypted, 0600); err != nil {
		return err
	}
	if err := db.RotatePGPReportBodies(metadata, func(body []byte) ([]byte, error) {
		plaintext, err := pgp.Decrypt(body, oldEncrypted, secret)
		if err != nil {
			return nil, err
		}
		return pgp.Encrypt(plaintext, pair.PublicKey)
	}, func() (func(), error) {
		rollback, err := installPGPFiles(dir, []byte(pair.PublicKey), pair.EncryptedPrivateKey)
		if err != nil {
			return nil, err
		}
		if err := atomicPGPWrite(filepath.Join(dir, "pgp.rotation.asc.sig"), signature, 0600); err != nil {
			rollback()
			return nil, err
		}
		return rollback, nil
	}); err != nil {
		_ = os.Remove(oldPrivatePath)
		return fmt.Errorf("%s: %w", cliText("cli.pgp.rotation_failed"), err)
	}
	if err := config.Update(func(current *config.ServerConfig) { current.Web.Security.PublishPGPKey = true }); err != nil {
		return err
	}
	if err := publishProjectPGP(cfg, dir); err != nil {
		fmt.Fprintln(os.Stderr, cliText("cli.pgp.publication_pending_rotated", "error", err))
	}
	if err := appendPGPAudit(logsDir, "security.pgp_rotated", fingerprint, ""); err != nil {
		return fmt.Errorf("%s: %w", cliText("cli.pgp.audit_failed_rotated"), err)
	}
	fmt.Println(cliText("cli.pgp.rotated", "fingerprint", fingerprint))
	return nil
}

func exportProjectPGP(cfg *config.ServerConfig, dir, logsDir string, args []string) error {
	if len(args) == 0 || args[0] == "public" {
		if len(args) > 2 || len(args) == 2 && args[0] != "public" {
			return errors.New(cliText("cli.pgp.usage_export"))
		}
		key, err := os.ReadFile(filepath.Join(dir, pgpPublicFilename))
		if err != nil {
			return err
		}
		if len(args) == 2 {
			return atomicPGPWrite(args[1], key, 0644)
		}
		_, err = os.Stdout.Write(key)
		return err
	}
	if args[0] != "private" || len(args) != 2 {
		return errors.New(cliText("cli.pgp.usage_export"))
	}
	reason, err := confirmPGPSensitive(cfg, cliText("cli.pgp.action.export_private"), true)
	if err != nil {
		return err
	}
	if err := enforcePGPExportRate(dir, cfg); err != nil {
		return err
	}
	encrypted, err := os.ReadFile(filepath.Join(dir, pgpPrivateFilename))
	if err != nil {
		return err
	}
	secret, err := db.InstallationSecret()
	if err != nil {
		return err
	}
	plaintext, err := pgp.UnprotectPrivateKey(encrypted, secret)
	if err != nil {
		return err
	}
	if err := atomicPGPWrite(args[1], plaintext, 0600); err != nil {
		return err
	}
	return appendPGPAudit(logsDir, "security.private_key_exported", "local", reason)
}

func importProjectPGP(cfg *config.ServerConfig, dir, logsDir, source string) error {
	reason, err := confirmPGPSensitive(cfg, cliText("cli.pgp.action.import_private"), true)
	if err != nil {
		return err
	}
	armored, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	entity, err := pgp.ParsePrivateKey(armored)
	if err != nil {
		return err
	}
	_, identity := entity.PrimaryIdentity(time.Now(), nil)
	if identity == nil || identity.UserId == nil {
		return errors.New(cliText("cli.pgp.invalid_key_identity"))
	}
	expectedIdentity := projectPGPIdentity(cfg)
	identitySeparator := strings.LastIndex(expectedIdentity, " <")
	expectedName := strings.TrimSpace(expectedIdentity[:identitySeparator])
	expectedEmail := strings.TrimSuffix(expectedIdentity[identitySeparator+2:], ">")
	if !strings.EqualFold(identity.UserId.Name, expectedName) || !strings.EqualFold(identity.UserId.Email, expectedEmail) {
		fmt.Fprintln(os.Stderr, cliText("cli.pgp.identity_mismatch_warning", "name", identity.UserId.Name, "email", identity.UserId.Email, "expected_name", expectedName, "expected_email", expectedEmail))
		if _, err := readPGPConfirmation(cliText("cli.pgp.identity_confirm_prompt"), "IMPORT"); err != nil {
			return err
		}
	}
	publicKey, err := pgp.PublicFromPrivate(armored)
	if err != nil {
		return err
	}
	fingerprint, err := pgp.Fingerprint(publicKey)
	if err != nil {
		return err
	}
	secret, err := db.InstallationSecret()
	if err != nil {
		return err
	}
	encrypted, err := pgp.ProtectPrivateKey(armored, secret)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Second)
	metadata := db.PGPKeyMetadata{
		Fingerprint: fingerprint, CreatedAt: now.Format(time.RFC3339),
		ExpiresAt:           entity.PrimaryKey.CreationTime.AddDate(2, 0, 0).Format(time.RFC3339),
		KeyserversPublished: "[]",
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	rollback, err := installPGPFiles(dir, []byte(publicKey), encrypted)
	if err != nil {
		return err
	}
	if err := db.SavePGPKeyMetadata(metadata); err != nil {
		rollback()
		return err
	}
	if err := config.Update(func(current *config.ServerConfig) { current.Web.Security.PublishPGPKey = true }); err != nil {
		rollback()
		return err
	}
	if err := publishProjectPGP(cfg, dir); err != nil {
		fmt.Fprintln(os.Stderr, cliText("cli.pgp.publication_pending_imported", "error", err))
	}
	return appendPGPAudit(logsDir, "security.pgp_imported", fingerprint, reason)
}

func deleteProjectPGP(cfg *config.ServerConfig, dir, logsDir string) error {
	fmt.Fprintln(os.Stderr, cliText("cli.pgp.delete_warning"))
	if _, err := confirmPGPSensitive(cfg, cliText("cli.pgp.action.delete_keypair"), false); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := db.RevokePGPKeyMetadata(); err != nil {
		return err
	}
	for _, name := range []string{pgpPublicFilename, pgpPrivateFilename, "pgp.priv.old.asc.enc", "pgp.rotation.asc.sig"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := config.Update(func(current *config.ServerConfig) { current.Web.Security.PublishPGPKey = false }); err != nil {
		return err
	}
	return appendPGPAudit(logsDir, "security.pgp_deleted", "", "")
}

func publishProjectPGP(cfg *config.ServerConfig, dir string) error {
	return publishProjectPGPContext(context.Background(), cfg, dir)
}

func publishProjectPGPContext(ctx context.Context, cfg *config.ServerConfig, dir string) error {
	return publishProjectPGPWithForce(ctx, cfg, dir, false)
}

func publishRestoredProjectPGP(cfg *config.ServerConfig, dir string) error {
	return publishProjectPGPWithForce(context.Background(), cfg, dir, true)
}

func publishProjectPGPWithForce(ctx context.Context, cfg *config.ServerConfig, dir string, force bool) error {
	return publishProjectPGPMode(ctx, cfg, dir, force, false)
}

func retryProjectPGP(ctx context.Context, cfg *config.ServerConfig, dir string) error {
	return publishProjectPGPMode(ctx, cfg, dir, false, true)
}

func publishProjectPGPMode(ctx context.Context, cfg *config.ServerConfig, dir string, force, retryOnly bool) error {
	publicKey, err := os.ReadFile(filepath.Join(dir, pgpPublicFilename))
	if err != nil {
		return err
	}
	fingerprint, err := pgp.Fingerprint(string(publicKey))
	if err != nil {
		return err
	}
	statePath := filepath.Join(dir, "keyservers.state")
	state := map[string]pgpPublishRecord{}
	if data, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(data, &state)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var failures []string
	for _, configured := range cfg.Web.Security.Keyservers {
		if err := ctx.Err(); err != nil {
			return err
		}
		endpoint, err := pgpKeyserverEndpoint(configured)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		record, known := state[endpoint]
		if retryOnly {
			if !known || record.Fingerprint != fingerprint || record.PublishedAt != "" || record.Permanent || record.Attempts >= 8 {
				continue
			}
			nextAttempt := record.NextAttempt
			if nextAttempt == "" {
				nextAttempt = record.RetryAt
			}
			retryAt, parseErr := time.Parse(time.RFC3339, nextAttempt)
			if parseErr != nil || time.Now().Before(retryAt) {
				continue
			}
		} else if known && record.Fingerprint == fingerprint {
			if record.PublishedAt != "" && !force {
				continue
			}
			if record.Permanent && !force || record.Attempts >= 8 && !force {
				continue
			}
			nextAttempt := record.NextAttempt
			if nextAttempt == "" {
				nextAttempt = record.RetryAt
			}
			if retryAt, parseErr := time.Parse(time.RFC3339, nextAttempt); !force && parseErr == nil && time.Now().Before(retryAt) {
				continue
			}
		}
		record = state[endpoint]
		if record.Fingerprint != fingerprint {
			record = pgpPublishRecord{}
		}
		record.Fingerprint = fingerprint
		record.UpdatedAt = time.Now().UTC()
		if err := submitPGPKey(ctx, endpoint, string(publicKey)); err != nil {
			record.LastError = err.Error()
			var statusErr pgpKeyserverHTTPError
			if errors.As(err, &statusErr) && statusErr.status != http.StatusTooManyRequests && statusErr.status < 500 {
				record.Permanent = true
				record.NextAttempt = ""
				record.RetryAt = ""
			} else {
				record.Attempts++
				if record.Attempts <= 8 {
					backoff := time.Duration(1<<min(record.Attempts-1, 8)) * time.Minute
					if backoff > 6*time.Hour {
						backoff = 6 * time.Hour
					}
					record.NextAttempt = time.Now().UTC().Add(backoff).Format(time.RFC3339)
				} else {
					record.NextAttempt = ""
				}
			}
			state[endpoint] = record
			failures = append(failures, endpoint+": "+err.Error())
			continue
		}
		record.PublishedAt = time.Now().UTC().Format(time.RFC3339)
		record.NextAttempt = ""
		record.RetryAt = ""
		record.LastError = ""
		record.Permanent = false
		state[endpoint] = record
	}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicPGPWrite(statePath, append(encoded, '\n'), 0600); err != nil {
		return err
	}
	if err := db.SetPGPKeyserversPublished(string(encoded)); err != nil {
		return err
	}
	if len(failures) != 0 {
		return fmt.Errorf("%s: %s", cliText("cli.pgp.keyserver_publication_failed"), strings.Join(failures, "; "))
	}
	return nil
}

func pgpKeyserverEndpoint(configured string) (string, error) {
	value := strings.TrimSpace(configured)
	if value == "" {
		return "", errors.New(cliText("cli.pgp.empty_keyserver"))
	}
	if !strings.Contains(value, "://") {
		switch strings.ToLower(value) {
		case "keys.openpgp.org":
			value = "https://keys.openpgp.org/vks/v1/upload"
		case "keyserver.ubuntu.com":
			value = "https://keyserver.ubuntu.com/pks/add"
		default:
			value = "https://" + value
		}
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", errors.New(cliText("cli.pgp.https_required", "url", configured))
	}
	if parsed.Path == "" || parsed.Path == "/" {
		switch strings.ToLower(parsed.Hostname()) {
		case "keys.openpgp.org":
			parsed.Path = "/vks/v1/upload"
		case "keyserver.ubuntu.com":
			parsed.Path = "/pks/add"
		default:
			return "", errors.New(cliText("cli.pgp.upload_path_required", "url", configured))
		}
	}
	return parsed.String(), nil
}

func submitPGPKey(ctx context.Context, endpoint, publicKey string) error {
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if request.URL.Scheme != "https" || len(via) >= 3 {
			return errors.New(cliText("cli.pgp.unsafe_redirect"))
		}
		return nil
	}}
	payload := url.Values{"keytext": {publicKey}}.Encode()
	contentType := "application/x-www-form-urlencoded"
	if parsed, parseErr := url.Parse(endpoint); parseErr == nil && strings.EqualFold(parsed.Hostname(), "keys.openpgp.org") {
		encoded, marshalErr := json.Marshal(map[string]string{"keytext": publicKey})
		if marshalErr != nil {
			return marshalErr
		}
		payload = string(encoded)
		contentType = "application/json"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept", "application/json, text/plain")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	closeErr := response.Body.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if response.StatusCode == http.StatusOK {
		if contentType == "application/json" {
			var result struct {
				Fingerprint string `json:"key_fpr"`
				Status      string `json:"status"`
			}
			if err := json.Unmarshal(body, &result); err != nil || result.Fingerprint == "" || result.Status == "" {
				return pgpKeyserverHTTPError{status: http.StatusBadRequest}
			}
		}
		return nil
	}
	return pgpKeyserverHTTPError{status: response.StatusCode}
}

func confirmPGPSensitive(cfg *config.ServerConfig, action string, requireReason bool) (string, error) {
	if (requireReason || !service.IsElevated()) && !authenticateSecurityMaintenance(cfg.Server.Token) {
		return "", errors.New(cliText("cli.pgp.operator_confirmation_failed"))
	}
	if _, err := readPGPConfirmation(cliText("cli.pgp.confirm_prompt", "action", action), "CONFIRM"); err != nil {
		return "", err
	}
	if !requireReason {
		return "", nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New(cliText("cli.pgp.terminal_reason_required"))
	}
	fmt.Fprint(os.Stderr, cliText("cli.pgp.reason_prompt"))
	reason, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	reason = strings.Join(strings.Fields(reason), " ")
	if reason == "" || len(reason) > 500 {
		return "", errors.New(cliText("cli.pgp.reason_length_invalid"))
	}
	return reason, nil
}

func readPGPConfirmation(prompt, expected string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New(cliText("cli.pgp.terminal_confirm_required"))
	}
	fmt.Fprint(os.Stderr, prompt)
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil || string(value) != expected {
		return "", errors.New(cliText("cli.pgp.confirmation_mismatch"))
	}
	return string(value), nil
}

func enforcePGPExportRate(dir string, cfg *config.ServerConfig) error {
	operator := "root"
	if !service.IsElevated() {
		digest := sha256.Sum256([]byte(cfg.Server.Token))
		operator = hex.EncodeToString(digest[:8])
	}
	path := filepath.Join(dir, "private-export-rate.json")
	previous := map[string]string{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &previous)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if timestamp, ok := previous[operator]; ok {
		last, err := time.Parse(time.RFC3339, timestamp)
		if err == nil && time.Since(last) < time.Hour {
			return errors.New(cliText("cli.pgp.export_rate_limited"))
		}
	}
	previous[operator] = time.Now().UTC().Format(time.RFC3339)
	encoded, err := json.Marshal(previous)
	if err != nil {
		return err
	}
	return atomicPGPWrite(path, encoded, 0600)
}

func appendPGPAudit(logsDir, event, fingerprint, reason string) error {
	var target *audit.Target
	if fingerprint != "" {
		target = &audit.Target{Type: "pgp_key", ID: fingerprint}
	}
	return audit.Append(logsDir, audit.Entry{
		Event: event, Category: "security", Severity: "info",
		Actor:  audit.Actor{Type: "operator", IP: localOperatorIP()},
		Target: target, Result: "success", Source: "cli", Reason: reason,
	})
}

func localOperatorIP() string {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, address := range addresses {
		ipNet, ok := address.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
			continue
		}
		return ipNet.IP.String()
	}
	return "127.0.0.1"
}

func projectPGPIdentity(cfg *config.ServerConfig) string {
	name := strings.TrimSpace(cfg.Server.Branding.Title)
	if name == "" {
		name = projectName
	}
	address := strings.TrimSpace(cfg.Server.Contact.Security.Email)
	if address == "" {
		address = strings.TrimSpace(cfg.Web.Security.Contact)
	}
	if address == "" {
		address = "security@" + cfg.Server.FQDN
	}
	return name + " Security <" + address + ">"
}

func installPGPFiles(dir string, publicKey, privateKey []byte) (func(), error) {
	publicPath := filepath.Join(dir, pgpPublicFilename)
	privatePath := filepath.Join(dir, pgpPrivateFilename)
	oldPublic, publicErr := os.ReadFile(publicPath)
	oldPrivate, privateErr := os.ReadFile(privatePath)
	hadPublic := publicErr == nil
	hadPrivate := privateErr == nil
	if publicErr != nil && !errors.Is(publicErr, os.ErrNotExist) {
		return nil, publicErr
	}
	if privateErr != nil && !errors.Is(privateErr, os.ErrNotExist) {
		return nil, privateErr
	}
	rollback := func() {
		if hadPublic {
			_ = atomicPGPWrite(publicPath, oldPublic, 0644)
		} else {
			_ = os.Remove(publicPath)
		}
		if hadPrivate {
			_ = atomicPGPWrite(privatePath, oldPrivate, 0600)
		} else {
			_ = os.Remove(privatePath)
		}
	}
	if err := atomicPGPWrite(publicPath, publicKey, 0644); err != nil {
		return nil, err
	}
	if err := atomicPGPWrite(privatePath, privateKey, 0600); err != nil {
		rollback()
		return nil, err
	}
	return rollback, nil
}

func atomicPGPWrite(path string, contents []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".pgp-update-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer func() { _ = os.Remove(temporary) }()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
