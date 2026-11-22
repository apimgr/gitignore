package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/gitignore/src/common/audit"
	"github.com/apimgr/gitignore/src/db"
	"github.com/go-chi/chi/v5"
)

const ownerTokenCookieName = "gitignore_owner_token_5qvQQyc5"

var tokenAlphabet = []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")

var errInvalidBearerToken = errors.New("invalid bearer token")

type ownerTokenPageFlash struct {
	Revealed map[int64]string
	Message  string
	Created  time.Time
}

var ownerTokenPageFlashes sync.Map

type TokenIdentity struct {
	Kind        string
	OwnerID     string
	TokenID     int64
	TokenPrefix string
}

type publicOwnerToken struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	Name       string `json:"name,omitempty"`
	Prefix     string `json:"prefix"`
	Masked     string `json:"masked"`
	CreatedAt  int64  `json:"created_at"`
	RotatedAt  *int64 `json:"rotated_at,omitempty"`
	ExpiresAt  *int64 `json:"expires_at,omitempty"`
	LastUsedAt *int64 `json:"last_used_at,omitempty"`
	Status     string `json:"status"`
}

type ownerTokenRequest struct {
	Name      string `json:"name"`
	Expiry    string `json:"expiry"`
	ExpiresOn string `json:"expires_on"`
}

// AuthenticateBearerToken validates the existing Authorization Bearer token
// and returns its operator or anonymous-owner identity. MCP and native API
// handlers use this same validation path.
func (s *Server) AuthenticateBearerToken(raw string) (TokenIdentity, error) {
	status := "failed"
	defer func() {
		if s.metrics != nil {
			s.metrics.AuthAttemptsTotal.WithLabelValues("bearer_token", status).Inc()
		}
	}()
	if raw == "" {
		return TokenIdentity{}, errInvalidBearerToken
	}
	if s.config != nil && s.currentConfig() != nil && s.currentConfig().Server.Token != "" {
		presented := sha256.Sum256([]byte(raw))
		configured := sha256.Sum256([]byte(s.currentConfig().Server.Token))
		if subtle.ConstantTimeCompare(presented[:], configured[:]) == 1 {
			status = "success"
			return TokenIdentity{Kind: "operator"}, nil
		}
	}
	if len(raw) != 36 || !strings.HasPrefix(raw, "own_") {
		return TokenIdentity{}, errInvalidBearerToken
	}
	digest := sha256.Sum256([]byte(raw))
	prefix := raw[:12]
	candidates, err := db.FindOwnerTokensByPrefix(prefix)
	if err != nil {
		return TokenIdentity{}, err
	}
	now := time.Now().UTC()
	for _, candidate := range candidates {
		stored, decodeErr := hex.DecodeString(candidate.TokenHash)
		if decodeErr != nil || len(stored) != sha256.Size || subtle.ConstantTimeCompare(digest[:], stored) != 1 {
			continue
		}
		if candidate.RevokedAt.Valid || candidate.ExpiresAt.Valid && candidate.ExpiresAt.Int64 <= now.Unix() {
			return TokenIdentity{}, errInvalidBearerToken
		}
		if err := db.MarkOwnerTokenUsed(candidate.ID, now); err != nil {
			return TokenIdentity{}, err
		}
		status = "success"
		return TokenIdentity{Kind: candidate.Kind, OwnerID: candidate.OwnerID, TokenID: candidate.ID, TokenPrefix: candidate.TokenPrefix}, nil
	}
	return TokenIdentity{}, errInvalidBearerToken
}

func (s *Server) ownerCookieMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/mcp" {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(ownerTokenCookieName)
		if err == nil && cookie.Value != "" {
			if _, authErr := s.AuthenticateBearerToken(cookie.Value); authErr != nil {
				s.clearOwnerCookie(w, r)
			} else {
				s.setOwnerCookie(w, r, cookie.Value)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func parseBearerHeader(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	return parts[1], true
}

func (s *Server) authenticateRequest(r *http.Request, allowCookie bool) (TokenIdentity, error) {
	if raw, ok := parseBearerHeader(r.Header.Get("Authorization")); ok {
		return s.AuthenticateBearerToken(raw)
	}
	if !allowCookie {
		return TokenIdentity{}, errInvalidBearerToken
	}
	cookie, err := r.Cookie(ownerTokenCookieName)
	if err != nil {
		return TokenIdentity{}, errInvalidBearerToken
	}
	return s.AuthenticateBearerToken(cookie.Value)
}

func (s *Server) requirePrimaryOwner(w http.ResponseWriter, r *http.Request, allowCookie bool) (TokenIdentity, bool) {
	identity, err := s.authenticateRequest(r, allowCookie)
	if err != nil {
		return TokenIdentity{}, false
	}
	if identity.Kind != "primary" || identity.OwnerID == "" {
		return TokenIdentity{}, false
	}
	if allowCookie && strings.TrimSpace(r.Header.Get("Authorization")) == "" {
		if cookie, cookieErr := r.Cookie(ownerTokenCookieName); cookieErr == nil {
			s.setOwnerCookie(w, r, cookie.Value)
		}
	}
	return identity, true
}

func newOwnerToken() (string, error) {
	const prefix = "own_"
	token := make([]byte, 32)
	for i := range token {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(tokenAlphabet))))
		if err != nil {
			return "", err
		}
		token[i] = tokenAlphabet[n.Int64()]
	}
	return prefix + string(token), nil
}

func newOwnerID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(buf)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

func (s *Server) encryptOwnerToken(raw string) (string, error) {
	if s.config == nil || s.currentConfig() == nil {
		return "", errors.New("owner token encryption is unavailable")
	}
	ciphertext, err := encryptSecurityReport(s.currentConfig().Server.Security.EncryptionKey, []byte(raw))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (s *Server) decryptOwnerToken(encoded string) (string, error) {
	if s.config == nil || s.currentConfig() == nil {
		return "", errors.New("owner token encryption is unavailable")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	plaintext, err := DecryptSecurityReport(s.currentConfig().Server.Security.EncryptionKey, ciphertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func hashOwnerToken(raw string) (string, string) {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:]), raw[:12]
}

func (s *Server) issueOwnerToken(now time.Time, source, ip string) (string, db.OwnerToken, error) {
	ownerID, err := newOwnerID()
	if err != nil {
		return "", db.OwnerToken{}, err
	}
	raw, err := newOwnerToken()
	if err != nil {
		return "", db.OwnerToken{}, err
	}
	encoded, err := s.encryptOwnerToken(raw)
	if err != nil {
		return "", db.OwnerToken{}, err
	}
	hash, prefix := hashOwnerToken(raw)
	item, err := db.CreateOwnerWithPrimary(ownerID, hash, prefix, encoded, now)
	if err != nil {
		return "", db.OwnerToken{}, err
	}
	s.logTokenAudit("token.created", ownerID, prefix, source, ip, "issue", true)
	return raw, item, nil
}

func (s *Server) issueLinkedToken(ownerID, name string, expiry *int64, now time.Time, source, ip string) (string, db.OwnerToken, error) {
	raw, err := newOwnerToken()
	if err != nil {
		return "", db.OwnerToken{}, err
	}
	encoded, err := s.encryptOwnerToken(raw)
	if err != nil {
		return "", db.OwnerToken{}, err
	}
	hash, prefix := hashOwnerToken(raw)
	item, err := db.CreateLinkedOwnerToken(ownerID, name, hash, prefix, encoded, expiry, s.maxLinkedTokens(), now)
	if err != nil {
		return "", db.OwnerToken{}, err
	}
	s.logTokenAudit("token.created", ownerID, prefix, source, ip, "linked", true)
	return raw, item, nil
}

func maskOwnerToken(raw string) string {
	if len(raw) != 36 {
		return "own_••••••••••••••••••••••••••••"
	}
	return "own_" + strings.Repeat("•", 28) + raw[len(raw)-4:]
}

func (s *Server) publicToken(item db.OwnerToken, now time.Time) publicOwnerToken {
	status := "active"
	if item.RevokedAt.Valid {
		status = "revoked"
	} else if item.ExpiresAt.Valid && item.ExpiresAt.Int64 <= now.Unix() {
		status = "expired"
	}
	value := publicOwnerToken{ID: item.ID, Kind: item.Kind, Name: item.Name.String, Prefix: item.TokenPrefix, CreatedAt: item.CreatedAt, Status: status}
	if raw, err := s.decryptOwnerToken(item.TokenEnc); err == nil {
		value.Masked = maskOwnerToken(raw)
	} else {
		value.Masked = maskOwnerToken("")
	}
	if item.RotatedAt.Valid {
		value.RotatedAt = &item.RotatedAt.Int64
	}
	if item.ExpiresAt.Valid {
		value.ExpiresAt = &item.ExpiresAt.Int64
	}
	if item.LastUsedAt.Valid {
		value.LastUsedAt = &item.LastUsedAt.Int64
	}
	return value
}

func (s *Server) loadTokenPage(ownerID string, revealed map[int64]string, message string) (map[string]interface{}, error) {
	items, err := db.ListOwnerTokens(ownerID)
	if err != nil {
		return nil, err
	}
	var primary *map[string]interface{}
	linked := make([]map[string]interface{}, 0, len(items))
	now := time.Now().UTC()
	for _, item := range items {
		value := s.publicToken(item, now)
		row := map[string]interface{}{
			"id": value.ID, "kind": value.Kind, "name": value.Name, "prefix": value.Prefix,
			"masked": value.Masked, "created": time.Unix(value.CreatedAt, 0).UTC().Format("2006-01-02 15:04 UTC"),
			"status": value.Status, "revealed": revealed[item.ID],
			"lastUsed": "Never", "expires": "Never",
		}
		if item.LastUsedAt.Valid {
			row["lastUsed"] = time.Unix(item.LastUsedAt.Int64, 0).UTC().Format("2006-01-02 15:04 UTC")
		}
		if item.ExpiresAt.Valid {
			row["expires"] = time.Unix(item.ExpiresAt.Int64, 0).UTC().Format("2006-01-02 15:04 UTC")
		}
		if item.Kind == "primary" {
			primary = &row
		} else {
			linked = append(linked, row)
		}
	}
	return map[string]interface{}{"authenticated": true, "primary": primary, "linked": linked, "message": message}, nil
}

func (s *Server) handleOwnerTokensPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	cookie, cookieErr := r.Cookie(ownerTokenCookieName)
	identity, err := TokenIdentity{}, errInvalidBearerToken
	if cookieErr == nil {
		identity, err = s.AuthenticateBearerToken(cookie.Value)
	}
	if err != nil || identity.Kind != "primary" {
		if cookieErr == nil && cookie.Value != "" {
			s.clearOwnerCookie(w, r)
		}
		s.renderPage(w, r, "preferences_tokens", PageData{Title: "Owner tokens", Data: map[string]interface{}{"authenticated": false}})
		return
	}
	s.setOwnerCookie(w, r, cookie.Value)
	data, err := s.loadTokenPage(identity.OwnerID, nil, "")
	if err != nil {
		s.renderPageStatus(w, r, "error", http.StatusServiceUnavailable, PageData{Title: "Owner tokens", Data: map[string]interface{}{"message": "Token storage is temporarily unavailable."}})
		return
	}
	if value, ok := takeOwnerTokenPageFlash(identity.OwnerID); ok {
		data, err = s.loadTokenPage(identity.OwnerID, value.Revealed, value.Message)
		if err != nil {
			s.renderPageStatus(w, r, "error", http.StatusServiceUnavailable, PageData{Title: "Owner tokens"})
			return
		}
	}
	data["csrfToken"] = ensureCSRFTokenWithSecure(w, r, preferenceCookieSecure(s, r))
	s.renderPage(w, r, "preferences_tokens", PageData{Title: "Owner tokens", Data: data})
}

func (s *Server) setOwnerCookie(w http.ResponseWriter, r *http.Request, raw string) {
	maxAge := 365 * 24 * 60 * 60
	if s.config != nil && s.currentConfig() != nil {
		maxAge = parseCookieMaxAge(s.currentConfig().Server.Tokens.CookieMaxAge)
	}
	http.SetCookie(w, &http.Cookie{Name: ownerTokenCookieName, Value: raw, Path: "/", HttpOnly: true, Secure: preferenceCookieSecure(s, r), SameSite: http.SameSiteStrictMode, MaxAge: maxAge, Expires: time.Now().Add(time.Duration(maxAge) * time.Second)})
}

func storeOwnerTokenPageFlash(ownerID string, revealed map[int64]string, message string) {
	ownerTokenPageFlashes.Range(func(key, value interface{}) bool {
		if flash, ok := value.(ownerTokenPageFlash); ok && time.Since(flash.Created) > 5*time.Minute {
			ownerTokenPageFlashes.Delete(key)
		}
		return true
	})
	ownerTokenPageFlashes.Store(ownerID, ownerTokenPageFlash{Revealed: revealed, Message: message, Created: time.Now()})
}

func takeOwnerTokenPageFlash(ownerID string) (ownerTokenPageFlash, bool) {
	value, ok := ownerTokenPageFlashes.LoadAndDelete(ownerID)
	if !ok {
		return ownerTokenPageFlash{}, false
	}
	flash, ok := value.(ownerTokenPageFlash)
	if !ok || time.Since(flash.Created) > 5*time.Minute {
		return ownerTokenPageFlash{}, false
	}
	return flash, true
}

func redirectOwnerTokenPage(w http.ResponseWriter, r *http.Request, fragment string) {
	w.Header().Set("Cache-Control", "private, no-store")
	http.Redirect(w, r, "/server/preferences/tokens#"+fragment, http.StatusSeeOther)
}

func (s *Server) maxLinkedTokens() int {
	if s.config != nil && s.currentConfig() != nil && s.currentConfig().Server.Tokens.MaxLinked > 0 {
		return s.currentConfig().Server.Tokens.MaxLinked
	}
	return 25
}

func (s *Server) clearOwnerCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: ownerTokenCookieName, Value: "", Path: "/", HttpOnly: true, Secure: preferenceCookieSecure(s, r), SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}

func (s *Server) tokenActionLimiter(w http.ResponseWriter, r *http.Request) bool {
	if s.strictTokenLimiter != nil && !s.strictTokenLimiter.allow(s.clientIP(r)) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			sendAPIResponseError(w, "RATE_LIMITED", "too many token requests")
		} else {
			s.renderPageStatus(w, r, "preferences_tokens", http.StatusTooManyRequests, PageData{Title: "Owner tokens", Data: map[string]interface{}{"authenticated": false, "message": "Too many token requests. Try again later."}})
		}
		return false
	}
	return true
}

func (s *Server) handleIssueOwnerTokenWeb(w http.ResponseWriter, r *http.Request) {
	if !s.tokenActionLimiter(w, r) {
		return
	}
	if !validCSRFRequest(r) {
		sendAPIResponseError(w, "CSRF_FAILED", "invalid request token")
		return
	}
	if _, err := s.authenticateRequest(r, true); err == nil {
		s.renderPageStatus(w, r, "preferences_tokens", http.StatusConflict, PageData{Title: "Owner tokens", Data: map[string]interface{}{"authenticated": false, "message": "This browser already has a valid owner token."}})
		return
	}
	raw, item, err := s.issueOwnerToken(time.Now(), "web", s.clientIP(r))
	if err != nil {
		s.renderPageStatus(w, r, "preferences_tokens", http.StatusServiceUnavailable, PageData{Title: "Owner tokens", Data: map[string]interface{}{"authenticated": false, "message": "A token could not be created."}})
		return
	}
	s.setOwnerCookie(w, r, raw)
	w.Header().Set("Cache-Control", "private, no-store")
	storeOwnerTokenPageFlash(item.OwnerID, map[int64]string{item.ID: raw}, "Your primary token was created. Copy and store it safely.")
	redirectOwnerTokenPage(w, r, "primary")
}

func (s *Server) handleRestoreOwnerToken(w http.ResponseWriter, r *http.Request) {
	if !s.tokenActionLimiter(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if !validCSRFRequest(r) {
		sendAPIResponseError(w, "CSRF_FAILED", "invalid request token")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderPageStatus(w, r, "preferences_tokens", http.StatusBadRequest, PageData{Title: "Owner tokens", Data: map[string]interface{}{"authenticated": false, "message": "Enter a valid primary token."}})
		return
	}
	raw := strings.TrimSpace(r.FormValue("token"))
	identity, err := s.AuthenticateBearerToken(raw)
	if err != nil || identity.Kind != "primary" {
		s.logTokenAudit("token.restore_failed", "", "", "web", s.clientIP(r), "", false)
		s.renderPageStatus(w, r, "preferences_tokens", http.StatusUnauthorized, PageData{Title: "Owner tokens", Data: map[string]interface{}{"authenticated": false, "message": "That primary token is invalid."}})
		return
	}
	s.setOwnerCookie(w, r, raw)
	s.logTokenAudit("token.restored", identity.OwnerID, identity.TokenPrefix, "web", s.clientIP(r), "", true)
	storeOwnerTokenPageFlash(identity.OwnerID, nil, "Identity restored in this browser.")
	w.Header().Set("Cache-Control", "private, no-store")
	http.Redirect(w, r, "/server/preferences/tokens#primary", http.StatusSeeOther)
}

func (s *Server) handleForgetOwnerToken(w http.ResponseWriter, r *http.Request) {
	if !validCSRFRequest(r) {
		sendAPIResponseError(w, "CSRF_FAILED", "invalid request token")
		return
	}
	s.clearOwnerCookie(w, r)
	w.Header().Set("Clear-Site-Data", `"storage"`)
	w.Header().Set("Cache-Control", "private, no-store")
	http.Redirect(w, r, "/server/preferences/tokens", http.StatusSeeOther)
}

func (s *Server) handleOwnerTokenWebAction(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	}
	if !s.tokenActionLimiter(w, r) {
		return
	}
	if !validCSRFRequest(r) {
		sendAPIResponseError(w, "CSRF_FAILED", "invalid request token")
		return
	}
	identity, ok := s.requirePrimaryOwner(w, r, true)
	if !ok {
		sendAPIResponseError(w, "FORBIDDEN", "primary owner token required")
		return
	}
	if err := r.ParseForm(); err != nil {
		sendAPIResponseError(w, "BAD_REQUEST", "invalid token action")
		return
	}
	pathID := chi.URLParam(r, "id")
	tokenID, _ := strconv.ParseInt(pathID, 10, 64)
	var target db.OwnerToken
	if strings.Contains(r.URL.Path, "/tokens/") && tokenID > 0 {
		var err error
		target, err = db.GetOwnerToken(identity.OwnerID, tokenID)
		if err != nil || target.Kind != "linked" {
			sendAPIResponseError(w, "NOT_FOUND", "token not found")
			return
		}
	} else {
		target, _ = db.GetOwnerToken(identity.OwnerID, identity.TokenID)
	}
	now := time.Now().UTC()
	var raw string
	var message string
	switch {
	case strings.HasSuffix(r.URL.Path, "/reveal"):
		raw, _ = s.decryptOwnerToken(target.TokenEnc)
		if raw == "" {
			sendAPIResponseError(w, "SERVER_ERROR", "token cannot be decrypted")
			return
		}
		s.logTokenAudit("token.revealed", identity.OwnerID, target.TokenPrefix, "web", s.clientIP(r), "", true)
		message = "Token revealed for this page load."
	case strings.HasSuffix(r.URL.Path, "/rotate"):
		raw, _ = newOwnerToken()
		encoded, err := s.encryptOwnerToken(raw)
		if err != nil {
			sendAPIResponseError(w, "SERVER_ERROR", "token rotation failed")
			return
		}
		hash, prefix := hashOwnerToken(raw)
		if err := db.RotateOwnerToken(identity.OwnerID, target.ID, hash, prefix, encoded, now); err != nil {
			sendAPIResponseError(w, "SERVER_ERROR", "token rotation failed")
			return
		}
		if target.Kind == "primary" {
			s.setOwnerCookie(w, r, raw)
		}
		s.logTokenAudit("token.rotated", identity.OwnerID, target.TokenPrefix, "web", s.clientIP(r), "", true)
		message = "Token rotated. The previous value is invalid."
	case strings.HasSuffix(r.URL.Path, "/update"):
		name := strings.TrimSpace(r.FormValue("name"))
		if len(name) < 1 || len(name) > 64 {
			sendAPIResponseError(w, "VALIDATION_FAILED", "token name must contain 1 to 64 characters")
			return
		}
		expiresAt, err := parseOwnerTokenExpiry(r.FormValue("expiry"), r.FormValue("expires_on"), now)
		if err != nil {
			sendAPIResponseError(w, "VALIDATION_FAILED", err.Error())
			return
		}
		if err := db.UpdateLinkedOwnerToken(identity.OwnerID, target.ID, name, expiresAt); err != nil {
			sendAPIResponseError(w, "SERVER_ERROR", "token update failed")
			return
		}
		message = "Linked token updated."
	case strings.HasSuffix(r.URL.Path, "/revoke"):
		if err := db.RevokeLinkedOwnerToken(identity.OwnerID, target.ID, now); err != nil {
			sendAPIResponseError(w, "NOT_FOUND", "token not found")
			return
		}
		s.logTokenAudit("token.revoked", identity.OwnerID, target.TokenPrefix, "web", s.clientIP(r), "", true)
		message = "Linked token revoked."
	default:
		sendAPIResponseError(w, "NOT_FOUND", "token action not found")
		return
	}
	if strings.HasSuffix(r.URL.Path, "/reveal") {
		data, err := s.loadTokenPage(identity.OwnerID, map[int64]string{target.ID: raw}, message)
		if err != nil {
			sendAPIResponseError(w, "SERVER_ERROR", "token data is unavailable")
			return
		}
		data["csrfToken"] = ensureCSRFTokenWithSecure(w, r, preferenceCookieSecure(s, r))
		w.Header().Set("Cache-Control", "private, no-store")
		s.renderPage(w, r, "preferences_tokens", PageData{Title: "Owner tokens", Data: data})
		return
	}
	revealed := map[int64]string(nil)
	if raw != "" {
		revealed = map[int64]string{target.ID: raw}
	}
	storeOwnerTokenPageFlash(identity.OwnerID, revealed, message)
	fragment := "primary"
	if target.Kind != "primary" {
		fragment = "token-" + strconv.FormatInt(target.ID, 10)
	}
	redirectOwnerTokenPage(w, r, fragment)
}

func (s *Server) handleCreateLinkedOwnerTokenWeb(w http.ResponseWriter, r *http.Request) {
	if !s.tokenActionLimiter(w, r) {
		return
	}
	if !validCSRFRequest(r) {
		sendAPIResponseError(w, "CSRF_FAILED", "invalid request token")
		return
	}
	identity, ok := s.requirePrimaryOwner(w, r, true)
	if !ok {
		sendAPIResponseError(w, "FORBIDDEN", "primary owner token required")
		return
	}
	if err := r.ParseForm(); err != nil {
		sendAPIResponseError(w, "BAD_REQUEST", "invalid token request")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if len(name) < 1 || len(name) > 64 {
		sendAPIResponseError(w, "VALIDATION_FAILED", "token name must contain 1 to 64 characters")
		return
	}
	if exists, err := ownerTokenNameExists(identity.OwnerID, name, 0); err != nil || exists {
		sendAPIResponseError(w, "CONFLICT", "a token with that name already exists")
		return
	}
	count, err := db.CountActiveLinkedTokens(identity.OwnerID, time.Now())
	if err != nil {
		s.renderPageStatus(w, r, "error", http.StatusServiceUnavailable, PageData{Title: "Owner tokens"})
		return
	}
	if count >= s.maxLinkedTokens() {
		sendAPIResponseError(w, "CONFLICT", "maximum active linked token count reached")
		return
	}
	expiresAt, err := parseOwnerTokenExpiry(r.FormValue("expiry"), r.FormValue("expires_on"), time.Now())
	if err != nil {
		sendAPIResponseError(w, "VALIDATION_FAILED", err.Error())
		return
	}
	raw, item, err := s.issueLinkedToken(identity.OwnerID, name, expiresAt, time.Now(), "web", s.clientIP(r))
	if err != nil {
		sendAPIResponseError(w, "SERVER_ERROR", "linked token could not be created")
		return
	}
	storeOwnerTokenPageFlash(identity.OwnerID, map[int64]string{item.ID: raw}, "Linked token created. Copy and store it safely.")
	redirectOwnerTokenPage(w, r, "token-"+strconv.FormatInt(item.ID, 10))
}

func parseOwnerTokenExpiry(expiry, date string, now time.Time) (*int64, error) {
	switch expiry {
	case "", "never":
		return nil, nil
	case "7d", "30d", "90d", "1y":
		days := map[string]int{"7d": 7, "30d": 30, "90d": 90, "1y": 365}[expiry]
		value := now.UTC().Add(time.Duration(days) * 24 * time.Hour).Unix()
		return &value, nil
	case "custom":
		parsed, err := time.Parse("2006-01-02", date)
		if err != nil || !parsed.After(now.UTC().Truncate(24*time.Hour)) {
			return nil, errors.New("custom expiry must be a future date")
		}
		value := parsed.Add(24*time.Hour - time.Second).Unix()
		return &value, nil
	default:
		return nil, errors.New("expiry must be never, 7d, 30d, 90d, 1y, or custom")
	}
}

func (s *Server) handleAPIOwnerTokens(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requirePrimaryOwner(w, r, false)
	if !ok {
		s.sendBearerError(w, r)
		return
	}
	if r.Method == http.MethodGet {
		items, err := db.ListOwnerTokens(identity.OwnerID)
		if err != nil {
			sendAPIResponseError(w, "SERVER_ERROR", "token data is unavailable")
			return
		}
		result := make([]publicOwnerToken, 0, len(items))
		for _, item := range items {
			result = append(result, s.publicToken(item, time.Now().UTC()))
		}
		w.Header().Set("Cache-Control", "private, no-store")
		sendPrivateAPIResponseOK(w, http.StatusOK, result)
		return
	}
	if !s.tokenActionLimiter(w, r) {
		return
	}
	request, err := decodeOwnerTokenRequest(w, r)
	if err != nil {
		sendAPIResponseError(w, "BAD_REQUEST", "invalid token request")
		return
	}
	name := strings.TrimSpace(request.Name)
	if len(name) < 1 || len(name) > 64 {
		sendAPIResponseError(w, "VALIDATION_FAILED", "token name must contain 1 to 64 characters")
		return
	}
	if exists, err := ownerTokenNameExists(identity.OwnerID, name, 0); err != nil || exists {
		sendAPIResponseError(w, "CONFLICT", "a token with that name already exists")
		return
	}
	count, err := db.CountActiveLinkedTokens(identity.OwnerID, time.Now())
	if err != nil {
		sendAPIResponseError(w, "SERVER_ERROR", "token data is unavailable")
		return
	}
	if count >= s.maxLinkedTokens() {
		sendAPIResponseError(w, "CONFLICT", "maximum active linked token count reached")
		return
	}
	expiresAt, err := parseOwnerTokenExpiry(request.Expiry, request.ExpiresOn, time.Now())
	if err != nil {
		sendAPIResponseError(w, "VALIDATION_FAILED", err.Error())
		return
	}
	raw, item, err := s.issueLinkedToken(identity.OwnerID, name, expiresAt, time.Now(), "api", s.clientIP(r))
	if err != nil {
		sendAPIResponseError(w, "SERVER_ERROR", "linked token could not be created")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	sendPrivateAPIResponseOK(w, http.StatusCreated, map[string]interface{}{"token": s.publicToken(item, time.Now()), "value": raw})
}

func decodeOwnerTokenRequest(w http.ResponseWriter, r *http.Request) (ownerTokenRequest, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var request ownerTokenRequest
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&request)
		return request, err
	}
	err := r.ParseForm()
	request = ownerTokenRequest{Name: r.FormValue("name"), Expiry: r.FormValue("expiry"), ExpiresOn: r.FormValue("expires_on")}
	return request, err
}

func (s *Server) handleAPIPrimaryOwnerToken(w http.ResponseWriter, r *http.Request) {
	if !s.tokenActionLimiter(w, r) {
		return
	}
	if header := strings.TrimSpace(r.Header.Get("Authorization")); header != "" {
		raw, ok := parseBearerHeader(header)
		if !ok {
			s.sendBearerError(w, r)
			return
		}
		if _, err := s.AuthenticateBearerToken(raw); err == nil {
			sendAPIResponseError(w, "CONFLICT", "a valid token is already presented")
			return
		} else {
			s.sendBearerError(w, r)
			return
		}
	}
	token, item, err := s.issueOwnerToken(time.Now(), "api", s.clientIP(r))
	if err != nil {
		sendAPIResponseError(w, "SERVER_ERROR", "owner token could not be created")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	sendPrivateAPIResponseOK(w, http.StatusCreated, map[string]interface{}{"owner_token": token, "token": s.publicToken(item, time.Now())})
}

func (s *Server) handleAPIPrimaryTokenAction(w http.ResponseWriter, r *http.Request) {
	if !s.tokenActionLimiter(w, r) {
		return
	}
	identity, ok := s.requirePrimaryOwner(w, r, false)
	if !ok {
		s.sendBearerError(w, r)
		return
	}
	item, err := db.GetOwnerToken(identity.OwnerID, identity.TokenID)
	if err != nil {
		s.sendBearerError(w, r)
		return
	}
	s.handleAPITokenAction(w, r, identity, item)
}

func (s *Server) handleAPILinkedTokenAction(w http.ResponseWriter, r *http.Request) {
	if !s.tokenActionLimiter(w, r) {
		return
	}
	identity, ok := s.requirePrimaryOwner(w, r, false)
	if !ok {
		s.sendBearerError(w, r)
		return
	}
	tokenID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	item, err := db.GetOwnerToken(identity.OwnerID, tokenID)
	if err != nil || item.Kind != "linked" {
		sendAPIResponseError(w, "NOT_FOUND", "token not found")
		return
	}
	s.handleAPITokenAction(w, r, identity, item)
}

func (s *Server) handleAPITokenAction(w http.ResponseWriter, r *http.Request, identity TokenIdentity, item db.OwnerToken) {
	now := time.Now().UTC()
	var raw string
	var err error
	if strings.HasSuffix(r.URL.Path, "/reveal") {
		raw, err = s.decryptOwnerToken(item.TokenEnc)
		if err != nil {
			sendAPIResponseError(w, "SERVER_ERROR", "token cannot be decrypted")
			return
		}
		s.logTokenAudit("token.revealed", identity.OwnerID, item.TokenPrefix, "api", s.clientIP(r), "", true)
	} else if strings.HasSuffix(r.URL.Path, "/rotate") {
		raw, err = newOwnerToken()
		if err == nil {
			var encoded string
			encoded, err = s.encryptOwnerToken(raw)
			if err == nil {
				var hash, prefix string
				hash, prefix = hashOwnerToken(raw)
				err = db.RotateOwnerToken(identity.OwnerID, item.ID, hash, prefix, encoded, now)
			}
		}
		if err != nil {
			sendAPIResponseError(w, "SERVER_ERROR", "token rotation failed")
			return
		}
		s.logTokenAudit("token.rotated", identity.OwnerID, item.TokenPrefix, "api", s.clientIP(r), "", true)
	} else if r.Method == http.MethodDelete || strings.HasSuffix(r.URL.Path, "/revoke") {
		if err := db.RevokeLinkedOwnerToken(identity.OwnerID, item.ID, now); err != nil {
			sendAPIResponseError(w, "NOT_FOUND", "token not found")
			return
		}
		s.logTokenAudit("token.revoked", identity.OwnerID, item.TokenPrefix, "api", s.clientIP(r), "", true)
		w.Header().Set("Cache-Control", "private, no-store")
		sendPrivateAPIResponseOK(w, http.StatusOK, map[string]string{"status": "revoked"})
		return
	} else if r.Method == http.MethodPatch {
		request, err := decodeOwnerTokenRequest(w, r)
		if err != nil {
			sendAPIResponseError(w, "BAD_REQUEST", "invalid token update")
			return
		}
		name := strings.TrimSpace(request.Name)
		if len(name) < 1 || len(name) > 64 {
			sendAPIResponseError(w, "VALIDATION_FAILED", "token name must contain 1 to 64 characters")
			return
		}
		if exists, err := ownerTokenNameExists(identity.OwnerID, name, item.ID); err != nil || exists {
			sendAPIResponseError(w, "CONFLICT", "a token with that name already exists")
			return
		}
		expiresAt, err := parseOwnerTokenExpiry(request.Expiry, request.ExpiresOn, now)
		if err != nil {
			sendAPIResponseError(w, "VALIDATION_FAILED", err.Error())
			return
		}
		if err := db.UpdateLinkedOwnerToken(identity.OwnerID, item.ID, name, expiresAt); err != nil {
			sendAPIResponseError(w, "SERVER_ERROR", "token update failed")
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		sendPrivateAPIResponseOK(w, http.StatusOK, map[string]string{"status": "updated"})
		return
	} else {
		sendAPIResponseError(w, "METHOD_NOT_ALLOWED", "unsupported token action")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	sendPrivateAPIResponseOK(w, http.StatusOK, map[string]interface{}{"token": s.publicToken(item, now), "value": raw})
}

func sendPrivateAPIResponseOK(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	setCacheHeaders(w, "authenticated")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(APIResponse{OK: true, Data: data})
}

func (s *Server) sendBearerError(w http.ResponseWriter, r *http.Request) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="gitignore"`)
		w.Header().Set("Cache-Control", "private, no-store")
		sendAPIResponseError(w, "UNAUTHORIZED", "a primary owner token is required")
		return
	}
	value, parsed := parseBearerHeader(header)
	if parsed {
		_, err := s.AuthenticateBearerToken(value)
		if err == nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="gitignore", error="insufficient_scope"`)
			w.Header().Set("Cache-Control", "private, no-store")
			sendAPIResponseError(w, "FORBIDDEN", "primary owner token required")
			return
		}
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="gitignore", error="invalid_token"`)
	w.Header().Set("Cache-Control", "private, no-store")
	sendAPIResponseError(w, "UNAUTHORIZED", "a valid primary owner token is required")
}

func ownerTokenNameExists(ownerID, name string, except int64) (bool, error) {
	items, err := db.ListOwnerTokens(ownerID)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if item.Kind == "linked" && item.ID != except && strings.EqualFold(item.Name.String, name) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) logTokenAudit(event, ownerID, prefix, source, ip, operation string, success bool) {
	if s.config == nil || s.config.Paths == nil {
		return
	}
	result, severity := "failure", "warn"
	if success {
		result, severity = "success", "info"
	}
	details := map[string]any{}
	if ownerID != "" {
		details["owner_id"] = ownerID
	}
	if prefix != "" {
		details["token_prefix"] = safeSecurityLogField(prefix)
	}
	if operation != "" {
		details["source"] = operation
	}
	actor := audit.Actor{Type: "owner", ID: ownerID, IP: safeSecurityLogField(ip)}
	if ownerID == "" {
		actor.Type = "anonymous"
	}
	_ = audit.Append(s.config.Paths.GetLogsDir(), audit.Entry{
		Event: event, Category: "security", Severity: severity,
		Actor: actor, Details: details, Result: result, Source: source,
	})
}
