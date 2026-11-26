package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/db"
)

const securityIDWindow = 48 * time.Hour

// securityID returns the deterministic one-shot security.txt token for the
// current 48-hour window.
func securityID(now time.Time) (string, error) {
	secret, err := db.InstallationSecret()
	if err != nil {
		return "", err
	}
	window := now.UTC().Unix() / int64(securityIDWindow.Seconds())
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(strconv.FormatInt(window, 10)))
	return hex.EncodeToString(mac.Sum(nil))[:16], nil
}

// validSecurityID accepts the current and immediately previous 48-hour
// windows so a security.txt fetched at a rotation boundary remains usable.
func validSecurityID(supplied string, now time.Time) bool {
	secrets, err := db.InstallationSecretCandidates(now)
	if err != nil || len(supplied) != 16 {
		return false
	}
	current := now.UTC().Unix() / int64(securityIDWindow.Seconds())
	for _, secret := range secrets {
		for _, window := range []int64{current, current - 1} {
			mac := hmac.New(sha256.New, secret)
			_, _ = mac.Write([]byte(strconv.FormatInt(window, 10)))
			expected := hex.EncodeToString(mac.Sum(nil))[:16]
			if hmac.Equal([]byte(expected), []byte(supplied)) {
				return true
			}
		}
	}
	return false
}

func securityReportURL(rURL string, id string) string {
	return fmt.Sprintf("%s/server/contact?security_id=%s", rURL, id)
}

func (s *Server) publicServerURL(r *http.Request) string {
	serverURL := strings.TrimRight(s.detectServerURL(r), "/")
	if s.config != nil && s.currentConfig() != nil {
		prefix := strings.Trim(strings.TrimSpace(s.currentConfig().Server.BaseURL), "/")
		if prefix != "" {
			serverURL += "/" + prefix
		}
	}
	return serverURL
}

func (s *Server) logInvalidSecurityID(r *http.Request, supplied string) {
	if supplied == "" || s.config == nil || s.config.Paths == nil {
		return
	}
	digest := sha256.Sum256([]byte(supplied))
	logDir := s.config.Paths.GetLogsDir()
	if os.MkdirAll(logDir, 0750) != nil {
		return
	}
	file, err := os.OpenFile(filepath.Join(logDir, "security.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	line := fmt.Sprintf("security.security_id_invalid ip=%q user_agent=%q supplied_id_sha256=%s\n", safeSecurityLogField(s.clientIP(r)), safeSecurityLogField(r.UserAgent()), hex.EncodeToString(digest[:])[:8])
	_, _ = file.WriteString(line)
}

func (s *Server) logSecurityReportReceived(trackingID, severity, component string) {
	if s.config == nil || s.config.Paths == nil {
		return
	}
	logDir := s.config.Paths.GetLogsDir()
	if os.MkdirAll(logDir, 0750) != nil {
		return
	}
	file, err := os.OpenFile(filepath.Join(logDir, "security.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	_ = file.Chmod(0600)
	_, _ = fmt.Fprintf(file, "security.report_received tracking_id=%q severity=%q component=%q\n", safeSecurityLogField(trackingID), safeSecurityLogField(severity), safeSecurityLogField(component))
}

func safeSecurityLogField(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, value)
}
