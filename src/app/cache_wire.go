package app

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/redis/go-redis/v9"
)

func configureRateLimitCache(cfg config.CacheConfig, configDir string) {
	if cfg.Type != "valkey" && cfg.Type != "redis" {
		db.ConfigureRateLimitCache(nil, "")
		return
	}
	var options *redis.Options
	if strings.TrimSpace(cfg.URL) != "" {
		connectionURL := strings.TrimSpace(cfg.URL)
		connectionURL = strings.Replace(connectionURL, "valkey://", "redis://", 1)
		connectionURL = strings.Replace(connectionURL, "valkeys://", "rediss://", 1)
		parsed, err := redis.ParseURL(connectionURL)
		if err != nil {
			logCacheConfigWarning(err)
			db.ConfigureRateLimitCache(nil, "")
			return
		}
		options = parsed
	} else {
		port := cfg.Port
		if port < 1 || port > 65535 {
			port = 6379
		}
		host := strings.TrimSpace(cfg.Host)
		if host == "" {
			host = "localhost"
		}
		options = &redis.Options{Addr: net.JoinHostPort(host, fmt.Sprint(port)), Username: cfg.Username, Password: cfg.Password, DB: cfg.DB}
	}
	if cfg.TLS || options.TLSConfig != nil {
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.TLSSkipVerify}
		if options.TLSConfig != nil {
			tlsConfig = options.TLSConfig.Clone()
		}
		tlsConfig.MinVersion = tls.VersionTLS12
		tlsConfig.InsecureSkipVerify = cfg.TLSSkipVerify
		if cfg.TLSCA != "" {
			caPath := filepath.Join(configDir, "ssl", filepath.Base(cfg.TLSCA))
			data, err := os.ReadFile(caPath)
			if err != nil {
				logCacheConfigWarning(err)
				db.ConfigureRateLimitCache(nil, "")
				return
			}
			pool, err := x509.SystemCertPool()
			if err != nil || pool == nil {
				pool = x509.NewCertPool()
			}
			if !pool.AppendCertsFromPEM(data) {
				logCacheConfigWarning(fmt.Errorf("cache TLS CA contains no certificates"))
				db.ConfigureRateLimitCache(nil, "")
				return
			}
			tlsConfig.RootCAs = pool
		}
		if cfg.TLSCert != "" || cfg.TLSKey != "" {
			certPath := filepath.Join(configDir, "ssl", filepath.Base(cfg.TLSCert))
			keyPath := filepath.Join(configDir, "ssl", filepath.Base(cfg.TLSKey))
			cert, err := tls.LoadX509KeyPair(certPath, keyPath)
			if err != nil {
				logCacheConfigWarning(err)
				db.ConfigureRateLimitCache(nil, "")
				return
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}
		options.TLSConfig = tlsConfig
	}
	options.PoolSize = cfg.PoolSize
	options.MinIdleConns = cfg.MinIdle
	if timeout, err := time.ParseDuration(cfg.Timeout); err == nil && timeout > 0 {
		options.DialTimeout, options.ReadTimeout, options.WriteTimeout = timeout, timeout, timeout
	}
	db.ConfigureRateLimitCache(redis.NewClient(options), cfg.Prefix)
}

func logCacheConfigWarning(err error) {
	log.Printf("cache configuration unavailable; rate limits will use the configured fallback: %v", err)
}
