package server

import (
	"encoding/json"
	"io"
	"net/http"

	graphapi "github.com/apimgr/gitignore/src/graphql"
)

const maxGraphQLRequestBytes = 1 << 20

type graphQLRequest struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables"`
}

func (s *Server) graphQLSchema() (*graphapi.Schema, error) {
	s.graphQLOnce.Do(func() {
		s.graphQL, s.graphQLError = graphapi.New(s.config.Templates)
	})
	return s.graphQL, s.graphQLError
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

	schema, err := s.graphQLSchema()
	if err != nil {
		writeGraphQLError(w, http.StatusInternalServerError, "GraphQL schema is unavailable")
		return
	}

	setCacheHeaders(w, "api")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	response := schema.Execute(r.Context(), request.Query, request.OperationName, request.Variables)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		return
	}
}

// handleGraphQLSchema serves the SDL used to build the executable schema.
func (s *Server) handleGraphQLSchema(w http.ResponseWriter, _ *http.Request) {
	sdl, err := graphapi.SDL()
	if err != nil {
		writeGraphQLError(w, http.StatusInternalServerError, "GraphQL schema is unavailable")
		return
	}

	setCacheHeaders(w, "api")
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
