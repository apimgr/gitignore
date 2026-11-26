package server

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/config"
)

const dismissedAnnouncementsCookie = "dismissed_announcements"

var announcementIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func (s *Server) activeAnnouncements(r *http.Request) []AnnouncementView {
	cfg := s.currentConfig()
	if cfg == nil {
		return nil
	}
	settings := cfg.Web.Announcements
	if !settings.Enabled {
		return nil
	}
	dismissed := map[string]bool{}
	if cookie, err := r.Cookie(dismissedAnnouncementsCookie); err == nil {
		for _, id := range strings.Split(cookie.Value, ",") {
			dismissed[id] = true
		}
	}
	now := time.Now().UTC()
	views := make([]AnnouncementView, 0, len(settings.Messages))
	seen := make(map[string]bool, len(settings.Messages))
	for _, message := range settings.Messages {
		id := message.ID
		kind := strings.ToLower(strings.TrimSpace(message.Type))
		if !announcementIDPattern.MatchString(id) || dismissed[id] || seen[id] {
			continue
		}
		if kind == "" {
			kind = "info"
		}
		icon, ok := map[string]string{"info": "ℹ", "warning": "⚠", "error": "!", "success": "✓"}[kind]
		if !ok || !announcementInWindow(message, now) || strings.TrimSpace(message.Message) == "" {
			continue
		}
		seen[id] = true
		views = append(views, AnnouncementView{
			ID: id, Type: kind, Icon: icon, Title: strings.TrimSpace(message.Title),
			Message: strings.TrimSpace(message.Message), Dismissible: message.Dismissible,
		})
	}
	return views
}

func announcementInWindow(message config.WebAnnouncementConfig, now time.Time) bool {
	if message.Start != "" {
		start, err := time.Parse(time.RFC3339, message.Start)
		if err != nil || now.Before(start) {
			return false
		}
	}
	if message.End != "" {
		end, err := time.Parse(time.RFC3339, message.End)
		if err != nil || !now.Before(end) {
			return false
		}
	}
	return true
}

func (s *Server) handleAnnouncementDismiss(w http.ResponseWriter, r *http.Request) {
	if !validCSRFRequest(r) {
		http.Error(w, requestTranslation(r, "web.handler.error.csrf_invalid"), http.StatusForbidden)
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	if !announcementIDPattern.MatchString(id) || !s.dismissibleAnnouncement(id) {
		http.Error(w, requestTranslation(r, "errors.validation_failed"), http.StatusBadRequest)
		return
	}
	ids := make([]string, 0, 32)
	seen := map[string]bool{id: true}
	if cookie, err := r.Cookie(dismissedAnnouncementsCookie); err == nil && len(cookie.Value) <= 2048 {
		for _, previous := range strings.Split(cookie.Value, ",") {
			if announcementIDPattern.MatchString(previous) && !seen[previous] && len(ids) < 31 {
				ids = append(ids, previous)
				seen[previous] = true
			}
		}
	}
	ids = append(ids, id)
	http.SetCookie(w, &http.Cookie{
		Name: dismissedAnnouncementsCookie, Value: strings.Join(ids, ","), Path: "/",
		MaxAge: 365 * 24 * 60 * 60, SameSite: http.SameSiteLaxMode,
		Secure: preferenceCookieSecure(s, r),
	})
	http.Redirect(w, r, safeAnnouncementReturn(r.FormValue("return_to")), http.StatusSeeOther)
}

func (s *Server) dismissibleAnnouncement(id string) bool {
	cfg := s.currentConfig()
	if cfg == nil {
		return false
	}
	for _, message := range cfg.Web.Announcements.Messages {
		if message.ID == id && message.Dismissible {
			return true
		}
	}
	return false
}

func safeAnnouncementReturn(target string) string {
	if target == "" || !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") || strings.HasPrefix(target, "/\\") || strings.ContainsAny(target, "\\\r\n") {
		return "/"
	}
	parsed, err := url.ParseRequestURI(target)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return "/"
	}
	return parsed.RequestURI()
}
