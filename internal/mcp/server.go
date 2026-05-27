package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/somoore/auto-bot/internal/agent"
)

// Protocol-level constants for the minimal JSON-RPC 2.0 / MCP slice. Full MCP
// negotiation (capabilities, prompts, resources) lands in S2.1 once we know
// which surface the connected clients actually exercise.
const (
	ProtocolVersion = "auto-bot-mcp/0.1"
	ServerName      = "auto-bot-mcpd"
	ServerVersion   = "0.1.0"
)

// JSON-RPC 2.0 error codes. Only a handful are surfaced today; we name them
// so callers (and tests) can assert against the constants rather than magic
// numbers.
const (
	ErrCodeParse          = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603
)

// Request is a JSON-RPC 2.0 request envelope. ID may be a string, number, or
// null; we round-trip it as json.RawMessage so notifications (missing ID) and
// numeric IDs from non-Go clients stay intact.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is the JSON-RPC 2.0 response envelope.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is the JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// Tool describes one callable MCP tool.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Handler     ToolHandler    `json:"-"`
}

// ToolHandler is the signature every MCP tool implements.
type ToolHandler func(ctx context.Context, params json.RawMessage) (any, error)

// Server is the protocol layer. It owns a registry of tools and dispatches
// JSON-RPC requests over both stdio and HTTP transports.
//
// Authentication model (S2.2 / #58):
//
//   - HTTP transport: every request must carry a Bearer token issued by
//     cmd/server's POST /admin/mcp-tokens endpoint. Verifier resolves the
//     token to Claims (subject, tenant, scopes, jti) and the dispatcher
//     enforces per-tool scope requirements declared in ToolScopes.
//   - Stdio transport: the client process is local; the OS process
//     boundary is the trust boundary. StdioClaims provides the (subject,
//     tenant, scopes) the server treats every stdio call as carrying.
//     Operators configure it via the cmd/mcpd flag set; tests can mint
//     an in-process Claims directly.
//
// Setting Verifier to nil disables HTTP auth — only appropriate for
// tests; production cmd/mcpd refuses to serve HTTP without a Verifier.
type Server struct {
	mu          sync.RWMutex
	tools       map[string]Tool
	order       []string
	Verifier    *Verifier
	StdioClaims Claims
}

// NewServer returns a Server with the supplied tools registered in order.
func NewServer(tools []Tool) *Server {
	s := &Server{tools: map[string]Tool{}}
	for _, t := range tools {
		if t.Name == "" {
			panic("mcp: tool name is required")
		}
		if _, exists := s.tools[t.Name]; exists {
			panic(fmt.Sprintf("mcp: duplicate tool registration: %s", t.Name))
		}
		s.tools[t.Name] = t
		s.order = append(s.order, t.Name)
	}
	return s
}

// Tools returns the registered tool list in registration order.
func (s *Server) Tools() []Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Tool, 0, len(s.order))
	for _, name := range s.order {
		out = append(out, s.tools[name])
	}
	return out
}

// claimsContextKey carries the verified Claims through context so tool
// handlers (and any future middleware) can read the caller's identity
// + scopes without an out-of-band channel.
type claimsContextKey struct{}

// WithClaims attaches claims to ctx. Used by HTTPHandler after a
// successful Verify and by ServeStdio when StdioClaims is configured.
func WithClaims(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey{}, c)
}

// ClaimsFromContext returns the claims attached by WithClaims, if any.
// Tool handlers use this to read the caller's tenant + subject when
// they need to scope their work; absent claims means an unauthenticated
// path (test fixtures only).
func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsContextKey{}).(Claims)
	return c, ok
}

// HandleRequest dispatches a single decoded request and returns the response.
func (s *Server) HandleRequest(ctx context.Context, req Request) *Response {
	if req.JSONRPC != "" && req.JSONRPC != "2.0" {
		return s.errorResponse(req.ID, ErrCodeInvalidRequest, "jsonrpc must be \"2.0\"", nil)
	}
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	default:
		return s.errorResponse(req.ID, ErrCodeMethodNotFound, fmt.Sprintf("method not found: %s", req.Method), nil)
	}
}

// InitializeResult is the payload returned from the initialize handshake.
type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	ServerInfo      ServerInfo     `json:"serverInfo"`
	Capabilities    map[string]any `json:"capabilities"`
	Tools           []Tool         `json:"tools"`
}

// ServerInfo is the {name, version} pair embedded in InitializeResult.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (s *Server) handleInitialize(req Request) *Response {
	result := InitializeResult{
		ProtocolVersion: ProtocolVersion,
		ServerInfo:      ServerInfo{Name: ServerName, Version: ServerVersion},
		Capabilities:    map[string]any{"tools": map[string]any{}},
		Tools:           s.Tools(),
	}
	return &Response{JSONRPC: "2.0", ID: req.ID, Result: result}
}

// ToolsListResult is the payload of tools/list.
type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

func (s *Server) handleToolsList(req Request) *Response {
	return &Response{JSONRPC: "2.0", ID: req.ID, Result: ToolsListResult{Tools: s.Tools()}}
}

// ToolCallParams is the inbound shape of tools/call.
type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolCallResult wraps the handler's return value in a {content: [...]} envelope.
type ToolCallResult struct {
	Content []ToolCallContent `json:"content"`
	IsError bool              `json:"isError,omitempty"`
	Data    any               `json:"data,omitempty"`
}

// ToolCallContent is one element of ToolCallResult.Content.
type ToolCallContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ErrCodeForbidden is the JSON-RPC error code returned when a caller is
// authenticated but lacks the scope a tool requires. -32001 sits in the
// JSON-RPC 2.0 implementation-defined range (-32000 to -32099) per the
// spec; we name it so clients can distinguish "you don't have permission"
// from "tool not found" or "bad parameters".
const ErrCodeForbidden = -32001

func (s *Server) handleToolsCall(ctx context.Context, req Request) *Response {
	if len(req.Params) == 0 {
		return s.errorResponse(req.ID, ErrCodeInvalidParams, "tools/call requires params", nil)
	}
	var p ToolCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return s.errorResponse(req.ID, ErrCodeInvalidParams, fmt.Sprintf("invalid params: %v", err), nil)
	}
	if p.Name == "" {
		return s.errorResponse(req.ID, ErrCodeInvalidParams, "tools/call: name is required", nil)
	}
	s.mu.RLock()
	tool, ok := s.tools[p.Name]
	s.mu.RUnlock()
	if !ok {
		return s.errorResponse(req.ID, ErrCodeMethodNotFound, fmt.Sprintf("unknown tool: %s", p.Name), nil)
	}
	if tool.Handler == nil {
		return s.errorResponse(req.ID, ErrCodeInternal, fmt.Sprintf("tool %s has no handler", p.Name), nil)
	}
	// Centralized scope check. Tool authors declare requirements in
	// ToolScopes (internal/mcp/scopes.go); the dispatcher enforces them
	// here so individual handlers cannot forget. A tool absent from
	// ToolScopes has no scope requirement.
	if required, gated := ToolScopes[p.Name]; gated {
		claims, present := ClaimsFromContext(ctx)
		if !present {
			return s.errorResponse(req.ID, ErrCodeForbidden, fmt.Sprintf("tool %s requires authentication", p.Name), nil)
		}
		if !claims.HasScope(required) {
			return s.errorResponse(req.ID, ErrCodeForbidden, fmt.Sprintf("tool %s requires scope %q", p.Name, required), nil)
		}
	}
	out, err := tool.Handler(ctx, p.Arguments)
	if err != nil {
		// SE-1 F6: surface RunStore "not found" sentinels as JSON-RPC
		// InvalidParams (-32602) so MCP clients can distinguish "the run
		// or question id you sent does not exist" from generic internal
		// errors (-32603). Other errors continue to land in the
		// IsError-styled tool result envelope, which preserves the
		// JSON-RPC contract while letting tool authors return free-form
		// failure context. Use errors.Is so wrapped chains (e.g.
		// "load run %s: %w" upstream) keep mapping correctly.
		if errors.Is(err, agent.ErrRunNotFound) {
			return s.errorResponse(req.ID, ErrCodeInvalidParams, fmt.Sprintf("run not found: %v", err), nil)
		}
		if errors.Is(err, agent.ErrRunQuestionNotFound) {
			return s.errorResponse(req.ID, ErrCodeInvalidParams, fmt.Sprintf("run question not found: %v", err), nil)
		}
		text := err.Error()
		return &Response{JSONRPC: "2.0", ID: req.ID, Result: ToolCallResult{
			Content: []ToolCallContent{{Type: "text", Text: text}},
			IsError: true,
		}}
	}
	text, mErr := json.Marshal(out)
	if mErr != nil {
		return s.errorResponse(req.ID, ErrCodeInternal, fmt.Sprintf("marshal tool result: %v", mErr), nil)
	}
	return &Response{JSONRPC: "2.0", ID: req.ID, Result: ToolCallResult{
		Content: []ToolCallContent{{Type: "text", Text: string(text)}},
		Data:    out,
	}}
}

func (s *Server) errorResponse(id json.RawMessage, code int, message string, data any) *Response {
	return &Response{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: message, Data: data}}
}

// ServeStdio reads newline-delimited JSON-RPC requests from r and writes
// responses to w. Each request is its own line; ordering is preserved.
// The trust boundary on stdio is the OS process boundary — operators
// pre-configure StdioClaims on the Server to declare what scopes the
// local caller holds. Without StdioClaims set, tool calls that require
// any scope return -32001 Forbidden.
func (s *Server) ServeStdio(ctx context.Context, r io.Reader, w io.Writer) error {
	if s.StdioClaims.Subject != "" {
		ctx = WithClaims(ctx, s.StdioClaims)
	}
	dec := json.NewDecoder(bufio.NewReader(r))
	enc := json.NewEncoder(w)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var req Request
		if err := dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			resp := s.errorResponse(nil, ErrCodeParse, fmt.Sprintf("parse error: %v", err), nil)
			if encErr := enc.Encode(resp); encErr != nil {
				return encErr
			}
			return err
		}
		resp := s.HandleRequest(ctx, req)
		if resp == nil {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
}

// HTTPHandler returns an http.Handler that serves JSON-RPC requests.
// A non-nil Verifier is required for the handler to admit requests —
// the handler returns 503 on every call when Verifier is nil so a
// missing-keys boot bug surfaces as service unavailable, not silent
// anonymous access.
func (s *Server) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.Verifier == nil {
			http.Error(w, "mcp http transport requires a Verifier", http.StatusServiceUnavailable)
			return
		}
		token, ok := extractBearerToken(r.Header.Get("Authorization"))
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
			http.Error(w, "unauthorized: bearer token required", http.StatusUnauthorized)
			return
		}
		claims, err := s.Verifier.Verify(token)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp", error="invalid_token"`)
			http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)
			return
		}
		ctx := WithClaims(r.Context(), claims)
		r = r.WithContext(ctx)
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		body = []byte(strings.TrimSpace(string(body)))
		if len(body) == 0 {
			http.Error(w, "empty body", http.StatusBadRequest)
			return
		}
		var req Request
		if err := json.Unmarshal(body, &req); err != nil {
			writeJSON(w, s.errorResponse(nil, ErrCodeParse, fmt.Sprintf("parse error: %v", err), nil))
			return
		}
		resp := s.HandleRequest(r.Context(), req)
		if resp == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, resp)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
