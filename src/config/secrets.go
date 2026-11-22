package config

import (
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

func writeGeneratedSecretAudit(key string) {
	configAuditLog.RLock()
	dir := configAuditLog.dir
	configAuditLog.RUnlock()
	if strings.TrimSpace(dir) == "" {
		return
	}
	if err := audit.Append(dir, audit.Entry{
		Event: "config.secret_generated", Category: "config", Severity: "info",
		Actor:  audit.Actor{Type: "system", IP: "127.0.0.1"},
		Target: &audit.Target{Type: "config", ID: key}, Result: "success",
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write configuration audit event for key %s\n", key)
	}
}

// loadSecretsConfig applies the protected configuration file after every
// ordinary configuration source so its values have the required precedence.
func loadSecretsConfig(configPath string, cfg *Config) error {
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
		return fmt.Errorf("encode secrets configuration: %w", err)
	}
	return applyYAMLConfigFile(secretsPath, data, cfg)
}

// loadSensitiveSecrets reads protected values, migrates a legacy encryption
// key from server.yml, and generates required secrets when they are absent.
func loadSensitiveSecrets(cfg *Config, configPath string) (bool, error) {
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
				return false, fmt.Errorf("generate operator token: %w", err)
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
			return false, fmt.Errorf("generate %s metrics token: %w", name, err)
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
					return false, fmt.Errorf("generate %s webhook secret: %w", role, err)
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
	var cfg Config
	cfg.Server.Security.EncryptionKey = value
	return ensureEncryptionKey(&cfg)
}

func readSecrets(path string) (map[string]interface{}, error) {
	if info, err := os.Stat(path); err == nil && info.Mode().Perm() != 0600 {
		if err := os.Chmod(path, 0600); err != nil {
			invalidConfigFiles[configFileCacheKey(path)] = true
			fmt.Fprintf(os.Stderr, "Warning: secrets file %s permissions could not be secured; using its last good copy or generated defaults\n", filepath.Base(path))
			return cachedSecretValues(path), nil
		}
	} else if err != nil && !os.IsNotExist(err) {
		invalidConfigFiles[configFileCacheKey(path)] = true
		fmt.Fprintf(os.Stderr, "Warning: secrets file %s could not be checked; using its last good copy or generated defaults\n", filepath.Base(path))
		return cachedSecretValues(path), nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		invalidConfigFiles[configFileCacheKey(path)] = false
		return map[string]interface{}{}, nil
	}
	if err != nil {
		invalidConfigFiles[configFileCacheKey(path)] = true
		fmt.Fprintf(os.Stderr, "Warning: secrets file %s could not be read; using its last good copy or generated defaults\n", filepath.Base(path))
		return cachedSecretValues(path), nil
	}
	var values map[string]interface{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		invalidConfigFiles[configFileCacheKey(path)] = true
		fmt.Fprintf(os.Stderr, "Warning: secrets file %s could not be parsed; using its last good copy or generated defaults\n", filepath.Base(path))
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
		return fmt.Errorf("create secrets directory: %w", err)
	}
	content, err := yaml.Marshal(values)
	if err != nil {
		return fmt.Errorf("encode secrets file: %w", err)
	}
	file, err := os.CreateTemp(dir, ".secrets-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary secrets file: %w", err)
	}
	tempPath := file.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return fmt.Errorf("protect temporary secrets file: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("write temporary secrets file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync temporary secrets file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary secrets file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace secrets file: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("protect secrets file: %w", err)
	}
	return nil
}
