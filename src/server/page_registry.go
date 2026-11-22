package server

import (
	"sort"

	"github.com/apimgr/gitignore/src/common/i18n"
)

type registeredPage struct {
	path       string
	titleKey   string
	descKey    string
	order      int
	preference bool
	enabled    func(*Server) bool
}

type hubPage struct {
	Path        string `json:"path"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

var serverPageRegistry = []registeredPage{
	{path: "/server/preferences/tokens", titleKey: "owner_tokens_page.title", descKey: "owner_tokens_page.description", order: 10, preference: true, enabled: func(*Server) bool { return true }},
	{path: "/server/preferences/sync", titleKey: "preferences_page.sync_title", descKey: "preferences_page.sync_description", order: 20, preference: true, enabled: func(*Server) bool { return true }},
	{path: "/server/help/mcp", titleKey: "help.mcp_title", descKey: "help.mcp_description", order: 10, enabled: func(*Server) bool { return true }},
}

func (s *Server) registeredHubPages(preferences bool, lang string) []hubPage {
	pages := make([]registeredPage, 0, len(serverPageRegistry))
	for _, page := range serverPageRegistry {
		if page.preference == preferences && page.enabled(s) {
			pages = append(pages, page)
		}
	}
	sort.SliceStable(pages, func(i, j int) bool { return pages[i].order < pages[j].order })
	result := make([]hubPage, 0, len(pages))
	for _, page := range pages {
		result = append(result, hubPage{Path: page.path, Title: i18n.Translate(lang, page.titleKey), Description: i18n.Translate(lang, page.descKey)})
	}
	return result
}
