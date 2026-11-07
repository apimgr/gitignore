package graphql

import (
	"context"
	"embed"

	graphqlgo "github.com/graph-gophers/graphql-go"

	"github.com/apimgr/gitignore/src/template"
)

//go:embed schema.graphql
var schemaSource embed.FS

// Schema wraps the executable query schema and its documentation source.
type Schema struct {
	executable *graphqlgo.Schema
}

// New loads the validated schema with read-only resolvers for the template manager.
func New(templates *template.Manager) (*Schema, error) {
	source, err := schemaSource.ReadFile("schema.graphql")
	if err != nil {
		return nil, err
	}
	executable, err := graphqlgo.ParseSchema(string(source), &queryResolver{templates: templates}, graphqlgo.MaxDepth(12))
	if err != nil {
		return nil, err
	}
	return &Schema{executable: executable}, nil
}

// SDL returns the schema used to construct the executable GraphQL API.
func SDL() (string, error) {
	source, err := schemaSource.ReadFile("schema.graphql")
	if err != nil {
		return "", err
	}
	return string(source), nil
}

// Execute parses, validates, and executes a GraphQL query against the schema.
func (s *Schema) Execute(ctx context.Context, query, operation string, variables map[string]any) *graphqlgo.Response {
	return s.executable.Exec(ctx, query, operation, variables)
}

type queryResolver struct {
	templates *template.Manager
}

type templateResolver struct {
	template *template.Template
}

type statsResolver struct {
	totalTemplates int32
	categories     int32
	totalSizeBytes int32
}

func (r *queryResolver) Template(_ context.Context, args *struct{ Name string }) *templateResolver {
	tmpl, err := r.templates.Get(args.Name)
	if err != nil {
		return nil
	}
	return &templateResolver{template: tmpl}
}

func (r *queryResolver) List() []*templateResolver {
	return resolveTemplates(r.templates.ListAll())
}

func (r *queryResolver) Search(_ context.Context, args *struct{ Q string }) []*templateResolver {
	return resolveTemplates(r.templates.Search(args.Q))
}

func (r *queryResolver) Categories() []string {
	return r.templates.GetCategories()
}

func (r *queryResolver) Category(_ context.Context, args *struct{ Name string }) []*templateResolver {
	return resolveTemplates(r.templates.GetByCategory(args.Name))
}

func (r *queryResolver) Combine(_ context.Context, args *struct{ Templates []string }) (string, error) {
	return r.templates.Combine(args.Templates)
}

func (r *queryResolver) Stats() *statsResolver {
	stats := r.templates.Stats()
	return &statsResolver{
		totalTemplates: int32(stats["total_templates"].(int)),
		categories:     int32(stats["categories"].(int)),
		totalSizeBytes: int32(stats["total_size_bytes"].(int)),
	}
}

func (r *templateResolver) Name() string        { return r.template.Name }
func (r *templateResolver) FileName() string    { return r.template.FileName }
func (r *templateResolver) Category() string    { return r.template.Category }
func (r *templateResolver) Content() *string    { return &r.template.Content }
func (r *templateResolver) Description() *string { return &r.template.Description }
func (r *templateResolver) Size() int32         { return int32(r.template.Size) }
func (r *templateResolver) Tags() []string {
	if len(r.template.Tags) == 0 {
		return []string{}
	}
	return r.template.Tags
}

func (r *statsResolver) TotalTemplates() int32 { return r.totalTemplates }
func (r *statsResolver) Categories() int32     { return r.categories }
func (r *statsResolver) TotalSizeBytes() int32 { return r.totalSizeBytes }

func resolveTemplates(templates []*template.Template) []*templateResolver {
	resolved := make([]*templateResolver, 0, len(templates))
	for _, tmpl := range templates {
		resolved = append(resolved, &templateResolver{template: tmpl})
	}
	return resolved
}
