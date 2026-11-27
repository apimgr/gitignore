package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/apimgr/gitignore/src/template"
)

func TestGraphQLHandlerAndSchemaEndpoint(t *testing.T) {
	templates, err := template.New()
	if err != nil {
		t.Fatal(err)
	}
	s := New(&ServerOptions{Templates: templates})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/server/graphql", strings.NewReader(`{"query":"{ template(name: \"Go\") { name } }"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	s.handleGraphQL(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"name": "Go"`) {
		t.Fatalf("GraphQL response = %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	s.handleGraphQLSchema(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/server/graphql", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "type Query") {
		t.Fatalf("GraphQL SDL response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestGraphQLGETSupportsQueryVariablesAndUsage(t *testing.T) {
	templates, err := template.New()
	if err != nil {
		t.Fatal(err)
	}
	s := New(&ServerOptions{Templates: templates})
	values := url.Values{
		"query":         {"query GetTemplate($name: String!) { template(name: $name) { name } }"},
		"operationName": {"GetTemplate"},
		"variables":     {`{"name":"Go"}`},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/server/graphql?"+values.Encode(), nil)
	recorder := httptest.NewRecorder()
	s.handleGraphQL(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"name": "Go"`) {
		t.Fatalf("GraphQL GET response = %d %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/server/graphql", nil)
	recorder = httptest.NewRecorder()
	s.handleGraphQL(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "text/plain; charset=utf-8" || !strings.Contains(recorder.Body.String(), "query parameter") {
		t.Fatalf("GraphQL GET usage response = %d %q %s", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/server/graphql?query=%7B%20list%20%7B%20name%20%7D%20%7D&variables=%5B%5D", nil)
	recorder = httptest.NewRecorder()
	s.handleGraphQL(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "errors") {
		t.Fatalf("invalid GraphQL GET variables returned %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestGraphQLHandlerRejectsMalformedAndMultipleJSONValues(t *testing.T) {
	s := New(&ServerOptions{})
	for _, body := range []string{"{", `{"query":"{ list { name } }"} {}`} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/server/graphql", strings.NewReader(body))
		s.handleGraphQL(recorder, request)
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "errors") {
			t.Errorf("malformed GraphQL body %q returned %d %s", body, recorder.Code, recorder.Body.String())
		}
	}
}

func TestGraphQLHandlerLocalizesErrors(t *testing.T) {
	s := New(&ServerOptions{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/server/graphql?lang=es", strings.NewReader("{"))
	recorder := httptest.NewRecorder()
	i18n.Middleware(http.HandlerFunc(s.handleGraphQL)).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("GraphQL error status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	var response struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode GraphQL error response: %v", err)
	}
	want := i18n.Translate("es", "errors.bad_request")
	if len(response.Errors) != 1 || response.Errors[0].Message != want {
		t.Fatalf("GraphQL error message = %#v, want %q", response.Errors, want)
	}
}

func TestGraphQLExecutionErrorsAreLocalizedWithoutLosingLocations(t *testing.T) {
	templates, err := template.New()
	if err != nil {
		t.Fatal(err)
	}
	s := New(&ServerOptions{Templates: templates})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/server/graphql?lang=es", strings.NewReader(`{"query":"{ unknownField }"}`))
	recorder := httptest.NewRecorder()
	i18n.Middleware(http.HandlerFunc(s.handleGraphQL)).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GraphQL validation status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var response struct {
		Errors []struct {
			Message   string `json:"message"`
			Locations []struct {
				Line   int `json:"line"`
				Column int `json:"column"`
			} `json:"locations"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode GraphQL validation response: %v", err)
	}
	want := i18n.Translate("es", "graphql.errors.query_failed")
	if len(response.Errors) != 1 || response.Errors[0].Message != want || len(response.Errors[0].Locations) == 0 {
		t.Fatalf("GraphQL validation error = %#v, want localized message %q with source locations", response.Errors, want)
	}
}
