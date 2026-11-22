package graphql

import (
	"encoding/json"
	"strings"

	"github.com/apimgr/gitignore/src/common/i18n"
)

type schemaField struct {
	name      string
	arguments string
	result    string
}

type schemaType struct {
	name        string
	description string
	fields      []schemaField
}

func generatedSchema(lang string) string {
	types := []schemaType{
		{
			name:        "Template",
			description: "graphql.type_descriptions.template",
			fields: []schemaField{
				{name: "name", result: "String!"},
				{name: "fileName", result: "String!"},
				{name: "category", result: "String!"},
				{name: "content", result: "String"},
				{name: "description", result: "String"},
				{name: "tags", result: "[String!]!"},
				{name: "size", result: "Int!"},
			},
		},
		{
			name:        "Stats",
			description: "graphql.type_descriptions.stats",
			fields: []schemaField{
				{name: "totalTemplates", result: "Int!"},
				{name: "categories", result: "Int!"},
				{name: "totalSizeBytes", result: "Int!"},
			},
		},
		{
			name:        "Query",
			description: "graphql.type_descriptions.query",
			fields: []schemaField{
				{name: "template", arguments: "name: String!", result: "Template"},
				{name: "list", result: "[Template!]!"},
				{name: "search", arguments: "q: String!", result: "[Template!]!"},
				{name: "categories", result: "[String!]!"},
				{name: "category", arguments: "name: String!", result: "[Template!]!"},
				{name: "combine", arguments: "templates: [String!]!", result: "String!"},
				{name: "stats", result: "Stats!"},
			},
		},
	}

	var source strings.Builder
	for _, typ := range types {
		writeDescription(&source, i18n.Translate(lang, typ.description), "")
		source.WriteString("type " + typ.name + " {\n")
		for _, field := range typ.fields {
			key := "graphql.field_descriptions." + strings.ToLower(typ.name) + "." + field.name
			writeDescription(&source, i18n.Translate(lang, key), "  ")
			source.WriteString("  " + field.name)
			if field.arguments != "" {
				source.WriteString("(" + field.arguments + ")")
			}
			source.WriteString(": " + field.result + "\n")
		}
		source.WriteString("}\n\n")
	}
	return source.String()
}

func writeDescription(source *strings.Builder, description, indent string) {
	encoded, _ := json.Marshal(description)
	source.WriteString(indent + string(encoded) + "\n")
}
