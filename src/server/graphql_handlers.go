package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/apimgr/gitignore/src/common/i18n"
	graphapi "github.com/apimgr/gitignore/src/graphql"
)

const maxGraphQLRequestBytes = 1 << 20

type graphQLRequest struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables"`
}

func (s *Server) graphQLSchema(lang string) (*graphapi.Schema, error) {
	s.graphQLMu.Lock()
	defer s.graphQLMu.Unlock()
	if s.graphQL == nil {
		s.graphQL = make(map[string]*graphapi.Schema)
	}
	if schema := s.graphQL[lang]; schema != nil {
		return schema, nil
	}
	schema, err := graphapi.NewForLanguage(s.config.Templates, lang)
	if err != nil {
		return nil, err
	}
	s.graphQL[lang] = schema
	return schema, nil
}

// handleGraphQL executes a read-only GraphQL query against the template catalog.
func (s *Server) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxGraphQLRequestBytes))
	var request graphQLRequest
	if err := decoder.Decode(&request); err != nil {
		writeGraphQLError(w, http.StatusBadRequest, "request body must be a valid GraphQL JSON object")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeGraphQLError(w, http.StatusBadRequest, "request body must contain exactly one JSON object")
		return
	}

	lang := i18n.LangFromContext(r.Context())
	schema, err := s.graphQLSchema(lang)
	if err != nil {
		writeGraphQLError(w, http.StatusInternalServerError, "GraphQL schema is unavailable")
		return
	}

	setCacheHeaders(w, "api")
	w.Header().Add("Vary", "Accept-Language")
	w.Header().Add("Vary", "Cookie")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	response := schema.Execute(r.Context(), request.Query, request.OperationName, request.Variables)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		return
	}
}

// handleGraphQLSchema serves the SDL used to build the executable schema.
func (s *Server) handleGraphQLSchema(w http.ResponseWriter, r *http.Request) {
	sdl, err := graphapi.SDLForLanguage(i18n.LangFromContext(r.Context()))
	if err != nil {
		writeGraphQLError(w, http.StatusInternalServerError, "GraphQL schema is unavailable")
		return
	}

	setCacheHeaders(w, "api")
	w.Header().Add("Vary", "Accept-Language")
	w.Header().Add("Vary", "Cookie")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, sdl)
}

func writeGraphQLError(w http.ResponseWriter, status int, message string) {
	setCacheHeaders(w, "api")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]string{{"message": message}},
	})
}
