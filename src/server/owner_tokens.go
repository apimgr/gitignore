package server

import (
	"crypto/hmac"
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
	"time"

	"github.com/apimgr/gitignore/src/common/audit"
	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/apimgr/gitignore/src/db"
	"github.com/go-chi/chi/v5"
)

const ownerTokenCookieName = "gitignore_owner_token_ksqmkkbi"

const ownerTokenViewCookieName = "gitignore_token_view"

const legacyOwnerTokenCookieName = "gitignore_owner_token_5qvQQyc5"

var tokenAlphabet = []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")

var errInvalidBearerToken = errors.New("invalid bearer token")

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
		_ = db.SetOwnerTokenLast4IfEmpty(candidate.ID, raw[len(raw)-4:])
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
		cookies := ownerCookies(r)
		var valid *http.Cookie
		for _, cookie := range cookies {
			if _, authErr := s.AuthenticateBearerToken(cookie.Value); authErr == nil {
				valid = cookie
				break
			}
		}
		if valid != nil {
			s.setOwnerCookie(w, r, valid.Value)
		} else if len(cookies) > 0 {
			s.clearOwnerCookie(w, r)
		}
		next.ServeHTTP(w, r)
	})
}

func ownerCookies(r *http.Request) []*http.Cookie {
	cookies := make([]*http.Cookie, 0, 2)
	if cookie, err := r.Cookie(ownerTokenCookieName); err == nil {
		cookies = append(cookies, cookie)
	}
	if cookie, err := r.Cookie(legacyOwnerTokenCookieName); err == nil {
		cookies = append(cookies, cookie)
	}
	return cookies
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
	lastErr := errInvalidBearerToken
	for _, cookie := range ownerCookies(r) {
		identity, err := s.AuthenticateBearerToken(cookie.Value)
		if err == nil {
			return identity, nil
		}
		lastErr = err
	}
	return TokenIdentity{}, lastErr
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
		for _, cookie := range ownerCookies(r) {
			cookieIdentity, err := s.AuthenticateBearerToken(cookie.Value)
			if err == nil && cookieIdentity.OwnerID == identity.OwnerID && cookieIdentity.TokenID == identity.TokenID {
				s.setOwnerCookie(w, r, cookie.Value)
				break
			}
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
		plaintext, rotated, rotationErr := db.DecryptOwnerTokenDuringRotation(encoded, s.currentConfig().Server.Security.EncryptionKey)
		if rotationErr != nil || !rotated {
			return "", err
		}
		return string(plaintext), nil
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
	item, err := db.CreateOwnerWithPrimary(ownerID, hash, prefix, raw[len(raw)-4:], encoded, now)
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
	item, err := db.CreateLinkedOwnerToken(ownerID, name, hash, prefix, raw[len(raw)-4:], encoded, expiry, s.maxLinkedTokens(), now)
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
	linkedActive, err := db.CountActiveLinkedTokens(ownerID, now)
	if err != nil {
		return nil, err
	}
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
			last4, err := db.OwnerTokenLast4(ownerID)
			if err != nil {
				return nil, err
			}
			row["token_last4"] = last4
			primary = &row
		} else {
			linked = append(linked, row)
		}
	}
	return map[string]interface{}{"authenticated": true, "primary": primary, "linked": linked, "linkedCount": linkedActive, "message": message}, nil
}

func (s *Server) handleOwnerTokensPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	var cookie *http.Cookie
	identity, err := TokenIdentity{}, errInvalidBearerToken
	for _, candidate := range ownerCookies(r) {
		if candidateIdentity, candidateErr := s.AuthenticateBearerToken(candidate.Value); candidateErr == nil && candidateIdentity.Kind == "primary" {
			cookie, identity, err = candidate, candidateIdentity, nil
			break
		}
	}
	if err != nil || identity.Kind != "primary" {
		clearOwnerTokenViewCookie(s, w, r)
		if len(ownerCookies(r)) > 0 {
			s.clearOwnerCookie(w, r)
		}
		s.renderPage(w, r, "preferences_tokens", PageData{Title: i18n.T(r, "owner_tokens_page.title"), Data: map[string]interface{}{"authenticated": false}})
		return
	}
	s.setOwnerCookie(w, r, cookie.Value)
	card := readOwnerTokenViewCookie(s, w, r, identity.OwnerID)
	var revealed map[int64]string
	if card != "" {
		items, err := db.ListOwnerTokens(identity.OwnerID)
		if err != nil {
			s.renderPageStatus(w, r, "error", http.StatusServiceUnavailable, PageData{Title: i18n.T(r, "owner_tokens_page.title"), PrivateCache: true})
			return
		}
		for _, item := range items {
			matches := card == "primary" && item.Kind == "primary"
			if item.Kind == "linked" && strconv.FormatInt(item.ID, 10) == card {
				matches = true
			}
			if !matches {
				continue
			}
			raw, err := s.decryptOwnerToken(item.TokenEnc)
			if err != nil {
				s.renderPageStatus(w, r, "error", http.StatusServiceUnavailable, PageData{Title: i18n.T(r, "owner_tokens_page.title"), PrivateCache: true})
				return
			}
			revealed = map[int64]string{item.ID: raw}
			break
		}
	}
	data, err := s.loadTokenPage(identity.OwnerID, revealed, "")
	if err != nil {
		s.renderPageStatus(w, r, "error", http.StatusServiceUnavailable, PageData{Title: i18n.T(r, "owner_tokens_page.title"), Data: map[string]interface{}{"message": i18n.T(r, "errors.server_error")}, PrivateCache: true})
		return
	}
	data["csrfToken"] = ensureCSRFTokenWithSecure(w, r, preferenceCookieSecure(s, r))
	s.renderPage(w, r, "preferences_tokens", PageData{Title: i18n.T(r, "owner_tokens_page.title"), Data: data, PrivateCache: true})
}

func (s *Server) setOwnerCookie(w http.ResponseWriter, r *http.Request, raw string) {
	maxAge := 365 * 24 * 60 * 60
	if s.config != nil && s.currentConfig() != nil {
		maxAge = parseCookieMaxAge(s.currentConfig().Server.Tokens.CookieMaxAge)
	}
	http.SetCookie(w, &http.Cookie{Name: ownerTokenCookieName, Value: raw, Path: "/", HttpOnly: true, Secure: preferenceCookieSecure(s, r), SameSite: http.SameSiteStrictMode, MaxAge: maxAge, Expires: time.Now().Add(time.Duration(maxAge) * time.Second)})
	clearCookie(w, r, legacyOwnerTokenCookieName, s)
}

func setOwnerTokenViewCookie(s *Server, w http.ResponseWriter, r *http.Request, ownerID, card string) error {
	if !validOwnerTokenViewCard(card) {
		return errors.New("invalid token view card")
	}
	secret, err := db.ApplicationSecret("cookie_signing_key")
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(ownerID + "|" + card))
	value := card + "." + hex.EncodeToString(mac.Sum(nil))
	http.SetCookie(w, &http.Cookie{
		Name: ownerTokenViewCookieName, Value: value, Path: "/server/preferences/tokens", MaxAge: 60,
		HttpOnly: true, Secure: preferenceCookieSecure(s, r), SameSite: http.SameSiteStrictMode,
	})
	return nil
}

func readOwnerTokenViewCookie(s *Server, w http.ResponseWriter, r *http.Request, ownerID string) string {
	cookie, err := r.Cookie(ownerTokenViewCookieName)
	if err != nil {
		return ""
	}
	clearOwnerTokenViewCookie(s, w, r)
	card, suppliedHex, ok := strings.Cut(cookie.Value, ".")
	if !ok || !validOwnerTokenViewCard(card) {
		return ""
	}
	supplied, err := hex.DecodeString(suppliedHex)
	if err != nil || len(supplied) != sha256.Size {
		return ""
	}
	secrets, err := db.ApplicationSecretCandidates("cookie_signing_key", time.Now().UTC())
	if err != nil {
		return ""
	}
	for _, secret := range secrets {
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write([]byte(ownerID + "|" + card))
		if hmac.Equal(supplied, mac.Sum(nil)) {
			return card
		}
	}
	return ""
}

func clearOwnerTokenViewCookie(s *Server, w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: ownerTokenViewCookieName, Value: "", Path: "/server/preferences/tokens", MaxAge: -1,
		Expires: time.Unix(1, 0), HttpOnly: true, Secure: preferenceCookieSecure(s, r), SameSite: http.SameSiteStrictMode,
	})
}

func validOwnerTokenViewCard(card string) bool {
	if card == "primary" {
		return true
	}
	id, err := strconv.ParseInt(card, 10, 64)
	return err == nil && id > 0 && strconv.FormatInt(id, 10) == card
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
	clearCookie(w, r, ownerTokenCookieName, s)
	clearCookie(w, r, legacyOwnerTokenCookieName, s)
}

func clearCookie(w http.ResponseWriter, r *http.Request, name string, s *Server) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", HttpOnly: true, Secure: preferenceCookieSecure(s, r), SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}

func (s *Server) tokenActionLimiter(w http.ResponseWriter, r *http.Request) bool {
	if !isStrictOwnerTokenAction(r) {
		return true
	}
	identity := "ip:" + s.clientIP(r)
	if owner, err := s.authenticateRequest(r, true); err == nil {
		identity = "owner:" + owner.OwnerID
	}
	if s.strictTokenLimiter != nil && !s.strictTokenLimiter.allowKey(identity) {
		seconds := s.strictTokenLimiter.retryAfter(identity)
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		if strings.HasPrefix(r.URL.Path, "/api/") {
			sendAPIResponseErrorLocalized(w, r, "RATE_LIMITED")
		} else {
			s.renderPageStatus(w, r, "preferences_tokens", http.StatusTooManyRequests, PageData{Title: i18n.T(r, "owner_tokens_page.title"), Data: map[string]interface{}{"authenticated": false, "message": i18n.T(r, "errors.rate_limited")}})
		}
		return false
	}
	return true
}

func isStrictOwnerTokenAction(r *http.Request) bool {
	path := strings.TrimSuffix(r.URL.Path, "/")
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		return false
	}
	if strings.HasPrefix(path, "/server/preferences/") {
		if path == "/server/preferences/token/reveal" ||
			path == "/server/preferences/token/rotate" ||
			path == "/server/preferences/token/restore" ||
			path == "/server/preferences/token/create" ||
			path == "/server/preferences/token/revoke" {
			return true
		}
	}
	if strings.Contains(path, "/tokens/primary") {
		return path == "/api/v1/server/preferences/tokens/primary" ||
			strings.HasSuffix(path, "/tokens/primary") ||
			strings.HasSuffix(path, "/tokens/primary/reveal") ||
			strings.HasSuffix(path, "/tokens/primary/rotate")
	}
	parts := strings.Split(path, "/")
	for index, part := range parts {
		if part != "tokens" || len(parts)-index != 3 || parts[index+1] == "" {
			continue
		}
		if parts[index+2] == "reveal" || parts[index+2] == "rotate" {
			return true
		}
	}
	return strings.HasSuffix(path, "/tokens/reveal") || strings.HasSuffix(path, "/tokens/rotate")
}

func (s *Server) handleIssueOwnerTokenWeb(w http.ResponseWriter, r *http.Request) {
	if !s.tokenActionLimiter(w, r) {
		return
	}
	if !validCSRFRequest(r) {
		sendAPIResponseErrorLocalized(w, r, "CSRF_FAILED")
		return
	}
	if _, err := s.authenticateRequest(r, true); err == nil {
		s.renderPageStatus(w, r, "preferences_tokens", http.StatusConflict, PageData{Title: i18n.T(r, "owner_tokens_page.title"), Data: map[string]interface{}{"authenticated": false, "message": i18n.T(r, "errors.conflict")}})
		return
	}
	raw, item, err := s.issueOwnerToken(time.Now(), "web", s.clientIP(r))
	if err != nil {
		s.renderPageStatus(w, r, "preferences_tokens", http.StatusServiceUnavailable, PageData{Title: i18n.T(r, "owner_tokens_page.title"), Data: map[string]interface{}{"authenticated": false, "message": i18n.T(r, "errors.server_error")}})
		return
	}
	s.setOwnerCookie(w, r, raw)
	if err := setOwnerTokenViewCookie(s, w, r, item.OwnerID, "primary"); err != nil {
		s.renderPageStatus(w, r, "preferences_tokens", http.StatusServiceUnavailable, PageData{Title: i18n.T(r, "owner_tokens_page.title")})
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	redirectOwnerTokenPage(w, r, "primary")
}

func (s *Server) handleRestoreOwnerToken(w http.ResponseWriter, r *http.Request) {
	if !s.tokenActionLimiter(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if !validCSRFRequest(r) {
		sendAPIResponseErrorLocalized(w, r, "CSRF_FAILED")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderPageStatus(w, r, "preferences_tokens", http.StatusBadRequest, PageData{Title: i18n.T(r, "owner_tokens_page.title"), Data: map[string]interface{}{"authenticated": false, "message": i18n.T(r, "errors.token_invalid")}})
		return
	}
	raw := strings.TrimSpace(r.FormValue("token"))
	identity, err := s.AuthenticateBearerToken(raw)
	if err != nil || identity.Kind != "primary" {
		ip := s.clientIP(r)
		if s.recordAuthFailure(ip) {
			s.rejectRateLimited(w, r, s.rateLimiters["auth_failures"], "ip:"+ip)
			return
		}
		s.logTokenAudit("token.restore_failed", "", "", "web", s.clientIP(r), "", false)
		s.renderPageStatus(w, r, "preferences_tokens", http.StatusUnauthorized, PageData{Title: i18n.T(r, "owner_tokens_page.title"), Data: map[string]interface{}{"authenticated": false, "message": i18n.T(r, "errors.token_invalid")}})
		return
	}
	s.setOwnerCookie(w, r, raw)
	s.logTokenAudit("token.restored", identity.OwnerID, identity.TokenPrefix, "web", s.clientIP(r), "", true)
	clearOwnerTokenViewCookie(s, w, r)
	w.Header().Set("Cache-Control", "private, no-store")
	http.Redirect(w, r, "/server/preferences/tokens#primary", http.StatusSeeOther)
}

func (s *Server) handleForgetOwnerToken(w http.ResponseWriter, r *http.Request) {
	if !validCSRFRequest(r) {
		sendAPIResponseErrorLocalized(w, r, "CSRF_FAILED")
		return
	}
	s.clearOwnerCookie(w, r)
	w.Header().Set("Clear-Site-Data", `"storage"`)
	w.Header().Set("Cache-Control", "private, no-store")
	http.Redirect(w, r, "/server/preferences/tokens", http.StatusSeeOther)
}

func (s *Server) handleRevokePrimaryOwnerTokenWeb(w http.ResponseWriter, r *http.Request) {
	if !s.tokenActionLimiter(w, r) {
		return
	}
	if !validCSRFRequest(r) {
		sendAPIResponseErrorLocalized(w, r, "CSRF_FAILED")
		return
	}
	identity, ok := s.requirePrimaryOwner(w, r, true)
	if !ok {
		sendAPIResponseErrorLocalized(w, r, "FORBIDDEN")
		return
	}
	if err := r.ParseForm(); err != nil {
		sendAPIResponseErrorLocalized(w, r, "BAD_REQUEST")
		return
	}
	last4, err := db.OwnerTokenLast4(identity.OwnerID)
	if err != nil || len(last4) != 4 || subtle.ConstantTimeCompare([]byte(r.FormValue("confirm")), []byte(last4)) != 1 {
		sendAPIResponseErrorLocalized(w, r, "VALIDATION_FAILED")
		return
	}
	deleted, err := db.DeleteOwner(identity.OwnerID)
	if err != nil {
		sendAPIResponseErrorLocalized(w, r, "SERVER_ERROR")
		return
	}
	s.logOwnerDeletionAudit(identity.OwnerID, "web", deleted, 0, s.clientIP(r))
	s.clearOwnerCookie(w, r)
	w.Header().Set("Clear-Site-Data", `"storage"`)
	w.Header().Set("Cache-Control", "private, no-store")
	http.Redirect(w, r, "/server/preferences/tokens", http.StatusSeeOther)
}

func (s *Server) handleAPIPrimaryOwnerRevoke(w http.ResponseWriter, r *http.Request) {
	if !s.tokenActionLimiter(w, r) {
		return
	}
	identity, ok := s.requirePrimaryOwner(w, r, false)
	if !ok {
		s.sendBearerError(w, r)
		return
	}
	var request struct {
		Confirm string `json:"confirm"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
		sendAPIResponseErrorLocalized(w, r, "VALIDATION_FAILED")
		return
	}
	last4, err := db.OwnerTokenLast4(identity.OwnerID)
	if err != nil || len(last4) != 4 || subtle.ConstantTimeCompare([]byte(request.Confirm), []byte(last4)) != 1 {
		sendAPIResponseErrorLocalized(w, r, "VALIDATION_FAILED")
		return
	}
	deleted, err := db.DeleteOwner(identity.OwnerID)
	if err != nil {
		sendAPIResponseErrorLocalized(w, r, "SERVER_ERROR")
		return
	}
	s.logOwnerDeletionAudit(identity.OwnerID, "api", deleted, 0, s.clientIP(r))
	w.Header().Set("Cache-Control", "private, no-store")
	sendPrivateAPIResponseOK(w, http.StatusOK, map[string]int64{"tokens": deleted, "resources": 0})
}

func (s *Server) logOwnerDeletionAudit(ownerID, source string, tokens, resources int64, ip string) {
	if s.config == nil || s.config.Paths == nil {
		return
	}
	_ = audit.Append(s.config.Paths.GetLogsDir(), audit.Entry{
		Event: "token.revoked", Category: "security", Severity: "info",
		Actor:   audit.Actor{Type: "owner", ID: ownerID, IP: safeSecurityLogField(ip)},
		Target:  &audit.Target{Type: "owner", ID: ownerID},
		Details: map[string]any{"scope": "owner", "tokens_deleted": tokens, "resources_deleted": resources, "source": source},
		Result:  "success", Source: source,
	})
}

func (s *Server) handleOwnerTokenWebAction(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	}
	if !s.tokenActionLimiter(w, r) {
		return
	}
	if !validCSRFRequest(r) {
		sendAPIResponseErrorLocalized(w, r, "CSRF_FAILED")
		return
	}
	identity, ok := s.requirePrimaryOwner(w, r, true)
	if !ok {
		sendAPIResponseErrorLocalized(w, r, "FORBIDDEN")
		return
	}
	if err := r.ParseForm(); err != nil {
		sendAPIResponseErrorLocalized(w, r, "BAD_REQUEST")
		return
	}
	pathID := chi.URLParam(r, "id")
	tokenID, _ := strconv.ParseInt(pathID, 10, 64)
	var target db.OwnerToken
	if strings.Contains(r.URL.Path, "/tokens/") && tokenID > 0 {
		var err error
		target, err = db.GetOwnerToken(identity.OwnerID, tokenID)
		if err != nil || target.Kind != "linked" {
			sendAPIResponseErrorLocalized(w, r, "NOT_FOUND")
			return
		}
	} else {
		target, _ = db.GetOwnerToken(identity.OwnerID, identity.TokenID)
	}
	now := time.Now().UTC()
	var raw string
	switch {
	case strings.HasSuffix(r.URL.Path, "/reveal"):
		raw, _ = s.decryptOwnerToken(target.TokenEnc)
		if raw == "" {
			sendAPIResponseErrorLocalized(w, r, "SERVER_ERROR")
			return
		}
		s.logTokenAudit("token.revealed", identity.OwnerID, target.TokenPrefix, "web", s.clientIP(r), "", true)
	case strings.HasSuffix(r.URL.Path, "/rotate"):
		raw, _ = newOwnerToken()
		encoded, err := s.encryptOwnerToken(raw)
		if err != nil {
			sendAPIResponseErrorLocalized(w, r, "SERVER_ERROR")
			return
		}
		hash, prefix := hashOwnerToken(raw)
		if err := db.RotateOwnerToken(identity.OwnerID, target.ID, hash, prefix, raw[len(raw)-4:], encoded, now); err != nil {
			sendAPIResponseErrorLocalized(w, r, "SERVER_ERROR")
			return
		}
		if target.Kind == "primary" {
			s.setOwnerCookie(w, r, raw)
		}
		s.logTokenAudit("token.rotated", identity.OwnerID, target.TokenPrefix, "web", s.clientIP(r), "", true)
	case strings.HasSuffix(r.URL.Path, "/update"):
		name := strings.TrimSpace(r.FormValue("name"))
		if len(name) < 1 || len(name) > 64 {
			sendAPIResponseErrorLocalized(w, r, "VALIDATION_FAILED")
			return
		}
		expiresAt, err := parseOwnerTokenExpiry(r.FormValue("expiry"), r.FormValue("expires_on"), now)
		if err != nil {
			sendAPIResponseErrorLocalized(w, r, "VALIDATION_FAILED")
			return
		}
		if err := db.UpdateLinkedOwnerToken(identity.OwnerID, target.ID, name, expiresAt); err != nil {
			sendAPIResponseErrorLocalized(w, r, "SERVER_ERROR")
			return
		}
	case strings.HasSuffix(r.URL.Path, "/revoke"):
		if err := db.RevokeLinkedOwnerToken(identity.OwnerID, target.ID, now); err != nil {
			sendAPIResponseErrorLocalized(w, r, "NOT_FOUND")
			return
		}
		s.logTokenAudit("token.revoked", identity.OwnerID, target.TokenPrefix, "web", s.clientIP(r), "", true)
	default:
		sendAPIResponseErrorLocalized(w, r, "NOT_FOUND")
		return
	}
	if raw != "" {
		card := strconv.FormatInt(target.ID, 10)
		if target.Kind == "primary" {
			card = "primary"
		}
		if err := setOwnerTokenViewCookie(s, w, r, identity.OwnerID, card); err != nil {
			sendAPIResponseErrorLocalized(w, r, "SERVER_ERROR")
			return
		}
	}
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
		sendAPIResponseErrorLocalized(w, r, "CSRF_FAILED")
		return
	}
	identity, ok := s.requirePrimaryOwner(w, r, true)
	if !ok {
		sendAPIResponseErrorLocalized(w, r, "FORBIDDEN")
		return
	}
	if err := r.ParseForm(); err != nil {
		sendAPIResponseErrorLocalized(w, r, "BAD_REQUEST")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if len(name) < 1 || len(name) > 64 {
		sendAPIResponseErrorLocalized(w, r, "VALIDATION_FAILED")
		return
	}
	if exists, err := ownerTokenNameExists(identity.OwnerID, name, 0); err != nil || exists {
		sendAPIResponseErrorLocalized(w, r, "CONFLICT")
		return
	}
	count, err := db.CountActiveLinkedTokens(identity.OwnerID, time.Now())
	if err != nil {
		s.renderPageStatus(w, r, "error", http.StatusServiceUnavailable, PageData{Title: i18n.T(r, "owner_tokens_page.title")})
		return
	}
	if count >= s.maxLinkedTokens() {
		sendAPIResponseErrorLocalized(w, r, "CONFLICT")
		return
	}
	expiresAt, err := parseOwnerTokenExpiry(r.FormValue("expiry"), r.FormValue("expires_on"), time.Now())
	if err != nil {
		sendAPIResponseErrorLocalized(w, r, "VALIDATION_FAILED")
		return
	}
	_, item, err := s.issueLinkedToken(identity.OwnerID, name, expiresAt, time.Now(), "web", s.clientIP(r))
	if err != nil {
		sendAPIResponseErrorLocalized(w, r, "SERVER_ERROR")
		return
	}
	if err := setOwnerTokenViewCookie(s, w, r, identity.OwnerID, strconv.FormatInt(item.ID, 10)); err != nil {
		sendAPIResponseErrorLocalized(w, r, "SERVER_ERROR")
		return
	}
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
			sendAPIResponseErrorLocalized(w, r, "SERVER_ERROR")
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
		sendAPIResponseErrorLocalized(w, r, "BAD_REQUEST")
		return
	}
	name := strings.TrimSpace(request.Name)
	if len(name) < 1 || len(name) > 64 {
		sendAPIResponseErrorLocalized(w, r, "VALIDATION_FAILED")
		return
	}
	if exists, err := ownerTokenNameExists(identity.OwnerID, name, 0); err != nil || exists {
		sendAPIResponseErrorLocalized(w, r, "CONFLICT")
		return
	}
	count, err := db.CountActiveLinkedTokens(identity.OwnerID, time.Now())
	if err != nil {
		sendAPIResponseErrorLocalized(w, r, "SERVER_ERROR")
		return
	}
	if count >= s.maxLinkedTokens() {
		sendAPIResponseErrorLocalized(w, r, "CONFLICT")
		return
	}
	expiresAt, err := parseOwnerTokenExpiry(request.Expiry, request.ExpiresOn, time.Now())
	if err != nil {
		sendAPIResponseErrorLocalized(w, r, "VALIDATION_FAILED")
		return
	}
	raw, item, err := s.issueLinkedToken(identity.OwnerID, name, expiresAt, time.Now(), "api", s.clientIP(r))
	if err != nil {
		sendAPIResponseErrorLocalized(w, r, "SERVER_ERROR")
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
			sendAPIResponseErrorLocalized(w, r, "CONFLICT")
			return
		} else {
			s.sendBearerError(w, r)
			return
		}
	}
	token, item, err := s.issueOwnerToken(time.Now(), "api", s.clientIP(r))
	if err != nil {
		sendAPIResponseErrorLocalized(w, r, "SERVER_ERROR")
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
		sendAPIResponseErrorLocalized(w, r, "NOT_FOUND")
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
			sendAPIResponseErrorLocalized(w, r, "SERVER_ERROR")
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
				err = db.RotateOwnerToken(identity.OwnerID, item.ID, hash, prefix, raw[len(raw)-4:], encoded, now)
			}
		}
		if err != nil {
			sendAPIResponseErrorLocalized(w, r, "SERVER_ERROR")
			return
		}
		s.logTokenAudit("token.rotated", identity.OwnerID, item.TokenPrefix, "api", s.clientIP(r), "", true)
	} else if r.Method == http.MethodDelete || strings.HasSuffix(r.URL.Path, "/revoke") {
		if err := db.RevokeLinkedOwnerToken(identity.OwnerID, item.ID, now); err != nil {
			sendAPIResponseErrorLocalized(w, r, "NOT_FOUND")
			return
		}
		s.logTokenAudit("token.revoked", identity.OwnerID, item.TokenPrefix, "api", s.clientIP(r), "", true)
		w.Header().Set("Cache-Control", "private, no-store")
		sendPrivateAPIResponseOK(w, http.StatusOK, map[string]string{"status": "revoked"})
		return
	} else if r.Method == http.MethodPatch {
		request, err := decodeOwnerTokenRequest(w, r)
		if err != nil {
			sendAPIResponseErrorLocalized(w, r, "BAD_REQUEST")
			return
		}
		name := strings.TrimSpace(request.Name)
		if len(name) < 1 || len(name) > 64 {
			sendAPIResponseErrorLocalized(w, r, "VALIDATION_FAILED")
			return
		}
		if exists, err := ownerTokenNameExists(identity.OwnerID, name, item.ID); err != nil || exists {
			sendAPIResponseErrorLocalized(w, r, "CONFLICT")
			return
		}
		expiresAt, err := parseOwnerTokenExpiry(request.Expiry, request.ExpiresOn, now)
		if err != nil {
			sendAPIResponseErrorLocalized(w, r, "VALIDATION_FAILED")
			return
		}
		if err := db.UpdateLinkedOwnerToken(identity.OwnerID, item.ID, name, expiresAt); err != nil {
			sendAPIResponseErrorLocalized(w, r, "SERVER_ERROR")
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		sendPrivateAPIResponseOK(w, http.StatusOK, map[string]string{"status": "updated"})
		return
	} else {
		sendAPIResponseErrorLocalized(w, r, "METHOD_NOT_ALLOWED")
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
		sendAPIResponseErrorLocalized(w, r, "UNAUTHORIZED")
		return
	}
	value, parsed := parseBearerHeader(header)
	if parsed {
		_, err := s.AuthenticateBearerToken(value)
		if err == nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="gitignore", error="insufficient_scope"`)
			w.Header().Set("Cache-Control", "private, no-store")
			sendAPIResponseErrorLocalized(w, r, "FORBIDDEN")
			return
		}
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="gitignore", error="invalid_token"`)
	w.Header().Set("Cache-Control", "private, no-store")
	sendAPIResponseErrorLocalized(w, r, "UNAUTHORIZED")
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
