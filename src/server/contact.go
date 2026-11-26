package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/notify"
	"github.com/apimgr/gitignore/src/pgp"
)

type contactChallenge struct {
	answerHash [sha256.Size]byte
	ip         string
	expires    time.Time
}

func (s *Server) handleContactSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.contactPageEnabled() {
		if isVersionedAPIContact(s, r) {
			sendAPIResponseError(w, "NOT_FOUND", requestTranslation(r, "contact.runtime.form_not_found"))
			return
		}
		s.handleNotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
	var formErr error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var fields map[string]interface{}
		formErr = json.NewDecoder(r.Body).Decode(&fields)
		if formErr == nil {
			r.Form = make(url.Values)
			for key, value := range fields {
				r.Form.Set(key, fmt.Sprint(value))
			}
		}
	} else {
		formErr = r.ParseForm()
	}
	if formErr != nil {
		s.renderContact(w, r, http.StatusBadRequest, map[string]interface{}{"error": requestTranslation(r, "contact.runtime.form_too_large_or_invalid")})
		return
	}
	securityToken := strings.TrimSpace(r.FormValue("security_id"))
	typeName := strings.TrimSpace(r.FormValue("type"))
	if securityToken != "" && !validSecurityID(securityToken, time.Now()) {
		s.logInvalidSecurityID(r, securityToken)
	}
	if !validContactType(typeName, securityToken) {
		s.renderContact(w, r, http.StatusBadRequest, map[string]interface{}{"error": requestTranslation(r, "contact.runtime.invalid_type")})
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	address := strings.TrimSpace(r.FormValue("email"))
	parsedAddress, addressErr := mail.ParseAddress(address)
	if name == "" || len([]rune(name)) > 100 || addressErr != nil || parsedAddress.Address != address || len(address) > 254 || hasHeaderControl(name) || hasHeaderControl(address) || hasHeaderControl(r.FormValue("subject")) || !validContactText(allContactValues(r)) {
		s.renderContact(w, r, http.StatusBadRequest, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.name_email_required")))
		return
	}
	if r.FormValue("website") != "" {
		s.contactSuccess(w, r, typeName, "", "")
		return
	}
	if !s.contactOriginValid(r) {
		s.renderContact(w, r, http.StatusBadRequest, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.same_site_required")))
		return
	}
	step := r.FormValue("step")
	if step == "" {
		step = "submit"
	}
	if step == "edit" {
		s.renderContact(w, r, http.StatusOK, contactFormValues(r, securityToken, ""))
		return
	}
	if err := validateContactFields(r, typeName, i18n.LangFromContext(r.Context())); err != nil {
		s.renderContact(w, r, http.StatusBadRequest, contactFormValues(r, securityToken, err.Error()))
		return
	}
	if step == "submit" {
		if !s.verifyFormToken(r.FormValue("form_token"), time.Now()) || !s.verifyContactCaptcha(r) {
			s.renderContact(w, r, http.StatusBadRequest, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.verification_answer_invalid")))
			return
		}
		reviewToken, err := s.issueReviewToken(r.Form, time.Now())
		if err != nil {
			s.renderContact(w, r, http.StatusServiceUnavailable, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.verification_unavailable")))
			return
		}
		if isVersionedAPIContact(s, r) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			sendAPIResponseOK(w, map[string]interface{}{"review": map[string]string{"type": typeName, "email": address, "subject": r.FormValue("subject")}, "review_token": reviewToken, "confirm_after_seconds": 2})
			return
		}
		values := contactFormValues(r, securityToken, "")
		values["review"] = true
		values["reviewToken"] = reviewToken
		values["type"] = typeName
		s.renderContact(w, r, http.StatusOK, values)
		return
	}
	if step != "confirm" || !formChecked(r.FormValue("confirmed")) || !s.consumeReviewToken(r.FormValue("review_token"), r.Form, time.Now()) {
		values := contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.confirmation_required"))
		values["review"] = true
		values["type"] = typeName
		s.renderContact(w, r, http.StatusBadRequest, values)
		return
	}
	if typeName == "security" {
		s.handleSecurityReportSubmit(w, r, name, address, securityToken)
		return
	}
	recipient := s.contactRecipientForType(typeName)
	webhookTargets := s.contactWebhookTargets(typeName)
	webhooksAvailable := s.config.WebhookDispatcher != nil && len(webhookTargets) > 0
	if (recipient == "" || s.config.SendContactReply == nil) && !webhooksAvailable {
		s.renderContact(w, r, http.StatusServiceUnavailable, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.delivery_unconfigured")))
		return
	}
	message := contactMailBody(r.Form, typeName, i18n.LangFromContext(r.Context()))
	appName := "GitIgnore"
	if s.currentConfig() != nil && strings.TrimSpace(s.currentConfig().Server.Branding.Title) != "" {
		appName = s.currentConfig().Server.Branding.Title
	}
	contactLabel := i18n.Translate(i18n.LangFromContext(r.Context()), "contact.title")
	subject := "[" + appName + " " + contactLabel + "] " + r.FormValue("subject")
	var emailErr error
	if recipient != "" && s.config.SendContactReply != nil {
		emailErr = s.config.SendContactReply(recipient, address, subject, message)
	}
	if webhooksAvailable {
		s.dispatchContactWebhook(r, typeName, subject, message, "informational", "")
	}
	if emailErr != nil && !webhooksAvailable {
		s.renderContact(w, r, http.StatusServiceUnavailable, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.delivery_failed")))
		return
	}
	s.contactSuccess(w, r, typeName, "", "")
}

func isVersionedAPIContact(s *Server, r *http.Request) bool {
	return r.URL.Path == s.apiBasePath()+"/server/contact"
}

func (s *Server) handleAPIContact(w http.ResponseWriter, r *http.Request) {
	if !s.contactPageEnabled() {
		sendAPIResponseError(w, "NOT_FOUND", requestTranslation(r, "contact.runtime.form_not_found"))
		return
	}
	token, err := issueContactFormToken(time.Now())
	if err != nil {
		sendAPIResponseError(w, "SERVER_ERROR", requestTranslation(r, "contact.runtime.api_form_unavailable"))
		return
	}
	challengeID, question, err := s.issueContactChallenge(r)
	if err != nil {
		sendAPIResponseError(w, "RATE_LIMITED", requestTranslation(r, "contact.runtime.api_challenge_unavailable"))
		return
	}
	types := []string{"abuse", "dmca", "privacy"}
	if validSecurityID(r.URL.Query().Get("security_id"), time.Now()) {
		types = append(types, "security")
	}
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	sendAPIResponseOK(w, map[string]interface{}{
		"types":        types,
		"form_token":   token,
		"security_url": "/server/security",
		"abuse_email":  s.responseContactEmail(r),
		"captcha":      contactCaptchaDescriptor(s.contactCaptchaProviderForRequest(r), challengeID, question),
		"fields":       contactFieldDescriptors(),
	})
}

func contactCaptchaDescriptor(provider, challengeID, question string) map[string]interface{} {
	fallback := map[string]interface{}{"provider": "simple", "captcha_id": challengeID, "question": question}
	descriptor := map[string]interface{}{"provider": provider}
	if provider == "simple" {
		descriptor["captcha_id"], descriptor["question"] = challengeID, question
	} else {
		descriptor["fallback"] = fallback
	}
	return descriptor
}

func contactFieldDescriptors() []map[string]interface{} {
	return []map[string]interface{}{
		{"name": "name", "type": "text", "required": true, "max_length": 100},
		{"name": "email", "type": "email", "required": true, "max_length": 254},
		{"name": "reported_url", "type": "url", "required": false, "max_length": 2048, "required_when": map[string]interface{}{"field": "type", "in": []string{"abuse", "dmca"}}},
		{"name": "reason", "type": "enum", "required": false, "max_length": nil, "required_when": map[string]interface{}{"field": "type", "equals": "abuse"}},
		{"name": "copyrighted_work", "type": "textarea", "required": false, "max_length": 2000, "required_when": map[string]interface{}{"field": "type", "equals": "dmca"}},
		{"name": "good_faith", "type": "checkbox", "required": false, "max_length": nil, "required_when": map[string]interface{}{"field": "type", "equals": "dmca"}},
		{"name": "accuracy", "type": "checkbox", "required": false, "max_length": nil, "required_when": map[string]interface{}{"field": "type", "equals": "dmca"}},
		{"name": "signature", "type": "text", "required": false, "max_length": 100, "required_when": map[string]interface{}{"field": "type", "equals": "dmca"}},
		{"name": "request_kind", "type": "enum", "required": false, "max_length": nil, "required_when": map[string]interface{}{"field": "type", "equals": "privacy"}},
		{"name": "subject", "type": "text", "required": true, "max_length": 200},
		{"name": "message", "type": "textarea", "required": true, "max_length": 5000},
		{"name": "summary", "type": "text", "required": true, "max_length": 200, "required_when": map[string]interface{}{"field": "type", "equals": "security"}},
		{"name": "component", "type": "text", "required": true, "max_length": 255, "required_when": map[string]interface{}{"field": "type", "equals": "security"}},
		{"name": "endpoint", "type": "text", "required": false, "max_length": 2048},
		{"name": "severity", "type": "enum", "required": true, "max_length": nil, "required_when": map[string]interface{}{"field": "type", "equals": "security"}},
		{"name": "reproduction", "type": "textarea", "required": true, "max_length": 10000, "required_when": map[string]interface{}{"field": "type", "equals": "security"}},
		{"name": "impact", "type": "textarea", "required": true, "max_length": 5000, "required_when": map[string]interface{}{"field": "type", "equals": "security"}},
		{"name": "suggested_fix", "type": "textarea", "required": false, "max_length": 5000},
		{"name": "researcher_gpg", "type": "textarea", "required": false, "max_length": 16384, "max_length_when": []map[string]interface{}{{"prefix": "https://", "max_length": 2048}}},
		{"name": "cve_requested", "type": "checkbox", "required": false, "max_length": nil},
		{"name": "disclosure_days", "type": "number", "required": false, "max_length": nil},
		{"name": "credit_preference", "type": "enum", "required": true, "max_length": nil, "required_when": map[string]interface{}{"field": "type", "equals": "security"}},
		{"name": "credit_handle", "type": "text", "required": false, "max_length": 100, "required_when": map[string]interface{}{"field": "credit_preference", "equals": "handle"}},
		{"name": "coordinated_disclosure", "type": "checkbox", "required": true, "max_length": nil, "required_when": map[string]interface{}{"field": "type", "equals": "security"}},
	}
}

func validContactType(value, securityID string) bool {
	switch value {
	case "abuse", "dmca", "privacy":
		return true
	case "security":
		return validSecurityID(securityID, time.Now())
	default:
		return false
	}
}

func (s *Server) contactSuccess(w http.ResponseWriter, r *http.Request, typeName, trackingID, message string) {
	if isVersionedAPIContact(s, r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if message == "" {
			message = requestTranslation(r, "contact.runtime.api_success")
		}
		data := map[string]interface{}{"message": message}
		if trackingID != "" {
			data["tracking_id"] = trackingID
		}
		sendAPIResponseOK(w, data)
		return
	}
	location := "/server/contact?type=" + url.QueryEscape(typeName) + "&sent=1"
	if trackingID != "" {
		location += "&report=" + url.QueryEscape(trackingID)
	}
	http.Redirect(w, r, location, http.StatusSeeOther)
}

func hasHeaderControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func validContactText(values []string) bool {
	for _, value := range values {
		if !utf8.ValidString(value) {
			return false
		}
		for _, char := range value {
			switch char {
			case '\u200b', '\u200c', '\u200d', '\ufeff', '\u202a', '\u202b', '\u202c', '\u202d', '\u202e', '\u2066', '\u2067', '\u2068', '\u2069':
				return false
			}
		}
	}
	return true
}

func allContactValues(r *http.Request) []string {
	values := make([]string, 0, len(r.Form))
	for _, valuesForKey := range r.Form {
		values = append(values, valuesForKey...)
	}
	return values
}

func allowedContactFields(typeName string) map[string]bool {
	fields := map[string]bool{"type": true, "name": true, "email": true, "captcha": true,
		"challenge_id": true, "captcha_id": true, "captcha_answer": true, "form_token": true, "website": true, "step": true,
		"review_token": true, "confirmed": true, "security_id": true,
		"g-recaptcha-response": true, "h-captcha-response": true}
	if typeName == "security" {
		for _, key := range []string{"summary", "component", "endpoint", "severity", "reproduction", "impact", "suggested_fix", "researcher_gpg", "cve_requested", "disclosure_days", "credit_preference", "credit_handle", "coordinated_disclosure"} {
			fields[key] = true
		}
	} else {
		fields["subject"], fields["message"] = true, true
		switch typeName {
		case "abuse":
			fields["reported_url"], fields["reason"] = true, true
		case "dmca":
			for _, key := range []string{"reported_url", "copyrighted_work", "good_faith", "accuracy", "signature"} {
				fields[key] = true
			}
		case "privacy":
			fields["request_kind"] = true
		}
	}
	return fields
}

func validateContactFields(r *http.Request, typeName, lang string) error {
	allowed := allowedContactFields(typeName)
	for key := range r.Form {
		if !allowed[key] {
			return errors.New(i18n.Translate(lang, "contact.runtime.validation_unsupported_field"))
		}
	}
	for key, maximum := range map[string]int{"name": 100, "email": 254, "subject": 200, "message": 5000, "reported_url": 2048, "reason": 64, "copyrighted_work": 2000, "signature": 100, "summary": 200, "component": 255, "endpoint": 2048, "severity": 16, "reproduction": 10000, "impact": 5000, "suggested_fix": 5000, "researcher_gpg": 16384, "credit_preference": 16, "credit_handle": 100} {
		value := strings.TrimSpace(r.FormValue(key))
		if len([]rune(value)) > maximum {
			return errors.New(i18n.Translate(lang, "contact.runtime.validation_length"))
		}
	}
	if typeName != "security" && (strings.TrimSpace(r.FormValue("subject")) == "" || strings.TrimSpace(r.FormValue("message")) == "") {
		return errors.New(i18n.Translate(lang, "contact.runtime.validation_required"))
	}
	if typeName == "abuse" || typeName == "dmca" {
		u, err := url.Parse(strings.TrimSpace(r.FormValue("reported_url")))
		if err != nil || u.Scheme == "" || u.Host == "" || !strings.EqualFold(u.Host, r.Host) {
			return errors.New(i18n.Translate(lang, "contact.runtime.validation_reported_url"))
		}
	}
	switch typeName {
	case "abuse":
		switch r.FormValue("reason") {
		case "illegal_content", "terms_violation", "harassment", "malware_phishing", "other":
		default:
			return errors.New(i18n.Translate(lang, "contact.runtime.validation_abuse_reason"))
		}
	case "dmca":
		if strings.TrimSpace(r.FormValue("copyrighted_work")) == "" || !formChecked(r.FormValue("good_faith")) || !formChecked(r.FormValue("accuracy")) || strings.TrimSpace(r.FormValue("signature")) == "" {
			return errors.New(i18n.Translate(lang, "contact.runtime.validation_dmca_fields"))
		}
	case "privacy":
		switch r.FormValue("request_kind") {
		case "access", "export", "deletion", "correction", "opt_out", "other":
		default:
			return errors.New(i18n.Translate(lang, "contact.runtime.validation_privacy_kind"))
		}
	case "security":
		credit := r.FormValue("credit_preference")
		if !validSeverity(strings.ToLower(strings.TrimSpace(r.FormValue("severity")))) || strings.TrimSpace(r.FormValue("component")) == "" || strings.TrimSpace(r.FormValue("summary")) == "" || strings.TrimSpace(r.FormValue("reproduction")) == "" || strings.TrimSpace(r.FormValue("impact")) == "" || !validCredit(credit) || credit == "handle" && strings.TrimSpace(r.FormValue("credit_handle")) == "" || !formChecked(r.FormValue("coordinated_disclosure")) {
			return errors.New(i18n.Translate(lang, "contact.runtime.validation_security_fields"))
		}
	}
	return nil
}

func contactMailBody(form url.Values, typeName, lang string) string {
	keys := []string{"type", "name", "email", "reported_url", "reason", "copyrighted_work", "good_faith", "accuracy", "signature", "request_kind", "subject", "message"}
	var body strings.Builder
	for _, key := range keys {
		if value := strings.TrimSpace(form.Get(key)); value != "" {
			switch key {
			case "type":
				value = i18n.Translate(lang, "web.contact.type_"+value)
			case "reason", "request_kind":
				value = i18n.Translate(lang, "web.contact."+value)
			}
			fmt.Fprintf(&body, "%s: %s\n", i18n.Translate(lang, "contact.mail.label."+key), value)
		}
	}
	if typeName == "" {
		return ""
	}
	return body.String()
}

func (s *Server) contactOriginValid(r *http.Request) bool {
	if isVersionedAPIContact(s, r) {
		return true
	}
	site := strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))
	if site != "" && site != "same-origin" {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		origin = strings.TrimSpace(r.Header.Get("Referer"))
	}
	if origin == "" {
		return site == "same-origin"
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	requestScheme := "http"
	if r.TLS != nil {
		requestScheme = "https"
	}
	if expected, err := url.Parse(s.detectServerURL(r)); err == nil && expected.Host != "" {
		return strings.EqualFold(parsed.Host, expected.Host) && strings.EqualFold(parsed.Scheme, expected.Scheme)
	}
	return strings.EqualFold(parsed.Host, r.Host) && strings.EqualFold(parsed.Scheme, requestScheme)
}

func contactTokenKey() ([]byte, error) {
	return db.InstallationSecret()
}

func issueContactFormToken(now time.Time) (string, error) {
	secret, err := contactTokenKey()
	if err != nil {
		return "", err
	}
	nonce, err := randomContactToken()
	if err != nil {
		return "", err
	}
	issued := strconv.FormatInt(now.Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("contact-form|" + issued + "|" + nonce))
	return issued + "." + nonce + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

func (s *Server) verifyFormToken(token string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(parts[1]) != 32 {
		return false
	}
	issued, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || now.Sub(time.Unix(issued, 0)) < 2*time.Second || now.Sub(time.Unix(issued, 0)) > 30*time.Minute {
		return false
	}
	secret, err := contactTokenKey()
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("contact-form|" + parts[0] + "|" + parts[1]))
	expected, err := hex.DecodeString(parts[2])
	if err != nil || !hmac.Equal(expected, mac.Sum(nil)) {
		return false
	}
	return s.claimContactNonce(parts[1], false, now)
}

func (s *Server) issueReviewToken(form url.Values, now time.Time) (string, error) {
	secret, err := contactTokenKey()
	if err != nil {
		return "", err
	}
	nonce, err := randomContactToken()
	if err != nil {
		return "", err
	}
	issued := strconv.FormatInt(now.Unix(), 10)
	fields := formHash(form)
	payload := "contact-review|" + issued + "|" + nonce + "|" + fields
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	return issued + "." + nonce + "." + fields + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

func (s *Server) consumeReviewToken(token string, form url.Values, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 4 || len(parts[1]) != 32 {
		return false
	}
	issued, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || now.Sub(time.Unix(issued, 0)) < 2*time.Second || now.Sub(time.Unix(issued, 0)) > 30*time.Minute || parts[2] != formHash(form) {
		return false
	}
	secret, err := contactTokenKey()
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("contact-review|" + parts[0] + "|" + parts[1] + "|" + parts[2]))
	expected, err := hex.DecodeString(parts[3])
	if err != nil || !hmac.Equal(expected, mac.Sum(nil)) {
		return false
	}
	return s.claimContactNonce(parts[1], true, now)
}

func formHash(form url.Values) string {
	copy := make(url.Values, len(form))
	for key, values := range form {
		switch key {
		case "step", "review_token", "confirmed", "captcha", "captcha_answer", "captcha_id", "challenge_id", "form_token", "website", "g-recaptcha-response", "h-captcha-response":
			continue
		}
		copy[key] = append([]string(nil), values...)
	}
	digest := sha256.Sum256([]byte(copy.Encode()))
	return hex.EncodeToString(digest[:])
}

func (s *Server) claimContactNonce(nonce string, review bool, now time.Time) bool {
	s.contactMu.Lock()
	defer s.contactMu.Unlock()
	used := s.contactTokens
	if review {
		used = s.contactReviews
	}
	if used == nil {
		used = make(map[string]time.Time)
		if review {
			s.contactReviews = used
		} else {
			s.contactTokens = used
		}
	}
	for key, expiry := range used {
		if now.After(expiry) {
			delete(used, key)
		}
	}
	if _, ok := used[nonce]; ok || len(used) >= 50000 {
		return false
	}
	used[nonce] = now.Add(30 * time.Minute)
	return true
}

func (s *Server) renderContact(w http.ResponseWriter, r *http.Request, status int, data map[string]interface{}) {
	if data == nil {
		data = map[string]interface{}{}
	}
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	securityToken, _ := data["securityID"].(string)
	if securityToken == "" {
		securityToken = r.URL.Query().Get("security_id")
	}
	securityMode := validSecurityID(securityToken, time.Now())
	typeName, _ := data["type"].(string)
	if typeName == "" {
		typeName = r.URL.Query().Get("type")
	}
	if !validContactType(typeName, securityToken) {
		typeName = ""
	}
	data["type"] = typeName
	data["securityID"] = securityToken
	data["securityMode"] = securityMode && typeName == "security"
	data["showChooser"] = typeName == ""
	if typeName != "" {
		formToken, err := issueContactFormToken(time.Now())
		if err != nil {
			data["error"] = requestTranslation(r, "contact.runtime.api_form_unavailable")
		} else {
			data["formToken"] = formToken
		}
	}
	challengeID, question := "", ""
	provider := s.contactCaptchaProviderForRequest(r)
	data["captchaProvider"] = provider
	if typeName != "" {
		var err error
		challengeID, question, err = s.issueContactChallenge(r)
		if err != nil {
			data["error"] = requestTranslation(r, "contact.runtime.api_challenge_unavailable")
		}
	}
	if typeName != "" && provider != "simple" {
		data["captchaSiteKey"] = s.contactCaptchaSiteKey()
	}
	data["challengeID"] = challengeID
	data["challengeQuestion"] = question
	data["success"] = ""
	data["abuseEmail"] = s.responseContactEmail(r)
	if r.URL.Query().Get("sent") == "1" {
		message := requestTranslation(r, "contact.runtime.contact_success")
		if s.currentConfig() != nil {
			configured := s.currentConfig().Server.Pages.Contact.SuccessMessage
			defaultMessage := config.DefaultConfig().Server.Pages.Contact.SuccessMessage
			if configured != "" && configured != defaultMessage {
				message = configured
			}
		}
		data["success"] = message
	}
	if reportID := r.URL.Query().Get("report"); reportID != "" && strings.HasPrefix(reportID, "sec_") {
		data["reportID"] = reportID
		data["success"] = i18n.TranslateFormat(i18n.LangFromContext(r.Context()), "contact.runtime.security_success", map[string]string{"tracking_id": reportID})
	}
	if isVersionedAPIContact(s, r) && status >= http.StatusBadRequest {
		code := "VALIDATION_FAILED"
		switch status {
		case http.StatusNotFound:
			code = "NOT_FOUND"
		case http.StatusTooManyRequests:
			code = "RATE_LIMITED"
		case http.StatusServiceUnavailable:
			code = "MAINTENANCE"
		case http.StatusInternalServerError:
			code = "SERVER_ERROR"
		}
		message, _ := data["error"].(string)
		if message == "" {
			message = requestTranslation(r, "contact.runtime.request_unavailable")
		}
		sendAPIResponseErrorDetails(w, code, message, map[string]string{"field": "form", "rule": "validation"})
		return
	}
	s.renderPageStatus(w, r, "contact", status, PageData{Title: requestTranslation(r, "web.handler.title.contact"), Data: data})
}

func (s *Server) publicAbuseEmail() string {
	if s.currentConfig() == nil {
		return ""
	}
	if email := strings.TrimSpace(s.currentConfig().Server.Contact.Abuse.Email); email != "" {
		return email
	}
	return strings.TrimSpace(s.currentConfig().Server.Contact.General.Email)
}

func (s *Server) responseContactEmail(r *http.Request) string {
	if s.isTorRequest(r) {
		return s.responseSecurityContactEmail(r)
	}
	return s.publicAbuseEmail()
}

func contactFormValues(r *http.Request, securityID, message string) map[string]interface{} {
	values := map[string]interface{}{
		"error": message, "name": r.FormValue("name"), "email": r.FormValue("email"),
		"subject": r.FormValue("subject"), "message": r.FormValue("message"), "type": r.FormValue("type"),
		"reported_url": r.FormValue("reported_url"), "reason": r.FormValue("reason"),
		"copyrighted_work": r.FormValue("copyrighted_work"), "good_faith": r.FormValue("good_faith"),
		"accuracy": r.FormValue("accuracy"), "signature": r.FormValue("signature"),
		"request_kind": r.FormValue("request_kind"), "summary": r.FormValue("summary"),
		"component": r.FormValue("component"), "endpoint": r.FormValue("endpoint"),
		"severity": r.FormValue("severity"), "reproduction": r.FormValue("reproduction"),
		"impact": r.FormValue("impact"), "suggested_fix": r.FormValue("suggested_fix"),
		"researcher_gpg": r.FormValue("researcher_gpg"), "credit_preference": r.FormValue("credit_preference"),
		"credit_handle": r.FormValue("credit_handle"), "coordinated_disclosure": r.FormValue("coordinated_disclosure"),
	}
	if validSecurityID(securityID, time.Now()) {
		values["securityID"] = securityID
	}
	return values
}

type securityReportPayload struct {
	Summary               string `json:"summary"`
	Component             string `json:"component"`
	Endpoint              string `json:"endpoint"`
	Severity              string `json:"severity"`
	Reproduction          string `json:"reproduction"`
	Impact                string `json:"impact"`
	SuggestedFix          string `json:"suggested_fix"`
	CVERequested          bool   `json:"cve_requested"`
	DisclosureDays        int    `json:"disclosure_days"`
	Credit                string `json:"credit"`
	CreditName            string `json:"credit_name"`
	CoordinatedDisclosure bool   `json:"coordinated_disclosure"`
	AppVersion            string `json:"app_version"`
	CommitHash            string `json:"commit_hash"`
	SecurityID            string `json:"security_id"`
	Timestamp             string `json:"timestamp"`
	RequestUserAgent      string `json:"request_user_agent"`
}

type securityReportContact struct {
	Email         string `json:"email"`
	ResearcherGPG string `json:"researcher_gpg,omitempty"`
}

func (s *Server) handleSecurityReportSubmit(w http.ResponseWriter, r *http.Request, name, email, securityToken string) {
	severity := strings.TrimSpace(r.FormValue("severity"))
	component := strings.TrimSpace(r.FormValue("component"))
	summary := strings.TrimSpace(r.FormValue("summary"))
	reproduction := strings.TrimSpace(r.FormValue("reproduction"))
	impact := strings.TrimSpace(r.FormValue("impact"))
	credit := strings.TrimSpace(r.FormValue("credit_preference"))
	creditName := strings.TrimSpace(r.FormValue("credit_handle"))
	pubkey := strings.TrimSpace(r.FormValue("researcher_gpg"))
	disclosureDays, err := strconv.Atoi(r.FormValue("disclosure_days"))
	if strings.TrimSpace(r.FormValue("disclosure_days")) == "" {
		disclosureDays = 90
	} else if err != nil || disclosureDays < 1 || disclosureDays > 365 {
		s.renderContact(w, r, http.StatusBadRequest, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.disclosure_timeline_invalid")))
		return
	}
	if !validSeverity(severity) || component == "" || len([]rune(component)) > 255 || summary == "" || len([]rune(summary)) > 200 || len([]rune(reproduction)) > 10000 || len([]rune(impact)) > 5000 || len([]rune(r.FormValue("suggested_fix"))) > 5000 || len([]rune(pubkey)) > 16384 || !validCredit(credit) || credit == "handle" && creditName == "" || !formChecked(r.FormValue("coordinated_disclosure")) {
		s.renderContact(w, r, http.StatusBadRequest, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.security_fields_required")))
		return
	}
	if strings.HasPrefix(pubkey, "https://") && len([]rune(pubkey)) > 2048 {
		s.renderContact(w, r, http.StatusBadRequest, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.key_url_too_long")))
		return
	}
	if pubkey != "" && !strings.HasPrefix(pubkey, "https://") && !strings.Contains(pubkey, "-----BEGIN PGP PUBLIC KEY BLOCK-----") {
		s.renderContact(w, r, http.StatusBadRequest, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.key_material_required")))
		return
	}
	if strings.HasPrefix(pubkey, "https://") {
		keyURL, err := url.Parse(pubkey)
		if err != nil || keyURL.Scheme != "https" || keyURL.Host == "" || keyURL.User != nil {
			s.renderContact(w, r, http.StatusBadRequest, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.key_url_invalid")))
			return
		}
		pubkey, err = pgp.FetchPublicKey(r.Context(), pubkey)
		if err != nil {
			s.renderContact(w, r, http.StatusBadRequest, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.key_fetch_failed")))
			return
		}
	}
	if pubkey != "" {
		if _, err := pgp.ParsePublicKey(pubkey); err != nil {
			s.renderContact(w, r, http.StatusBadRequest, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.key_invalid")))
			return
		}
	}
	payload := securityReportPayload{
		Summary: summary, Component: component,
		Endpoint: strings.TrimSpace(r.FormValue("endpoint")), Severity: severity,
		Reproduction: reproduction, Impact: impact, SuggestedFix: r.FormValue("suggested_fix"),
		CVERequested:   formChecked(r.FormValue("cve_requested")),
		DisclosureDays: disclosureDays, Credit: credit, CreditName: creditName,
		CoordinatedDisclosure: true, SecurityID: securityToken,
		Timestamp: time.Now().UTC().Format(time.RFC3339), RequestUserAgent: r.UserAgent(),
	}
	if s.config != nil {
		payload.AppVersion, payload.CommitHash = s.config.Version, s.config.Commit
	}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		s.renderContact(w, r, http.StatusInternalServerError, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.report_prepare_failed")))
		return
	}
	key := ""
	if s.config != nil && s.currentConfig() != nil {
		key = s.currentConfig().Server.Security.EncryptionKey
	}
	ciphertext, bodyAlgorithm, projectPublicKey, err := s.encryptSecurityReportBody(key, plaintext)
	if err != nil {
		s.renderContact(w, r, http.StatusServiceUnavailable, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.encrypted_storage_unavailable")))
		return
	}
	contactPlaintext, err := json.Marshal(securityReportContact{Email: email, ResearcherGPG: pubkey})
	if err != nil {
		s.renderContact(w, r, http.StatusInternalServerError, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.contact_details_prepare_failed")))
		return
	}
	contactCiphertext, err := encryptSecurityReport(key, contactPlaintext)
	if err != nil {
		s.renderContact(w, r, http.StatusServiceUnavailable, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.encrypted_storage_unavailable")))
		return
	}
	trackingRandom, err := randomContactToken()
	if err != nil {
		s.renderContact(w, r, http.StatusInternalServerError, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.tracking_id_failed")))
		return
	}
	trackingID := "sec_" + trackingRandom[:16]
	researcherToken, err := randomContactToken()
	if err != nil {
		s.renderContact(w, r, http.StatusInternalServerError, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.status_link_failed")))
		return
	}
	tokenHash := sha256.Sum256([]byte(researcherToken))
	creditPref, creditName := securityCreditStorage(credit, name, creditName)
	if err := db.SaveSecurityReport(trackingID, securityToken, hex.EncodeToString(tokenHash[:]), ciphertext, bodyAlgorithm, contactCiphertext, severity, component, creditPref, creditName, payload.CVERequested); err != nil {
		s.renderContact(w, r, http.StatusServiceUnavailable, contactFormValues(r, securityToken, requestTranslation(r, "contact.runtime.report_store_failed")))
		return
	}
	s.logSecurityReportReceived(trackingID, severity, component)
	acknowledgmentSent := false
	if s.config.SendContact != nil || s.config.SendContactReply != nil || s.config.WebhookDispatcher != nil {
		lang := i18n.LangFromContext(r.Context())
		severityText := i18n.Translate(lang, "web.contact."+strings.ToLower(severity))
		secureMail := i18n.TranslateFormat(lang, "contact.mail.security.summary", map[string]string{"tracking_id": trackingID, "severity": severityText}) + "\n" + i18n.Translate(lang, "contact.mail.security.component") + ": " + component + "\n"
		if bodyAlgorithm == "pgp" {
			if encrypted, encryptErr := pgp.Encrypt(plaintext, projectPublicKey); encryptErr == nil {
				secureMail = string(encrypted)
			} else {
				secureMail = ""
			}
		} else {
			secureMail += i18n.Translate(lang, "contact.mail.security.warning") + "\n" + i18n.Translate(lang, "contact.mail.security.encrypted_payload") + "\n" + base64.StdEncoding.EncodeToString(ciphertext)
		}
		if secureMail != "" {
			if s.config.SendContact != nil || s.config.SendContactReply != nil {
				_ = s.sendContact(s.securityContactEmail(), i18n.TranslateFormat(lang, "contact.mail.security.subject", map[string]string{"tracking_id": trackingID}), secureMail)
			}
			securityTargets := s.contactWebhookTargets("security")
			if len(securityTargets) > 0 {
				s.dispatchWebhook(r, notify.Event{
					Role: "security", Event: "security.report_received",
					Subject:  i18n.TranslateFormat(lang, "contact.mail.security.subject", map[string]string{"tracking_id": trackingID}),
					Body:     i18n.TranslateFormat(lang, "contact.mail.security.summary", map[string]string{"tracking_id": trackingID, "severity": severityText}),
					Severity: severity, Component: component, TrackingID: trackingID,
				}, securityTargets)
			}
		}
		adminTargets := s.contactWebhookTargets("admin")
		if len(adminTargets) > 0 {
			summary := i18n.TranslateFormat(lang, "contact.mail.security.summary", map[string]string{"tracking_id": trackingID, "severity": severityText})
			s.dispatchWebhook(r, notify.Event{
				Role: "admin", Event: "security.report_received",
				Subject: i18n.Translate(lang, "contact.mail.security.admin_subject"), Body: summary,
				Severity: severity, TrackingID: trackingID,
			}, adminTargets)
		}
		statusURL := s.publicServerURL(r) + "/server/security/report/" + trackingID + "?token=" + researcherToken
		acknowledgment := i18n.TranslateFormat(lang, "contact.mail.security.acknowledgment", map[string]string{"tracking_id": trackingID, "status_url": statusURL})
		if pubkey != "" {
			if encrypted, encryptErr := pgp.Encrypt([]byte(acknowledgment), pubkey); encryptErr == nil {
				acknowledgment = string(encrypted)
			} else {
				acknowledgment = ""
			}
		}
		if acknowledgment != "" && s.sendContact(email, i18n.TranslateFormat(lang, "contact.mail.security.acknowledgment_subject", map[string]string{"tracking_id": trackingID}), acknowledgment) == nil {
			acknowledgmentSent = true
		}
	}
	if isVersionedAPIContact(s, r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		lang := i18n.LangFromContext(r.Context())
		message := i18n.TranslateFormat(lang, "contact.runtime.security_success", map[string]string{"tracking_id": trackingID})
		if acknowledgmentSent {
			message += i18n.Translate(lang, "contact.mail.security.confirmation_email_suffix")
		}
		sendAPIResponseOK(w, map[string]interface{}{"tracking_id": trackingID, "message": message})
		return
	}
	lang := i18n.LangFromContext(r.Context())
	message := i18n.TranslateFormat(lang, "contact.runtime.security_success", map[string]string{"tracking_id": trackingID})
	if acknowledgmentSent {
		message += i18n.Translate(lang, "contact.mail.security.confirmation_email_suffix")
	}
	s.contactSuccess(w, r, "security", trackingID, message)
}

func securityCreditStorage(preference, name, handle string) (string, string) {
	switch preference {
	case "real_name":
		return "name", name
	case "handle":
		return "handle", handle
	case "anonymous":
		return "anon", ""
	default:
		return "no", ""
	}
}

func (s *Server) sendContact(to, subject, body string) error {
	if s.config.SendContactReply != nil {
		return s.config.SendContactReply(to, "", subject, body)
	}
	if s.config.SendContact != nil {
		return s.config.SendContact(to, subject, body)
	}
	return fmt.Errorf("email delivery is not configured")
}

func (s *Server) contactWebhookTargets(typeName string) []notify.Target {
	if s.currentConfig() == nil {
		return nil
	}
	contact := s.currentConfig().Server.Contact
	var recipients []*config.ContactRecipientConfig
	switch typeName {
	case "admin":
		recipients = []*config.ContactRecipientConfig{&contact.Admin}
	case "security":
		recipients = []*config.ContactRecipientConfig{&contact.Security, &contact.Admin}
	case "abuse", "dmca":
		recipients = []*config.ContactRecipientConfig{&contact.Abuse, &contact.General, &contact.Admin}
	case "privacy":
		if strings.TrimSpace(contact.DPO.Email) != "" {
			return nil
		}
		recipients = []*config.ContactRecipientConfig{&contact.General, &contact.Admin}
	default:
		return nil
	}
	targets := make([]notify.Target, 0)
	seen := make(map[string]bool)
	for _, recipient := range recipients {
		for name, endpoint := range recipient.Webhooks {
			if strings.HasSuffix(name, "_secret") || strings.TrimSpace(endpoint) == "" || seen[name] {
				continue
			}
			secret := strings.TrimSpace(recipient.Webhooks[name+"_secret"])
			if secret == "" {
				continue
			}
			targets = append(targets, notify.Target{Name: name, URL: strings.TrimSpace(endpoint), Secret: secret})
			seen[name] = true
		}
	}
	return targets
}

func (s *Server) dispatchContactWebhook(r *http.Request, typeName, subject, body, severity, trackingID string) {
	targets := s.contactWebhookTargets(typeName)
	role := typeName
	switch typeName {
	case "dmca":
		role = "abuse"
	case "privacy":
		role = "general"
	}
	s.dispatchWebhook(r, notify.Event{
		Role: role, Event: role + ".report_received",
		Subject: role + " report received", Body: role + " report received",
		Severity: severity, Component: role, TrackingID: trackingID,
	}, targets)
}

func (s *Server) dispatchWebhook(r *http.Request, event notify.Event, targets []notify.Target) {
	if s == nil || s.config == nil || s.config.WebhookDispatcher == nil || len(targets) == 0 {
		return
	}
	if event.ProjectName == "" {
		event.ProjectName = "gitignore"
	}
	if s.config.Version != "" {
		event.ProjectVersion = s.config.Version
	}
	if r != nil {
		event.AppURL = s.publicServerURL(r)
	}
	go s.config.WebhookDispatcher.Dispatch(context.Background(), event, targets)
}

func validSeverity(value string) bool {
	switch value {
	case "critical", "high", "medium", "low", "informational":
		return true
	default:
		return false
	}
}

func validCredit(value string) bool {
	switch value {
	case "real_name", "handle", "no", "anonymous":
		return true
	default:
		return false
	}
}

func formChecked(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true", "1", "on":
		return true
	default:
		return false
	}
}

func (s *Server) contactPageEnabled() bool {
	return s.currentConfig() == nil || s.currentConfig().Server.Pages.Contact.Enabled
}

func (s *Server) contactCaptchaProvider() string {
	if s.currentConfig() == nil {
		return "simple"
	}
	provider := strings.ToLower(strings.TrimSpace(s.currentConfig().Server.Pages.Contact.Captcha))
	if provider == "" {
		return "simple"
	}
	return provider
}

func (s *Server) contactCaptchaProviderForRequest(r *http.Request) string {
	provider := s.contactCaptchaProvider()
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	host = strings.ToLower(host)
	if strings.HasSuffix(host, ".onion") || strings.HasSuffix(host, ".i2p") {
		return "simple"
	}
	return provider
}

func (s *Server) contactCaptchaSiteKey() string {
	if s.currentConfig() == nil {
		return ""
	}
	return s.currentConfig().Server.Pages.Contact.CaptchaSiteKey
}

func (s *Server) verifyContactCaptcha(r *http.Request) bool {
	provider := s.contactCaptchaProviderForRequest(r)
	if provider == "simple" {
		return s.consumeContactChallenge(r, contactCaptchaID(r), contactCaptchaAnswer(r))
	}
	if s.currentConfig() == nil {
		return false
	}
	page := s.currentConfig().Server.Pages.Contact
	endpoint, responseField := "", ""
	switch provider {
	case "recaptcha":
		endpoint, responseField = "https://www.google.com/recaptcha/api/siteverify", "g-recaptcha-response"
	case "hcaptcha":
		endpoint, responseField = "https://api.hcaptcha.com/siteverify", "h-captcha-response"
	default:
		return false
	}
	response := strings.TrimSpace(r.FormValue(responseField))
	if response == "" {
		return s.consumeContactChallenge(r, contactCaptchaID(r), contactCaptchaAnswer(r))
	}
	if page.CaptchaSecretKey == "" {
		return false
	}
	values := url.Values{"secret": {page.CaptchaSecretKey}, "response": {response}, "remoteip": {s.clientIP(r)}}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var result struct {
		Success bool `json:"success"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 16*1024)).Decode(&result) != nil {
		return false
	}
	return result.Success
}

func contactCaptchaID(r *http.Request) string {
	if value := r.FormValue("captcha_id"); value != "" {
		return value
	}
	return r.FormValue("challenge_id")
}

func contactCaptchaAnswer(r *http.Request) string {
	if value := r.FormValue("captcha_answer"); value != "" {
		return value
	}
	return r.FormValue("captcha")
}

func (s *Server) contactRecipientForType(typeName string) string {
	if s.currentConfig() == nil {
		return ""
	}
	contact := s.currentConfig().Server.Contact
	var recipient string
	switch typeName {
	case "abuse", "dmca":
		recipient = strings.TrimSpace(contact.Abuse.Email)
	case "privacy":
		recipient = strings.TrimSpace(contact.DPO.Email)
	case "security":
		recipient = strings.TrimSpace(contact.Security.Email)
	}
	if recipient != "" {
		return recipient
	}
	if typeName != "security" && strings.TrimSpace(contact.General.Email) != "" {
		return strings.TrimSpace(contact.General.Email)
	}
	if email := strings.TrimSpace(contact.Admin.Email); email != "" {
		return email
	}
	return strings.TrimSpace(s.currentConfig().Server.Admin.Email)
}

func (s *Server) securityContactEmail() string {
	if s.currentConfig() == nil {
		return ""
	}
	if email := strings.TrimSpace(s.currentConfig().Server.Contact.Security.Email); email != "" {
		return email
	}
	if email := strings.TrimSpace(s.currentConfig().Web.Security.Admin); email != "" {
		return email
	}
	if email := strings.TrimSpace(s.currentConfig().Server.Contact.Admin.Email); email != "" {
		return email
	}
	if email := strings.TrimSpace(s.currentConfig().Server.Admin.Email); email != "" {
		return email
	}
	if fqdn := strings.TrimSpace(s.currentConfig().Server.FQDN); fqdn != "" {
		return "security@" + fqdn
	}
	return ""
}

func (s *Server) responseSecurityContactEmail(r *http.Request) string {
	if s.isTorRequest(r) {
		if cfg := s.currentConfig(); cfg != nil {
			return strings.TrimSpace(cfg.Server.Tor.ContactEmail)
		}
		return ""
	}
	return s.securityContactEmail()
}

func (s *Server) issueContactChallenge(r *http.Request) (string, string, error) {
	first, err := rand.Int(rand.Reader, big.NewInt(8))
	if err != nil {
		return "", "", err
	}
	second, err := rand.Int(rand.Reader, big.NewInt(8))
	if err != nil {
		return "", "", err
	}
	left, right := first.Int64()+2, second.Int64()+2
	challengeID, err := randomContactToken()
	if err != nil {
		return "", "", err
	}
	ip := s.clientIP(r)
	now := time.Now()
	s.contactMu.Lock()
	defer s.contactMu.Unlock()
	if s.contactChallenges == nil {
		s.contactChallenges = map[string]contactChallenge{}
	}
	if s.contactRequests == nil {
		s.contactRequests = map[string][]time.Time{}
	}
	for id, challenge := range s.contactChallenges {
		if now.After(challenge.expires) {
			delete(s.contactChallenges, id)
		}
	}
	requests := s.contactRequests[ip][:0]
	for _, created := range s.contactRequests[ip] {
		if now.Sub(created) < time.Minute {
			requests = append(requests, created)
		}
	}
	if len(requests) >= 5 {
		s.contactRequests[ip] = requests
		return "", "", fmt.Errorf("challenge rate limit exceeded")
	}
	s.contactRequests[ip] = append(requests, now)
	answer := fmt.Sprintf("%d", left+right)
	s.contactChallenges[challengeID] = contactChallenge{
		answerHash: hashContactAnswer(challengeID, answer),
		ip:         ip,
		expires:    now.Add(10 * time.Minute),
	}
	return challengeID, i18n.TranslateFormat(i18n.LangFromContext(r.Context()), "contact.captcha.question", map[string]interface{}{"left": int(left), "right": int(right)}), nil
}

// CleanupExpiredContactChallenges removes expired in-memory simple challenges
// and stale request counters during the scheduled token-cleanup task.
func (s *Server) CleanupExpiredContactChallenges(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := time.Now()
	s.contactMu.Lock()
	defer s.contactMu.Unlock()
	for id, challenge := range s.contactChallenges {
		if !now.Before(challenge.expires) {
			delete(s.contactChallenges, id)
		}
	}
	for ip, requests := range s.contactRequests {
		kept := requests[:0]
		for _, created := range requests {
			if now.Sub(created) < time.Minute {
				kept = append(kept, created)
			}
		}
		if len(kept) == 0 {
			delete(s.contactRequests, ip)
		} else {
			s.contactRequests[ip] = kept
		}
	}
	return nil
}

func (s *Server) consumeContactChallenge(r *http.Request, id, answer string) bool {
	s.contactMu.Lock()
	challenge, ok := s.contactChallenges[id]
	delete(s.contactChallenges, id)
	s.contactMu.Unlock()
	if !ok || time.Now().After(challenge.expires) || challenge.ip != s.clientIP(r) {
		return false
	}
	got := hashContactAnswer(id, strings.TrimSpace(answer))
	return subtle.ConstantTimeCompare(got[:], challenge.answerHash[:]) == 1
}

func hashContactAnswer(id, answer string) [sha256.Size]byte {
	return sha256.Sum256([]byte(id + ":" + answer))
}

func randomContactToken() (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}
