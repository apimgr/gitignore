package config

import (
	"crypto/aes"
	"crypto/cipher"
	cryptorand "crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/apimgr/gitignore/src/common/audit"
	"gopkg.in/yaml.v3"
)

var configAuditLog struct {
	sync.RWMutex
	dir string
}

// SetAuditLogDir sets the directory used for configuration audit events.
func SetAuditLogDir(dir string) {
	configAuditLog.Lock()
	configAuditLog.dir = dir
	configAuditLog.Unlock()
}

// AuditLogDir returns the configured audit log directory.
func AuditLogDir() string {
	configAuditLog.RLock()
	defer configAuditLog.RUnlock()
	return configAuditLog.dir
}

func writeGeneratedSecretAudit(key string) {
	configAuditLog.RLock()
	dir := configAuditLog.dir
	configAuditLog.RUnlock()
	if strings.TrimSpace(dir) == "" {
		return
	}
	if err := audit.Append(dir, audit.Entry{
		Event: "config.secret_generated", Category: "config", Severity: "info",
		Actor:  audit.Actor{Type: "system"},
		Target: &audit.Target{Type: "config", ID: key}, Result: "success",
	}); err != nil {
		fmt.Fprintln(os.Stderr, configText("cli.config.secret_audit_write_warning", "key", key))
	}
}

// loadSecretsConfig applies the protected configuration file after every
// ordinary configuration source so its values have the required precedence.
func loadSecretsConfig(configPath string, cfg *ServerConfig) error {
	secretsPath := filepath.Join(filepath.Dir(configPath), "secrets.yml")
	secrets, err := readSecrets(secretsPath)
	if err != nil {
		return err
	}
	if data, readErr := os.ReadFile(secretsPath); readErr == nil && !invalidConfigFiles[configFileCacheKey(secretsPath)] {
		return applyYAMLConfigFile(secretsPath, data, cfg)
	}
	data, err := yaml.Marshal(secrets)
	if err != nil {
		return fmt.Errorf("%s: %w", configText("cli.config.secrets_encode_configuration_failed"), err)
	}
	return applyYAMLConfigFile(secretsPath, data, cfg)
}

// loadSensitiveSecrets reads protected values, migrates a legacy encryption
// key from server.yml, and generates required secrets when they are absent.
func loadSensitiveSecrets(cfg *ServerConfig, configPath string) (bool, error) {
	secretsPath := filepath.Join(filepath.Dir(configPath), "secrets.yml")
	secrets, err := readSecrets(secretsPath)
	if err != nil {
		return false, err
	}
	server := nestedSecretMap(secrets, "server")
	secretsChanged := false
	generatedKeys := make([]string, 0, 5)
	token, _ := server["token"].(string)
	token = expandEnvironmentValue(token)
	if token == "" {
		token = cfg.Server.Token
		if token == "" {
			token, err = generateOperatorToken()
			if err != nil {
				return false, fmt.Errorf("%s: %w", configText("cli.config.secret_generate_failed", "name", "operator token"), err)
			}
			generatedKeys = append(generatedKeys, "server.token")
		}
		server["token"] = token
		secrets["server"] = server
		secretsChanged = true
	}
	cfg.Server.Token = token
	security := nestedSecretMap(server, "security")
	secretKey, _ := security["encryption_key"].(string)
	secretKey = expandEnvironmentValue(secretKey)
	needsConfigSave := secretKey == ""
	encryptionKeyGenerated := false
	if secretKey != "" {
		cfg.Server.Security.EncryptionKey = secretKey
	}
	if strings.TrimSpace(cfg.Server.Security.EncryptionKey) == "" {
		if err := ensureEncryptionKey(cfg); err != nil {
			return false, err
		}
		encryptionKeyGenerated = true
	}
	if err := validateEncryptionKey(cfg.Server.Security.EncryptionKey); err != nil {
		cfg.Server.Security.EncryptionKey = ""
		if err := ensureEncryptionKey(cfg); err != nil {
			return false, err
		}
		secretKey = cfg.Server.Security.EncryptionKey
		security["encryption_key"] = secretKey
		security["encryption_key_version"] = 1
		server["security"] = security
		secrets["server"] = server
		secretsChanged = true
		warnInvalidConfigKey("server.security.encryption_key")
		generatedKeys = append(generatedKeys, "server.security.encryption_key")
	}
	if secretKey == "" {
		security["encryption_key"] = cfg.Server.Security.EncryptionKey
		security["encryption_key_version"] = 1
		server["security"] = security
		secrets["server"] = server
		secretsChanged = true
		if encryptionKeyGenerated {
			generatedKeys = append(generatedKeys, "server.security.encryption_key")
		}
	}
	backup := nestedSecretMap(server, "backup")
	backupPassword, _ := backup["encryption_password"].(string)
	if location, ok := configValueLocations["server.backup.encryption_password"]; ok && location.file == filepath.Base(configPath) {
		needsConfigSave = true
	}
	if backupPassword == "" {
		backupPassword = cfg.Server.Backup.EncryptionPassword
		if backupPassword != "" {
			needsConfigSave = true
		}
	}
	if strings.HasPrefix(backupPassword, "enc:v1:") {
		if plaintext, decryptErr := decryptDNSCredential(cfg.Server.Security.EncryptionKey, backupPassword); decryptErr == nil {
			cfg.Server.Backup.EncryptionPassword = plaintext
		} else {
			// Keep unreadable ciphertext marked so backup creation can fail closed.
			cfg.Server.Backup.EncryptionPassword = backupPassword
		}
	} else if backupPassword != "" {
		encrypted, encryptErr := encryptDNSCredential(cfg.Server.Security.EncryptionKey, backupPassword)
		if encryptErr != nil {
			return false, fmt.Errorf("cannot protect backup encryption password: %w", encryptErr)
		}
		backup["encryption_password"] = encrypted
		secretsChanged = true
		cfg.Server.Backup.EncryptionPassword = backupPassword
	}
	if backupPassword != "" {
		server["backup"] = backup
		secrets["server"] = server
	}
	dnsConfig := cfg.Server.SSL.LetsEncrypt
	credentialNode := nestedSecretMap(nestedSecretMap(nestedSecretMap(server, "ssl"), "letsencrypt"), "dns_credentials")
	decryptedCredentials := make(map[string]string, len(credentialNode))
	for name, rawValue := range credentialNode {
		value, ok := rawValue.(string)
		if !ok || strings.TrimSpace(name) == "" {
			return false, fmt.Errorf("invalid DNS credential entry %q", name)
		}
		if strings.HasPrefix(value, "enc:v1:") {
			value, err = decryptDNSCredential(cfg.Server.Security.EncryptionKey, value)
			if err != nil {
				return false, fmt.Errorf("cannot decrypt DNS credential %s", name)
			}
		} else if value != "" {
			encryptedValue, encryptErr := encryptDNSCredential(cfg.Server.Security.EncryptionKey, value)
			if encryptErr != nil {
				return false, fmt.Errorf("cannot protect DNS credential %s: %w", name, encryptErr)
			}
			credentialNode[name] = encryptedValue
			secretsChanged = true
		}
		decryptedCredentials[name] = value
	}
	dnsConfig.DNSCredentials = decryptedCredentials
	cfg.Server.SSL.LetsEncrypt = dnsConfig
	letsencrypt := nestedSecretMap(nestedSecretMap(server, "ssl"), "letsencrypt")
	letsencrypt["dns_credentials"] = credentialNode
	serverSSL := nestedSecretMap(server, "ssl")
	serverSSL["letsencrypt"] = letsencrypt
	server["ssl"] = serverSSL
	secrets["server"] = server
	metrics := nestedSecretMap(server, "metrics")
	metricsAuth := nestedSecretMap(metrics, "auth")
	metricTokens := nestedSecretMap(metricsAuth, "tokens")
	metricRuntimeValues := make(map[string]string, 3)
	for _, name := range []string{"prometheus", "grafana", "loki"} {
		value, _ := metricTokens[name].(string)
		value = expandEnvironmentValue(value)
		if value != "" {
			metricRuntimeValues[name] = value
			continue
		}
		value, err = generatePrefixedToken("mtr_")
		if err != nil {
			return false, fmt.Errorf("%s: %w", configText("cli.config.secret_generate_failed", "name", name+" metrics token"), err)
		}
		metricTokens[name] = value
		metricRuntimeValues[name] = value
		secretsChanged = true
		generatedKeys = append(generatedKeys, "server.metrics.auth.tokens."+name)
	}
	metricsAuth["tokens"] = metricTokens
	metrics["auth"] = metricsAuth
	server["metrics"] = metrics
	secrets["server"] = server
	cfg.Server.Metrics.Auth.Tokens = MetricsTokenConfig{
		Prometheus: metricRuntimeValues["prometheus"],
		Grafana:    metricRuntimeValues["grafana"],
		Loki:       metricRuntimeValues["loki"],
	}
	contact := nestedSecretMap(server, "contact")
	for role, recipient := range map[string]*ContactRecipientConfig{
		"admin": &cfg.Server.Contact.Admin, "security": &cfg.Server.Contact.Security,
		"abuse": &cfg.Server.Contact.Abuse, "general": &cfg.Server.Contact.General,
	} {
		roleConfig := nestedSecretMap(contact, role)
		webhooks := nestedSecretMap(roleConfig, "webhooks")
		for name, endpoint := range recipient.Webhooks {
			if strings.HasSuffix(name, "_secret") || strings.TrimSpace(endpoint) == "" {
				continue
			}
			secretName := name + "_secret"
			secret, _ := webhooks[secretName].(string)
			if secret == "" {
				raw := make([]byte, 32)
				if _, err := cryptorand.Read(raw); err != nil {
					return false, fmt.Errorf("%s: %w", configText("cli.config.secret_generate_failed", "name", role+" webhook secret"), err)
				}
				secret = base64.RawURLEncoding.EncodeToString(raw)
				webhooks[secretName] = secret
				secretsChanged = true
				generatedKeys = append(generatedKeys, "server.contact."+role+".webhooks."+secretName)
			}
			recipient.Webhooks[secretName] = secret
		}
		for name, endpoint := range recipient.Webhooks {
			if !strings.HasSuffix(name, "_secret") && endpoint != "" {
				webhooks[name] = endpoint
			}
		}
		roleConfig["webhooks"] = webhooks
		contact[role] = roleConfig
	}
	server["contact"] = contact
	secrets["server"] = server
	data := nestedSecretMap(secrets, "data")
	cve := nestedSecretMap(data, "cve")
	apiKey, _ := cve["api_key"].(string)
	apiKey = expandEnvironmentValue(apiKey)
	if apiKey == "" {
		apiKey = cfg.Data.CVE.APIKey
		if apiKey != "" {
			cve["api_key"] = apiKey
			data["cve"] = cve
			secrets["data"] = data
			secretsChanged = true
			needsConfigSave = true
		}
	}
	cfg.Data.CVE.APIKey = apiKey
	if secretsChanged && !invalidConfigFiles[configFileCacheKey(secretsPath)] {
		if err := writeSecrets(secretsPath, secrets); err != nil {
			return false, err
		}
		for _, key := range generatedKeys {
			writeGeneratedSecretAudit(key)
		}
	}
	return needsConfigSave && !invalidConfigFiles[configFileCacheKey(secretsPath)], nil
}

func encryptDNSCredential(keyText, plaintext string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(keyText)
	if err != nil || len(key) != 32 {
		return "", fmt.Errorf("invalid encryption key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := cryptorand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return "enc:v1:" + base64.StdEncoding.EncodeToString(sealed), nil
}

func decryptDNSCredential(keyText, encoded string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(keyText)
	if err != nil || len(key) != 32 {
		return "", fmt.Errorf("invalid encryption key")
	}
	sealed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, "enc:v1:"))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid encrypted value")
	}
	plaintext, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// RotateEncryptionKey atomically persists a replacement encryption key in
// secrets.yml and advances its monotonic file-backed version.
func RotateEncryptionKey(configPath, next string) (int, error) {
	if err := validateEncryptionKey(next); err != nil {
		return 0, err
	}
	path := filepath.Join(filepath.Dir(configPath), "secrets.yml")
	if invalidConfigFiles[configFileCacheKey(path)] {
		return 0, fmt.Errorf("secrets.yml is invalid; refusing to rotate encryption key")
	}
	values, err := readSecrets(path)
	if err != nil {
		return 0, err
	}
	server := nestedSecretMap(values, "server")
	security := nestedSecretMap(server, "security")
	current, _ := security["encryption_key"].(string)
	if strings.TrimSpace(current) == "" {
		return 0, fmt.Errorf("server.security.encryption_key is missing from secrets.yml")
	}
	if current == next {
		return 0, fmt.Errorf("replacement encryption key matches the current key")
	}
	ssl := nestedSecretMap(server, "ssl")
	letsencrypt := nestedSecretMap(ssl, "letsencrypt")
	credentials := nestedSecretMap(letsencrypt, "dns_credentials")
	reencrypted := make(map[string]string, len(credentials))
	for name, rawValue := range credentials {
		value, ok := rawValue.(string)
		if !ok {
			return 0, fmt.Errorf("invalid DNS credential %s", name)
		}
		if strings.HasPrefix(value, "enc:v1:") {
			value, err = decryptDNSCredential(current, value)
			if err != nil {
				return 0, fmt.Errorf("cannot decrypt DNS credential %s", name)
			}
		}
		reencrypted[name], err = encryptDNSCredential(next, value)
		if err != nil {
			return 0, fmt.Errorf("cannot re-encrypt DNS credential %s: %w", name, err)
		}
	}
	for name, value := range reencrypted {
		credentials[name] = value
	}
	letsencrypt["dns_credentials"] = credentials
	ssl["letsencrypt"] = letsencrypt
	server["ssl"] = ssl
	version := 1
	switch value := security["encryption_key_version"].(type) {
	case int:
		version = value
	case int64:
		version = int(value)
	case uint64:
		version = int(value)
	case float64:
		version = int(value)
	}
	version++
	security["encryption_key"] = next
	security["encryption_key_version"] = version
	server["security"] = security
	values["server"] = server
	if err := writeSecrets(path, values); err != nil {
		return 0, err
	}
	return version, nil
}

func generateOperatorToken() (string, error) {
	return generatePrefixedToken("adm_")
}

func generatePrefixedToken(prefix string) (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	const limit = 248
	token := make([]byte, 32)
	read := make([]byte, 64)
	for offset := 0; offset < len(token); {
		if _, err := cryptorand.Read(read); err != nil {
			return "", err
		}
		for _, value := range read {
			if value >= limit {
				continue
			}
			token[offset] = alphabet[int(value)%len(alphabet)]
			offset++
			if offset == len(token) {
				break
			}
		}
	}
	return prefix + string(token), nil
}

func validateEncryptionKey(value string) error {
	var cfg ServerConfig
	cfg.Server.Security.EncryptionKey = value
	return ensureEncryptionKey(&cfg)
}

func readSecrets(path string) (map[string]interface{}, error) {
	if info, err := os.Stat(path); err == nil && info.Mode().Perm() != 0600 {
		if err := os.Chmod(path, 0600); err != nil {
			invalidConfigFiles[configFileCacheKey(path)] = true
			fmt.Fprintln(os.Stderr, configText("cli.config.secrets_permissions_warning", "file", filepath.Base(path)))
			return cachedSecretValues(path), nil
		}
	} else if err != nil && !os.IsNotExist(err) {
		invalidConfigFiles[configFileCacheKey(path)] = true
		fmt.Fprintln(os.Stderr, configText("cli.config.secrets_check_warning", "file", filepath.Base(path)))
		return cachedSecretValues(path), nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		invalidConfigFiles[configFileCacheKey(path)] = false
		return map[string]interface{}{}, nil
	}
	if err != nil {
		invalidConfigFiles[configFileCacheKey(path)] = true
		fmt.Fprintln(os.Stderr, configText("cli.config.secrets_read_warning", "file", filepath.Base(path)))
		return cachedSecretValues(path), nil
	}
	var values map[string]interface{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		invalidConfigFiles[configFileCacheKey(path)] = true
		fmt.Fprintln(os.Stderr, configText("cli.config.secrets_parse_warning", "file", filepath.Base(path)))
		return cachedSecretValues(path), nil
	}
	invalidConfigFiles[configFileCacheKey(path)] = false
	if values == nil {
		values = map[string]interface{}{}
	}
	lastGoodConfigYAML[configFileCacheKey(path)] = append([]byte(nil), data...)
	return values, nil
}

func cachedSecretValues(path string) map[string]interface{} {
	data, ok := lastGoodConfigYAML[configFileCacheKey(path)]
	if !ok {
		return map[string]interface{}{}
	}
	var values map[string]interface{}
	if yaml.Unmarshal(data, &values) != nil || values == nil {
		return map[string]interface{}{}
	}
	return values
}

func nestedSecretMap(parent map[string]interface{}, key string) map[string]interface{} {
	if value, ok := parent[key].(map[string]interface{}); ok && value != nil {
		return value
	}
	value := map[string]interface{}{}
	parent[key] = value
	return value
}

func writeSecrets(path string, values map[string]interface{}) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("%s: %w", configText("cli.config.secrets_directory_create_failed"), err)
	}
	content, err := yaml.Marshal(values)
	if err != nil {
		return fmt.Errorf("%s: %w", configText("cli.config.secrets_encode_failed"), err)
	}
	file, err := os.CreateTemp(dir, ".secrets-*.tmp")
	if err != nil {
		return fmt.Errorf("%s: %w", configText("cli.config.secrets_temp_create_failed"), err)
	}
	tempPath := file.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return fmt.Errorf("%s: %w", configText("cli.config.secrets_temp_protect_failed"), err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("%s: %w", configText("cli.config.secrets_temp_write_failed"), err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("%s: %w", configText("cli.config.secrets_temp_sync_failed"), err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("%s: %w", configText("cli.config.secrets_temp_close_failed"), err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("%s: %w", configText("cli.config.secrets_replace_failed"), err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("%s: %w", configText("cli.config.secrets_protect_failed"), err)
	}
	return nil
}
