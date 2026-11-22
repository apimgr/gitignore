package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/template"
)

func TestGraphQLHandlerAndSchemaEndpoint(t *testing.T) {
	templates, err := template.New()
	if err != nil {
		t.Fatal(err)
	}
	s := New(&Config{Templates: templates})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/server/graphql", strings.NewReader(`{"query":"{ template(name: \"Go\") { name } }"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	s.handleGraphQL(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"name":"Go"`) {
		t.Fatalf("GraphQL response = %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	s.handleGraphQLSchema(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/server/graphql", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "type Query") {
		t.Fatalf("GraphQL SDL response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestGraphQLHandlerRejectsMalformedAndMultipleJSONValues(t *testing.T) {
	s := New(&Config{})
	for _, body := range []string{"{", `{"query":"{ list { name } }"} {}`} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/server/graphql", strings.NewReader(body))
		s.handleGraphQL(recorder, request)
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "errors") {
			t.Errorf("malformed GraphQL body %q returned %d %s", body, recorder.Code, recorder.Body.String())
		}
	}
}
