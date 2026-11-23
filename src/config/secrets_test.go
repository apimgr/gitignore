package config

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRecordsGeneratedSecretAuditEvents(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "logs")
	SetAuditLogDir(logsDir)
	t.Cleanup(func() { SetAuditLogDir("") })
	if _, err := Load(filepath.Join(dir, "server.yml")); err != nil {
		t.Fatalf("Load: %v", err)
	}
	file, err := os.Open(filepath.Join(logsDir, "audit.log"))
	if err != nil {
		t.Fatalf("open audit.log: %v", err)
	}
	defer file.Close()
	var events []struct {
		ID       string `json:"id"`
		Time     string `json:"time"`
		Event    string `json:"event"`
		Category string `json:"category"`
		Severity string `json:"severity"`
		Actor    struct {
			Type string `json:"type"`
			IP   string `json:"ip"`
		} `json:"actor"`
		Target struct {
			ID string `json:"id"`
		} `json:"target"`
		Result string `json:"result"`
	}
	decoder := json.NewDecoder(file)
	for {
		var event struct {
			ID       string `json:"id"`
			Time     string `json:"time"`
			Event    string `json:"event"`
			Category string `json:"category"`
			Severity string `json:"severity"`
			Actor    struct {
				Type string `json:"type"`
				IP   string `json:"ip"`
			} `json:"actor"`
			Target struct {
				ID string `json:"id"`
			} `json:"target"`
			Result string `json:"result"`
		}
		err := decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode audit event: %v", err)
		}
		events = append(events, event)
	}
	want := map[string]bool{
		"server.token":                          false,
		"server.security.encryption_key":        false,
		"server.metrics.auth.tokens.prometheus": false,
		"server.metrics.auth.tokens.grafana":    false,
		"server.metrics.auth.tokens.loki":       false,
	}
	for _, event := range events {
		if event.Event != "config.secret_generated" || event.Target.ID == "" {
			t.Errorf("unexpected secret audit event: %#v", event)
			continue
		}
		if len(event.ID) != len("audit_")+26 || event.Time == "" || event.Category != "config" || event.Severity != "info" || event.Actor.Type != "system" || event.Actor.IP == "" || event.Result != "success" {
			t.Errorf("secret audit event is missing required AI.md fields: %#v", event)
		}
		if _, ok := want[event.Target.ID]; !ok {
			t.Errorf("unexpected generated secret audit key %q", event.Target.ID)
			continue
		}
		want[event.Target.ID] = true
	}
	for key, recorded := range want {
		if !recorded {
			t.Errorf("audit.log is missing generated secret event for %s", key)
		}
	}
	info, err := os.Stat(filepath.Join(logsDir, "audit.log"))
	if err != nil {
		t.Fatalf("stat audit.log: %v", err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatalf("audit.log mode = %04o, want 0640", info.Mode().Perm())
	}
}

func TestLoadStoresEncryptionKeyOnlyInSecretsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	serverYAML, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read server.yml: %v", err)
	}
	if strings.Contains(string(serverYAML), cfg.Server.Security.EncryptionKey) || strings.Contains(string(serverYAML), "encryption_key:") || strings.Contains(string(serverYAML), cfg.Server.Token) {
		t.Fatal("server.yml contains a secret")
	}
	secretsPath := filepath.Join(dir, "secrets.yml")
	secrets, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatalf("read secrets.yml: %v", err)
	}
	if !strings.Contains(string(secrets), cfg.Server.Security.EncryptionKey) {
		t.Fatal("secrets.yml does not contain the configured encryption key")
	}
	if len(cfg.Server.Token) != 36 || !strings.HasPrefix(cfg.Server.Token, "adm_") {
		t.Fatalf("generated operator token has invalid format: %q", cfg.Server.Token)
	}
	if !strings.Contains(string(secrets), cfg.Server.Token) {
		t.Fatal("secrets.yml does not contain the operator token")
	}
	for name, token := range map[string]string{
		"prometheus": cfg.Server.Metrics.Auth.Tokens.Prometheus,
		"grafana":    cfg.Server.Metrics.Auth.Tokens.Grafana,
		"loki":       cfg.Server.Metrics.Auth.Tokens.Loki,
	} {
		if len(token) != 36 || !strings.HasPrefix(token, "mtr_") {
			t.Fatalf("generated %s metrics token has invalid format: %q", name, token)
		}
		if !strings.Contains(string(secrets), token) {
			t.Errorf("secrets.yml does not contain the %s metrics token", name)
		}
	}
	info, err := os.Stat(secretsPath)
	if err != nil {
		t.Fatalf("stat secrets.yml: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("secrets.yml mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestLoadMigratesLegacyEncryptionKeyAndPreservesOtherSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yml")
	legacy := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if err := os.WriteFile(path, []byte("server:\n  port: \"64180\"\n  security:\n    encryption_key: "+legacy+"\n"), 0600); err != nil {
		t.Fatalf("write legacy server.yml: %v", err)
	}
	secretsPath := filepath.Join(dir, "secrets.yml")
	if err := os.WriteFile(secretsPath, []byte("server:\n  token: adm_test\n"), 0600); err != nil {
		t.Fatalf("write existing secrets.yml: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Security.EncryptionKey != legacy {
		t.Fatal("legacy encryption key was not migrated")
	}
	serverYAML, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migrated server.yml: %v", err)
	}
	if strings.Contains(string(serverYAML), "encryption_key:") {
		t.Fatal("migrated server.yml still contains the encryption key")
	}
	secrets, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatalf("read migrated secrets.yml: %v", err)
	}
	if !strings.Contains(string(secrets), "token: adm_test") || !strings.Contains(string(secrets), legacy) {
		t.Fatal("secrets migration lost an existing secret or omitted the encryption key")
	}
}

func TestLoadReplacesInvalidEncryptionKeyWithoutFailing(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "server.yml")
	secretsPath := filepath.Join(dir, "secrets.yml")
	if err := os.WriteFile(configPath, []byte("server:\n  port: \"64180\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretsPath, []byte("server:\n  token: adm_test\n  security:\n    encryption_key: invalid-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() rejected an invalid encryption key: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(cfg.Server.Security.EncryptionKey)
	if err != nil || len(decoded) != 32 {
		t.Fatalf("replacement key is not a base64 32-byte key: %v", err)
	}
	if cfg.Server.Security.EncryptionKey == "invalid-key" {
		t.Fatal("invalid encryption key was retained")
	}
	if location := configValueLocations["server.security.encryption_key"]; location.file != "secrets.yml" || location.line != 4 {
		t.Fatalf("invalid key location = %#v, want secrets.yml:4", location)
	}
	data, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "encryption_key_version: 1") || !strings.Contains(string(data), cfg.Server.Security.EncryptionKey) {
		t.Fatalf("replacement key/version were not persisted in secrets.yml: %s", data)
	}
}

func TestMalformedSecretsFileUsesLastGoodValuesWithoutOverwrite(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "server.yml")
	secretsPath := filepath.Join(dir, "secrets.yml")
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	valid := "server:\n  token: adm_previous\n  security:\n    encryption_key: " + key + "\n"
	if err := os.WriteFile(secretsPath, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSecrets(secretsPath); err != nil {
		t.Fatalf("read initial secrets: %v", err)
	}
	malformed := "server: [invalid\n"
	if err := os.WriteFile(secretsPath, []byte(malformed), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	if _, err := loadSensitiveSecrets(cfg, configPath); err != nil {
		t.Fatalf("loadSensitiveSecrets rejected malformed file: %v", err)
	}
	if cfg.Server.Token != "adm_previous" || cfg.Server.Security.EncryptionKey != key {
		t.Fatal("last-good secret values were not retained")
	}
	data, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != malformed {
		t.Fatalf("malformed secrets file was overwritten: %q", data)
	}
}

func TestLoadSecretsConfigOverridesEarlierValuesAndSecuresPermissions(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "server.yml")
	secretsPath := filepath.Join(dir, "secrets.yml")
	if err := os.WriteFile(secretsPath, []byte("server:\n  cache:\n    password: protected-cache\n  notifications:\n    email:\n      smtp:\n        password: protected-mail\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Server.Cache.Password = "ordinary-cache"
	cfg.Server.Notifications.Email.SMTP.Password = "ordinary-mail"
	if err := loadSecretsConfig(configPath, cfg); err != nil {
		t.Fatalf("loadSecretsConfig: %v", err)
	}
	if cfg.Server.Cache.Password != "protected-cache" {
		t.Fatalf("cache password = %q, want secrets.yml value", cfg.Server.Cache.Password)
	}
	if cfg.Server.Notifications.Email.SMTP.Password != "protected-mail" {
		t.Fatalf("SMTP password = %q, want secrets.yml value", cfg.Server.Notifications.Email.SMTP.Password)
	}
	info, err := os.Stat(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("secrets.yml mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestBackupEncryptionPasswordLoadsAndPersistsOnlyInSecrets(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "server.yml")
	secretsPath := filepath.Join(dir, "secrets.yml")
	password := "configured backup password"
	if err := os.WriteFile(configPath, []byte("server: {}\n"), 0600); err != nil {
		t.Fatalf("write server.yml: %v", err)
	}
	if err := os.WriteFile(secretsPath, []byte("server:\n  backup:\n    encryption_password: "+password+"\n"), 0600); err != nil {
		t.Fatalf("write secrets.yml: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Backup.EncryptionPassword != password || !cfg.Server.Backup.Encryption.Enabled {
		t.Fatal("backup encryption password was not loaded and enabled")
	}

	cfg.Server.Backup.EncryptionPassword = "updated backup password"
	if err := saveConfig(cfg, configPath); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}
	serverYAML, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read server.yml: %v", err)
	}
	if strings.Contains(string(serverYAML), "encryption_password") || strings.Contains(string(serverYAML), "updated backup password") {
		t.Fatal("server.yml contains the backup encryption password")
	}
	secrets, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatalf("read secrets.yml: %v", err)
	}
	if !strings.Contains(string(secrets), "encryption_password: updated backup password") {
		t.Fatal("updated backup encryption password was not stored in secrets.yml")
	}
	info, err := os.Stat(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("secrets.yml mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestLoadSensitiveSecretsGeneratesAndPersistsWebhookSecret(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "server.yml")
	cfg := DefaultConfig()
	cfg.Server.Contact.Admin.Webhooks = map[string]string{"generic": "https://hooks.example.test/events"}
	if _, err := loadSensitiveSecrets(cfg, configPath); err != nil {
		t.Fatalf("loadSensitiveSecrets: %v", err)
	}
	secret := cfg.Server.Contact.Admin.Webhooks["generic_secret"]
	if secret == "" {
		t.Fatal("webhook signing secret was not generated")
	}
	data, err := os.ReadFile(filepath.Join(dir, "secrets.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "generic_secret: "+secret) || !strings.Contains(string(data), "https://hooks.example.test/events") {
		t.Fatal("webhook URL and signing secret were not persisted in secrets.yml")
	}
}
