package graphql

import (
	"context"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/template"
)

func TestNewSchemaExecutesTemplateQueries(t *testing.T) {
	templates, err := template.New()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := New(templates)
	if err != nil {
		t.Fatal(err)
	}

	response := schema.Execute(context.Background(), `{ template(name: "Go") { name fileName content } }`, "", nil)
	if len(response.Errors) != 0 {
		t.Fatalf("GraphQL query returned errors: %v", response.Errors)
	}
	if !strings.Contains(string(response.Data), `"name":"Go"`) || !strings.Contains(string(response.Data), `"content"`) {
		t.Fatalf("GraphQL query did not return the Go template: %s", response.Data)
	}
}

func TestSchemaRejectsInvalidFields(t *testing.T) {
	templates, err := template.New()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := New(templates)
	if err != nil {
		t.Fatal(err)
	}
	response := schema.Execute(context.Background(), `{ doesNotExist }`, "", nil)
	if len(response.Errors) == 0 {
		t.Fatalf("invalid query was accepted: %s", response.Data)
	}
}
