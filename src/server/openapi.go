package server

import (
	"net/http"
	"strings"

	"github.com/apimgr/gitignore/src/common/i18n"
)

func mergeOperations(left, right map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{}, len(left)+len(right))
	for key, value := range left {
		merged[key] = value
	}
	for key, value := range right {
		merged[key] = value
	}
	return merged
}

// openAPISpec builds the OpenAPI 3.0 document describing the public /api/v1
// surface (AI.md PART 14). The server URL is derived from the request so the
// document is correct regardless of host or scheme.
func (s *Server) openAPISpec(r *http.Request) map[string]interface{} {
	text := func(key string) string { return i18n.T(r, key) }
	base := s.publicServerURL(r)
	api := s.apiBasePath()

	templateName := map[string]interface{}{
		"name":        "name",
		"in":          "path",
		"required":    true,
		"description": text("swagger.openapi.parameter.template_name"),
		"schema":      map[string]interface{}{"type": "string"},
	}

	okEnvelope := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"ok":   map[string]interface{}{"type": "boolean"},
			"data": map[string]interface{}{},
			"meta": map[string]interface{}{"type": "object"},
		},
	}
	errEnvelope := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"ok":      map[string]interface{}{"type": "boolean"},
			"error":   map[string]interface{}{"type": "string"},
			"message": map[string]interface{}{"type": "string"},
		},
	}

	jsonOK := map[string]interface{}{
		"description": text("swagger.openapi.response.success"),
		"content": map[string]interface{}{
			"application/json": map[string]interface{}{
				"schema": map[string]interface{}{"$ref": "#/components/schemas/APIResponse"},
			},
		},
	}
	jsonErr := map[string]interface{}{
		"description": text("swagger.openapi.response.error"),
		"content": map[string]interface{}{
			"application/json": map[string]interface{}{
				"schema": map[string]interface{}{"$ref": "#/components/schemas/APIError"},
			},
		},
	}

	get := func(summary string, params []interface{}) map[string]interface{} {
		op := map[string]interface{}{
			"summary": summary,
			"responses": map[string]interface{}{
				"200": jsonOK,
				"404": jsonErr,
			},
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
		return map[string]interface{}{"get": op}
	}
	graphQLRequestBody := map[string]interface{}{
		"required": true,
		"content": map[string]interface{}{
			"application/json": map[string]interface{}{
				"schema": map[string]interface{}{
					"type":     "object",
					"required": []string{"query"},
					"properties": map[string]interface{}{
						"query":         map[string]interface{}{"type": "string"},
						"operationName": map[string]interface{}{"type": "string"},
						"variables":     map[string]interface{}{"type": "object", "additionalProperties": true},
					},
				},
			},
		},
	}
	graphQLPost := map[string]interface{}{
		"post": map[string]interface{}{
			"summary":     text("swagger.openapi.operation.graphql_execute"),
			"requestBody": graphQLRequestBody,
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": text("swagger.openapi.response.graphql_result"),
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{"schema": map[string]interface{}{"type": "object"}},
					},
				},
				"400": map[string]interface{}{
					"description": text("swagger.openapi.response.graphql_invalid_request"),
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{"schema": map[string]interface{}{"type": "object"}},
					},
				},
				"500": map[string]interface{}{
					"description": text("swagger.openapi.response.graphql_schema_unavailable"),
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{"schema": map[string]interface{}{"type": "object"}},
					},
				},
			},
		},
	}
	preferencesGet := get(text("swagger.openapi.operation.preferences_read"), nil)
	preferencesGet["get"].(map[string]interface{})["operationId"] = "get_api_server_preferences"
	preferencesSyncGet := get(text("swagger.openapi.operation.preferences_export"), nil)
	preferencesSyncGet["get"].(map[string]interface{})["operationId"] = "get_api_server_preferences_sync"
	preferencesImportGet := get(text("swagger.openapi.operation.preferences_import"), nil)
	preferencesImportGet["get"].(map[string]interface{})["operationId"] = "get_api_server_preferences_sync_import"
	preferencesImportGet["get"].(map[string]interface{})["x-mcp-disabled"] = true
	preferencesSavePost := map[string]interface{}{"post": map[string]interface{}{
		"summary":     text("swagger.openapi.operation.preferences_save"),
		"operationId": "post_api_server_preferences",
		"requestBody": map[string]interface{}{"required": true, "content": map[string]interface{}{
			"application/json":                  map[string]interface{}{"schema": map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}}},
			"application/x-www-form-urlencoded": map[string]interface{}{"schema": map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}}},
		}},
		"responses": map[string]interface{}{"200": jsonOK, "400": jsonErr},
	}}
	ownerTokenSecurity := []interface{}{map[string]interface{}{"BearerAuth": []interface{}{}}}
	ownerTokensGet := get(text("swagger.openapi.operation.owner_tokens_list"), nil)
	ownerTokensGetOp := ownerTokensGet["get"].(map[string]interface{})
	ownerTokensGetOp["operationId"] = "get_api_server_preferences_tokens"
	ownerTokensGetOp["x-mcp-disabled"] = true
	ownerTokensGetOp["security"] = ownerTokenSecurity
	ownerTokensPost := map[string]interface{}{"post": map[string]interface{}{
		"operationId":    "post_api_server_preferences_tokens",
		"summary":        text("swagger.openapi.operation.owner_token_create"),
		"security":       ownerTokenSecurity,
		"x-mcp-disabled": true,
		"requestBody": map[string]interface{}{"required": true, "content": map[string]interface{}{
			"application/json": map[string]interface{}{"schema": map[string]interface{}{"type": "object", "required": []string{"name"}, "properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "minLength": 1, "maxLength": 64}, "expiry": map[string]interface{}{"type": "string", "enum": []string{"never", "7d", "30d", "90d", "1y", "custom"}}, "expires_on": map[string]interface{}{"type": "string", "format": "date"},
			}}},
		}},
		"responses": map[string]interface{}{"201": jsonOK, "400": jsonErr, "401": jsonErr, "403": jsonErr, "409": jsonErr},
	}}
	issuePrimaryToken := map[string]interface{}{"post": map[string]interface{}{
		"operationId":    "post_api_server_preferences_tokens_primary",
		"summary":        text("swagger.openapi.operation.primary_token_issue"),
		"x-mcp-disabled": true,
		"responses":      map[string]interface{}{"201": jsonOK, "401": jsonErr, "409": jsonErr, "429": jsonErr},
	}}
	primaryTokenAction := func(operationID, summary string) map[string]interface{} {
		return map[string]interface{}{"post": map[string]interface{}{"operationId": operationID, "summary": summary, "security": ownerTokenSecurity, "x-mcp-disabled": true, "responses": map[string]interface{}{"200": jsonOK, "401": jsonErr, "403": jsonErr, "404": jsonErr, "429": jsonErr}}}
	}
	linkedTokenPath := []interface{}{map[string]interface{}{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "integer", "minimum": 1}}}
	linkedTokenAction := func(method, operationID, summary string) map[string]interface{} {
		operation := map[string]interface{}{"operationId": operationID, "summary": summary, "security": ownerTokenSecurity, "x-mcp-disabled": true, "responses": map[string]interface{}{"200": jsonOK, "401": jsonErr, "403": jsonErr, "404": jsonErr, "429": jsonErr}}
		if method == "patch" {
			operation["requestBody"] = map[string]interface{}{"required": true, "content": map[string]interface{}{"application/json": map[string]interface{}{"schema": map[string]interface{}{"type": "object", "required": []string{"name"}, "properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}, "expiry": map[string]interface{}{"type": "string"}, "expires_on": map[string]interface{}{"type": "string", "format": "date"}}}}}}
		}
		return map[string]interface{}{method: operation}
	}
	linkedTokenPathItem := func(operations ...map[string]interface{}) map[string]interface{} {
		item := map[string]interface{}{"parameters": linkedTokenPath}
		for _, operation := range operations {
			for method, value := range operation {
				item[method] = value
			}
		}
		return item
	}
	mcpPost := map[string]interface{}{
		"post": map[string]interface{}{
			"operationId": "mcp",
			"summary":     text("swagger.openapi.operation.mcp_call"),
			"requestBody": map[string]interface{}{
				"required": true,
				"content":  map[string]interface{}{"application/json": map[string]interface{}{"schema": map[string]interface{}{"type": "object", "required": []string{"jsonrpc", "method"}}}},
			},
			"responses": map[string]interface{}{"200": jsonOK, "400": jsonErr, "403": jsonErr, "405": jsonErr},
		},
	}
	contactPost := map[string]interface{}{
		"post": map[string]interface{}{
			"summary": text("swagger.openapi.operation.contact_submit"),
			"requestBody": map[string]interface{}{
				"required": true,
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{"schema": map[string]interface{}{
						"type": "object", "required": []string{"name", "email"},
						"properties": map[string]interface{}{
							"name": map[string]interface{}{"type": "string"}, "email": map[string]interface{}{"type": "string", "format": "email"},
							"subject": map[string]interface{}{"type": "string"}, "message": map[string]interface{}{"type": "string"},
							"captcha": map[string]interface{}{"type": "string"}, "challenge_id": map[string]interface{}{"type": "string"},
							"security_id": map[string]interface{}{"type": "string"}, "component": map[string]interface{}{"type": "string"},
							"endpoint": map[string]interface{}{"type": "string"}, "severity": map[string]interface{}{"type": "string"},
							"summary": map[string]interface{}{"type": "string"}, "reproduction": map[string]interface{}{"type": "string"},
							"impact": map[string]interface{}{"type": "string"}, "suggested_fix": map[string]interface{}{"type": "string"},
							"researcher_pubkey": map[string]interface{}{"type": "string"}, "coordinated_disclosure": map[string]interface{}{"type": "boolean"},
						},
					}},
				},
			},
			"responses": map[string]interface{}{"200": jsonOK, "400": jsonErr, "503": jsonErr},
		},
	}
	graphQLGet := map[string]interface{}{
		"get": map[string]interface{}{
			"summary": text("swagger.openapi.operation.graphql_execute"),
			"parameters": []interface{}{
				map[string]interface{}{"name": "query", "in": "query", "required": false, "schema": map[string]interface{}{"type": "string"}},
				map[string]interface{}{"name": "variables", "in": "query", "required": false, "description": "JSON-encoded GraphQL variables object", "schema": map[string]interface{}{"type": "string"}},
				map[string]interface{}{"name": "operationName", "in": "query", "required": false, "schema": map[string]interface{}{"type": "string"}},
			},
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": text("swagger.openapi.response.graphql_result"),
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{"schema": map[string]interface{}{"type": "object"}},
						"text/plain":       map[string]interface{}{"schema": map[string]interface{}{"type": "string"}},
					},
				},
				"400": map[string]interface{}{
					"description": text("swagger.openapi.response.graphql_invalid_request"),
					"content":     map[string]interface{}{"application/json": map[string]interface{}{"schema": map[string]interface{}{"type": "object"}}},
				},
			},
		},
	}

	paths := map[string]interface{}{
		"/api/autodiscover":                        get(text("swagger.openapi.operation.autodiscover"), nil),
		"/api/graphql":                             mergeOperations(graphQLGet, graphQLPost),
		"/api/healthz":                             get(text("swagger.openapi.operation.health"), nil),
		"/api/healthz.txt":                         get(text("swagger.openapi.operation.health_text"), nil),
		"/api/mcp":                                 mcpPost,
		"/api/list":                                get(text("swagger.openapi.operation.legacy_list"), nil),
		"/api/{list}":                              get(text("swagger.openapi.operation.legacy_compose"), []interface{}{map[string]interface{}{"name": "list", "in": "path", "required": true, "description": text("swagger.openapi.parameter.template_names"), "schema": map[string]interface{}{"type": "string"}}}),
		"/api/swagger":                             get(text("swagger.openapi.operation.current_openapi"), nil),
		"/mcp":                                     mcpPost,
		api + "/":                                  get(text("swagger.openapi.operation.api_info"), nil),
		api + "/server/healthz":                    get(text("swagger.openapi.operation.health"), nil),
		api + "/server/healthz.txt":                get(text("swagger.openapi.operation.health_text"), nil),
		api + "/server/swagger":                    get(text("swagger.openapi.operation.openapi_document"), nil),
		api + "/server/graphql":                    mergeOperations(graphQLGet, graphQLPost),
		api + "/server/mcp":                        mcpPost,
		api + "/server/security":                   get(text("swagger.openapi.operation.security_info"), nil),
		api + "/server/security/policy":            get(text("swagger.openapi.operation.security_policy"), nil),
		api + "/server/security/thanks":            get(text("swagger.openapi.operation.security_acknowledgments"), nil),
		api + "/server/dpo":                        get(text("swagger.openapi.operation.dpo_contact"), nil),
		api + "/server/about":                      get(text("swagger.openapi.operation.about"), nil),
		api + "/server/privacy":                    get(text("swagger.openapi.operation.privacy"), nil),
		api + "/server/help":                       get(text("swagger.openapi.operation.help"), nil),
		api + "/server/help/mcp":                   get(text("swagger.openapi.operation.mcp_help"), nil),
		api + "/server/terms":                      get(text("swagger.openapi.operation.terms"), nil),
		api + "/server/contact":                    contactPost,
		api + "/server/scheduler":                  get(text("swagger.openapi.operation.scheduler"), nil),
		api + "/server/preferences":                mergeOperations(preferencesGet, preferencesSavePost),
		api + "/server/preferences/tokens":         mergeOperations(ownerTokensGet, ownerTokensPost),
		api + "/server/preferences/tokens/primary": issuePrimaryToken,
		api + "/server/preferences/tokens/primary/reveal": primaryTokenAction("post_api_server_preferences_tokens_primary_reveal", text("swagger.openapi.operation.primary_token_reveal")),
		api + "/server/preferences/tokens/primary/rotate": primaryTokenAction("post_api_server_preferences_tokens_primary_rotate", text("swagger.openapi.operation.primary_token_rotate")),
		api + "/server/preferences/tokens/{id}/reveal":    linkedTokenPathItem(linkedTokenAction("post", "post_api_server_preferences_tokens_id_reveal", text("swagger.openapi.operation.linked_token_reveal"))),
		api + "/server/preferences/tokens/{id}/rotate":    linkedTokenPathItem(linkedTokenAction("post", "post_api_server_preferences_tokens_id_rotate", text("swagger.openapi.operation.linked_token_rotate"))),
		api + "/server/preferences/tokens/{id}":           linkedTokenPathItem(linkedTokenAction("patch", "patch_api_server_preferences_tokens_id", text("swagger.openapi.operation.linked_token_update")), linkedTokenAction("delete", "delete_api_server_preferences_tokens_id", text("swagger.openapi.operation.linked_token_revoke"))),
		api + "/server/preferences/sync":                  preferencesSyncGet,
		api + "/server/preferences/sync/import":           preferencesImportGet,
		api + "/list":                                     get(text("swagger.openapi.operation.templates_list"), nil),
		api + "/list.txt":                                 get(text("swagger.openapi.operation.templates_list_text"), nil),
		api + "/categories":                               get(text("swagger.openapi.operation.categories_list"), nil),
		api + "/categories.txt":                           get(text("swagger.openapi.operation.categories_list_text"), nil),
		api + "/categories/{name}":                        get(text("swagger.openapi.operation.category_templates_list"), []interface{}{templateName}),
		api + "/categories/{name}.txt":                    get(text("swagger.openapi.operation.category_templates_text"), []interface{}{templateName}),
		api + "/stats":                                    get(text("swagger.openapi.operation.template_stats"), nil),
		api + "/stats.txt":                                get(text("swagger.openapi.operation.template_stats_text"), nil),
		api + "/templates/{name}":                         get(text("swagger.openapi.operation.template_get"), []interface{}{templateName}),
		api + "/templates/{name}.txt":                     get(text("swagger.openapi.operation.template_get_text"), []interface{}{templateName}),
		api + "/templates/{name}.json":                    get(text("swagger.openapi.operation.template_get_json"), []interface{}{templateName}),
		api + "/templates.json":                           get(text("swagger.openapi.operation.templates_export"), nil),
		api + "/templates.tar.gz":                         get(text("swagger.openapi.operation.templates_archive"), nil),
		api + "/search": get(text("swagger.openapi.operation.search"), []interface{}{
			map[string]interface{}{"name": "q", "in": "query", "required": true, "description": text("swagger.openapi.parameter.search_query"), "schema": map[string]interface{}{"type": "string"}},
		}),
		api + "/search.txt": get(text("swagger.openapi.operation.search_text"), []interface{}{
			map[string]interface{}{"name": "q", "in": "query", "required": true, "description": text("swagger.openapi.parameter.search_query"), "schema": map[string]interface{}{"type": "string"}},
		}),
		api + "/combine": get(text("swagger.openapi.operation.combine"), []interface{}{
			map[string]interface{}{"name": "templates", "in": "query", "required": true, "description": text("swagger.openapi.parameter.template_names"), "schema": map[string]interface{}{"type": "string"}},
		}),
		api + "/combine.txt": get(text("swagger.openapi.operation.combine_text"), []interface{}{
			map[string]interface{}{"name": "templates", "in": "query", "required": true, "description": text("swagger.openapi.parameter.template_names"), "schema": map[string]interface{}{"type": "string"}},
		}),
		api + "/cli/sh":              get(text("swagger.openapi.operation.cli_posix"), nil),
		api + "/cli/ps":              get(text("swagger.openapi.operation.cli_powershell"), nil),
		api + "/cli/completion/bash": get(text("swagger.openapi.operation.completion_bash"), nil),
		api + "/cli/completion/zsh":  get(text("swagger.openapi.operation.completion_zsh"), nil),
		api + "/cli/completion/fish": get(text("swagger.openapi.operation.completion_fish"), nil),
	}
	for path, raw := range paths {
		if !strings.HasPrefix(path, api+"/") {
			continue
		}
		operations, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if operation, ok := operations["get"].(map[string]interface{}); ok {
			operation["operationId"] = mcpOperationID(path)
		}
	}

	return map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":       text("swagger.openapi.info.title"),
			"description": text("swagger.openapi.info.description"),
			"version":     s.config.Version,
		},
		"servers": []interface{}{
			map[string]interface{}{"url": base},
		},
		"paths": paths,
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{"BearerAuth": map[string]interface{}{"type": "http", "scheme": "bearer"}},
			"schemas": map[string]interface{}{
				"APIResponse": okEnvelope,
				"APIError":    errEnvelope,
			},
		},
	}
}
