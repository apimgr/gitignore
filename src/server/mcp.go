package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const mcpProtocolVersion = "2026-07-28"

type mcpMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Meta    json.RawMessage `json:"_meta"`
}

type mcpTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
	Annotations map[string]bool        `json:"annotations"`
	Path        string                 `json:"-"`
	PathParams  []string               `json:"-"`
	QueryParams []string               `json:"-"`
}

type mcpToolCall struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if !s.validMCPOrigin(r) {
		s.writeMCPError(w, nil, -32003, "request origin is not allowed", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		sendAPIResponseError(w, "METHOD_NOT_ALLOWED", "MCP endpoint accepts POST requests only")
		return
	}
	rawToken, ok := parseBearerHeader(r.Header.Get("Authorization"))
	if !ok {
		s.writeMCPUnauthorized(w, nil)
		return
	}
	if _, err := s.AuthenticateBearerToken(rawToken); err != nil {
		s.writeMCPUnauthorized(w, nil)
		return
	}
	if !strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		s.writeMCPError(w, nil, -32600, "MCP requests must use application/json", http.StatusBadRequest)
		return
	}
	accept := r.Header.Get("Accept")
	if !strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/event-stream") {
		s.writeMCPError(w, nil, -32600, "MCP clients must accept application/json or text/event-stream", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var message mcpMessage
	if err := json.NewDecoder(r.Body).Decode(&message); err != nil || message.JSONRPC != "2.0" || message.Method == "" {
		s.writeMCPError(w, nil, -32700, "invalid JSON-RPC request", http.StatusBadRequest)
		return
	}
	params := map[string]json.RawMessage{}
	if len(message.Params) != 0 {
		if err := json.Unmarshal(message.Params, &params); err != nil {
			s.writeMCPError(w, message.ID, -32600, "invalid request params", http.StatusBadRequest)
			return
		}
	}
	var meta map[string]string
	if len(message.Meta) > 0 {
		_ = json.Unmarshal(message.Meta, &meta)
	}
	if rawMeta := params["_meta"]; len(rawMeta) > 0 {
		var paramsMeta map[string]string
		_ = json.Unmarshal(rawMeta, &paramsMeta)
		if meta == nil {
			meta = paramsMeta
		} else if paramsMeta["io.modelcontextprotocol/protocolVersion"] != meta["io.modelcontextprotocol/protocolVersion"] {
			s.writeMCPError(w, message.ID, -32020, "HeaderMismatch: conflicting protocol metadata", http.StatusBadRequest)
			return
		}
	}
	version := r.Header.Get("MCP-Protocol-Version")
	if message.Method == "initialize" && len(meta) == 0 || version != mcpProtocolVersion {
		s.writeMCPError(w, message.ID, -32000, "UnsupportedProtocolVersionError: supported version is "+mcpProtocolVersion, http.StatusBadRequest)
		return
	}
	if meta["io.modelcontextprotocol/protocolVersion"] != version {
		s.writeMCPError(w, message.ID, -32020, "HeaderMismatch: MCP-Protocol-Version does not match request metadata", http.StatusBadRequest)
		return
	}
	if r.Header.Get("Mcp-Method") != message.Method {
		s.writeMCPError(w, message.ID, -32020, "HeaderMismatch: Mcp-Method does not match request method", http.StatusBadRequest)
		return
	}
	if message.Method == "tools/call" || message.Method == "resources/read" || message.Method == "prompts/get" {
		var named struct {
			Name string `json:"name"`
			URI  string `json:"uri"`
		}
		_ = json.Unmarshal(params["name"], &named.Name)
		_ = json.Unmarshal(params["uri"], &named.URI)
		want := named.Name
		if want == "" {
			want = named.URI
		}
		got, decodeErr := decodeMCPName(r.Header.Get("Mcp-Name"))
		if decodeErr != nil || want == "" || got != want {
			s.writeMCPError(w, message.ID, -32020, "HeaderMismatch: Mcp-Name does not match request name", http.StatusBadRequest)
			return
		}
	}
	if !mcpMethodKnown(message.Method) {
		s.writeMCPError(w, message.ID, -32601, "Method not found", http.StatusNotFound)
		return
	}
	result, rpcError := s.dispatchMCP(r, message)
	response := map[string]interface{}{"jsonrpc": "2.0", "id": json.RawMessage(message.ID)}
	if len(message.ID) == 0 || string(message.ID) == "null" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if rpcError != nil {
		response["error"] = rpcError
	} else {
		response["result"] = result
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		s.writeMCPError(w, message.ID, -32603, "internal error", http.StatusInternalServerError)
		return
	}
	if strings.Contains(accept, "text/event-stream") && !strings.Contains(accept, "application/json") {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", encoded)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(append(encoded, '\n'))
}

func decodeMCPName(value string) (string, error) {
	if strings.HasPrefix(strings.ToLower(value), "=?base64?") && strings.HasSuffix(value, "?=") {
		decoded, err := base64.StdEncoding.DecodeString(value[len("=?base64?") : len(value)-2])
		return string(decoded), err
	}
	if strings.HasPrefix(value, "=?") {
		return new(mime.WordDecoder).Decode(value)
	}
	for _, r := range value {
		if r > 0x7f {
			return "", fmt.Errorf("Mcp-Name must encode non-ASCII characters")
		}
	}
	return value, nil
}

func mcpMethodKnown(method string) bool {
	switch method {
	case "initialize", "ping", "tools/list", "tools/call":
		return true
	default:
		return false
	}
}

func (s *Server) writeMCPUnauthorized(w http.ResponseWriter, id interface{}) {
	w.Header().Set("WWW-Authenticate", `Bearer`)
	s.writeMCPError(w, id, -32001, "Unauthorized", http.StatusUnauthorized)
}

func (s *Server) dispatchMCP(r *http.Request, message mcpMessage) (interface{}, map[string]interface{}) {
	switch message.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil || params.ProtocolVersion != mcpProtocolVersion {
			return nil, map[string]interface{}{"code": -32000, "message": "UnsupportedProtocolVersionError: supported version is " + mcpProtocolVersion}
		}
		return map[string]interface{}{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]interface{}{"name": "gitignore", "version": s.config.Version},
		}, nil
	case "ping":
		return map[string]interface{}{}, nil
	case "tools/list":
		return map[string]interface{}{"tools": s.mcpTools()}, nil
	case "tools/call":
		var call mcpToolCall
		if err := json.Unmarshal(message.Params, &call); err != nil {
			return nil, map[string]interface{}{"code": -32602, "message": "invalid tool arguments"}
		}
		return s.callMCPTool(r, call)
	default:
		return nil, map[string]interface{}{"code": -32601, "message": "method not found"}
	}
}

func (s *Server) mcpTools() []mcpTool {
	tools := make([]mcpTool, 0)
	spec := s.openAPISpec(httptest.NewRequest(http.MethodGet, "/", nil))
	specPaths, _ := spec["paths"].(map[string]interface{})
	err := chi.Walk(s.router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method != http.MethodGet || !strings.HasPrefix(route, s.apiBasePath()+"/") || strings.Contains(route, "/server/mcp") {
			return nil
		}
		pathItem, ok := specPaths[route].(map[string]interface{})
		if !ok {
			return nil
		}
		operation, ok := pathItem["get"].(map[string]interface{})
		if !ok {
			return nil
		}
		if operation["x-mcp-disabled"] == true {
			return nil
		}
		name, _ := operation["operationId"].(string)
		description, _ := operation["summary"].(string)
		properties := map[string]interface{}{}
		required := make([]string, 0)
		pathParams := []string{}
		queryParams := []string{}
		if params, ok := operation["parameters"].([]interface{}); ok {
			for _, raw := range params {
				parameter, ok := raw.(map[string]interface{})
				if !ok {
					continue
				}
				parameterName, _ := parameter["name"].(string)
				parameterIn, _ := parameter["in"].(string)
				parameterSchema, _ := parameter["schema"].(map[string]interface{})
				if parameterName == "" {
					continue
				}
				properties[parameterName] = parameterSchema
				switch parameterIn {
				case "path":
					pathParams = append(pathParams, parameterName)
				case "query":
					queryParams = append(queryParams, parameterName)
				}
				if parameter["required"] == true {
					required = append(required, parameterName)
				}
			}
		}
		tools = append(tools, mcpTool{
			Name:        name,
			Description: description,
			InputSchema: map[string]interface{}{"type": "object", "properties": properties, "required": required},
			Annotations: map[string]bool{"readOnlyHint": true},
			Path:        route,
			PathParams:  pathParams,
			QueryParams: queryParams,
		})
		return nil
	})
	if err != nil {
		return []mcpTool{}
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}

func mcpOperationID(route string) string {
	name := strings.Trim(route, "/")
	name = strings.ReplaceAll(name, "{", "")
	name = strings.ReplaceAll(name, "}", "")
	name = strings.NewReplacer("/", "_", ".", "_", "-", "_").Replace(name)
	return "get_" + name
}

func (s *Server) callMCPTool(r *http.Request, call mcpToolCall) (interface{}, map[string]interface{}) {
	var tool *mcpTool
	for _, candidate := range s.mcpTools() {
		if candidate.Name == call.Name {
			tool = &candidate
			break
		}
	}
	if tool == nil {
		return nil, map[string]interface{}{"code": -32602, "message": "unknown tool"}
	}
	path := tool.Path
	for _, parameter := range tool.PathParams {
		value, ok := call.Arguments[parameter].(string)
		if !ok || value == "" || strings.Contains(value, "/") || value == "." || value == ".." {
			return nil, map[string]interface{}{"code": -32602, "message": "invalid path argument"}
		}
		path = strings.Replace(path, "{"+parameter+"}", url.PathEscape(value), 1)
	}
	query := url.Values{}
	allowedArguments := make(map[string]bool, len(tool.PathParams)+len(tool.QueryParams))
	for _, parameter := range tool.PathParams {
		allowedArguments[parameter] = true
	}
	for _, parameter := range tool.QueryParams {
		allowedArguments[parameter] = true
		if raw, exists := call.Arguments[parameter]; exists {
			switch value := raw.(type) {
			case string:
				query.Set(parameter, value)
			case float64, bool:
				query.Set(parameter, fmt.Sprint(value))
			default:
				return nil, map[string]interface{}{"code": -32602, "message": "query values must be scalar"}
			}
		}
	}
	for key := range call.Arguments {
		if !allowedArguments[key] {
			return nil, map[string]interface{}{"code": -32602, "message": "unknown tool argument"}
		}
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header = r.Header.Clone()
	request.Header.Set("Accept", "application/json")
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, request)
	body := bytes.TrimSpace(response.Body.Bytes())
	if response.Code >= http.StatusBadRequest {
		return map[string]interface{}{"content": []map[string]string{{"type": "text", "text": string(body)}}, "isError": true}, nil
	}
	if !json.Valid(body) {
		body, _ = json.Marshal(string(body))
	}
	return map[string]interface{}{"content": []map[string]string{{"type": "text", "text": string(body)}}, "isError": false}, nil
}

func (s *Server) validMCPOrigin(r *http.Request) bool {
	if r == nil {
		return true
	}
	rawOrigin := strings.TrimSpace(r.Header.Get("Origin"))
	if rawOrigin == "" {
		return true
	}
	origin, valid := canonicalOrigin(rawOrigin)
	if !valid {
		return false
	}
	if ownOrigin, ok := canonicalOrigin(s.detectServerURL(r)); ok && origin == ownOrigin {
		return true
	}
	if s.currentConfig() != nil {
		for _, configured := range s.currentConfig().Server.CORS.AllowedOrigins {
			if strings.TrimSpace(configured) == "*" {
				continue
			}
			if explicit, ok := canonicalOrigin(configured); ok && origin == explicit {
				return true
			}
		}
	}
	for _, domain := range strings.FieldsFunc(os.Getenv("DOMAIN"), func(value rune) bool {
		return value == ',' || value == ';' || value == ' ' || value == '\t' || value == '\n'
	}) {
		candidate := domain
		if !strings.Contains(candidate, "://") {
			candidate = "https://" + candidate
		}
		if configured, ok := canonicalOrigin(candidate); ok && origin == configured {
			return true
		}
	}
	s.learnForwardedOrigin(r)
	return s.hasLearnedOrigin(origin, time.Now())
}

func (s *Server) writeMCPError(w http.ResponseWriter, id interface{}, code int, message string, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": id, "error": map[string]interface{}{"code": code, "message": message}})
}
