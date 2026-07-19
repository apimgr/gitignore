package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
)

// gitignoreIOEntry matches gitignore.io's raw JSON list entry shape exactly:
// {"key": "go", "name": "Go", "fileName": "Go.gitignore", "contents": "..."}
type gitignoreIOEntry struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	FileName string `json:"fileName"`
	Contents string `json:"contents"`
}

// handleCompatList implements gitignore.io's GET /api/list route.
// format=lines (default): text/plain, comma-separated sorted keys.
// format=json: application/json, flat object keyed by lowercase template key.
func (s *Server) handleCompatList(w http.ResponseWriter, r *http.Request) {
	all := s.config.Templates.ListAll()

	keys := make([]string, 0, len(all))
	byKey := make(map[string]*gitignoreIOEntry, len(all))
	for _, tmpl := range all {
		key := strings.ToLower(tmpl.Name)
		keys = append(keys, key)
		byKey[key] = &gitignoreIOEntry{
			Key:      key,
			Name:     tmpl.Name,
			FileName: tmpl.FileName,
			Contents: tmpl.Content,
		}
	}
	sort.Strings(keys)

	if r.URL.Query().Get("format") == "json" {
		result := make(map[string]*gitignoreIOEntry, len(keys))
		for _, key := range keys {
			result[key] = byKey[key]
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(result)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, strings.Join(keys, ","))
}

// handleCompatTemplates implements gitignore.io's GET /api/{name1,name2,...} route.
// Resolved names render as "### {Name} ###\n{contents}" blocks; unresolved names
// render as "#!! ERROR: {name} is undefined. Use list command to see defined
// gitignore types !!#" blocks. Status is 404 if the first requested name fails
// to resolve, 200 otherwise, matching the live gitignore.io service.
func (s *Server) handleCompatTemplates(w http.ResponseWriter, r *http.Request) {
	list := chi.URLParam(r, "list")

	names := strings.Split(list, ",")
	for i, name := range names {
		names[i] = strings.TrimSpace(name)
	}

	serverURL := s.detectServerURL(r)
	header := fmt.Sprintf("# Created by %s/api/%s", serverURL, list)
	edit := fmt.Sprintf("# Edit at %s/api?templates=%s", serverURL, list)
	footer := fmt.Sprintf("# End of %s/api/%s", serverURL, list)

	var body strings.Builder
	body.WriteString(header)
	body.WriteString("\n")
	body.WriteString(edit)
	body.WriteString("\n\n")

	firstResolved := true
	firstOK := false
	for _, name := range names {
		tmpl, err := s.config.Templates.Get(name)
		if err != nil {
			if firstResolved {
				firstOK = false
				firstResolved = false
			}
			fmt.Fprintf(&body, "#!! ERROR: %s is undefined. Use list command to see defined gitignore types !!#\n\n", name)
			continue
		}
		if firstResolved {
			firstOK = true
			firstResolved = false
		}
		fmt.Fprintf(&body, "### %s ###\n%s\n\n", tmpl.Name, tmpl.Content)
	}

	body.WriteString(footer)

	status := http.StatusOK
	if !firstOK {
		status = http.StatusNotFound
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprint(w, body.String())
}
