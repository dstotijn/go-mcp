// Copyright 2025 David Stotijn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/dstotijn/go-mcp/internal/jsonrpc"
	"github.com/dstotijn/valtor/valtorjsonschema"
	"github.com/invopop/jsonschema"
)

// ProtocolVersion defines the protocol version this implementation supports.
const ProtocolVersion = "2025-03-26"

var jsonschemaReflector = jsonschema.Reflector{
	Anonymous:      true,
	ExpandedStruct: true,
}

type ctxKey int

const sessionKey ctxKey = 0

type Server struct {
	name     string
	version  string
	endpoint *url.URL

	sessions   map[string]*Session
	sessionsMu sync.RWMutex

	tools   map[string]Tool
	toolsMu sync.RWMutex

	listResourcesFn         func(ctx context.Context, params ListResourcesParams) (*ListResourcesResult, error)
	readResourceFn          func(ctx context.Context, params ReadResourceParams) (*ReadResourceResult, error)
	listResourceTemplatesFn func(ctx context.Context, params ListResourceTemplatesParams) (*ListResourceTemplatesResult, error)
	listPromptsFn           func(ctx context.Context, params ListPromptsParams) (*ListPromptsResult, error)
	getPromptFn             func(ctx context.Context, params GetPromptParams) (*GetPromptResult, error)
	completeFn              func(ctx context.Context, params CompleteParams) (*CompleteResult, error)
	onClientInitializedFn   func(ctx context.Context, session *Session)
	onRootsListChangedFn    func(ctx context.Context, session *Session)
	onSubscribeResourceFn   func(ctx context.Context, session *Session, params ResourceSubscribeParams) error
}

type ServerOption func(*Server)

type ServerConfig struct {
	Name    string
	Version string

	ListResourcesFn         func(ctx context.Context, params ListResourcesParams) (*ListResourcesResult, error)
	ReadResourceFn          func(ctx context.Context, params ReadResourceParams) (*ReadResourceResult, error)
	ListResourceTemplatesFn func(ctx context.Context, params ListResourceTemplatesParams) (*ListResourceTemplatesResult, error)
	ListRootsFn             func(ctx context.Context, params ListRootsParams) (*ListRootsResult, error)
	ListPromptsFn           func(ctx context.Context, params ListPromptsParams) (*ListPromptsResult, error)
	GetPromptFn             func(ctx context.Context, params GetPromptParams) (*GetPromptResult, error)
	CompleteFn              func(ctx context.Context, params CompleteParams) (*CompleteResult, error)
	OnClientInitializedFn   func(ctx context.Context, session *Session)
	OnRootsListChangedFn    func(ctx context.Context, session *Session)
	OnSubscribeResourceFn   func(ctx context.Context, session *Session, params ResourceSubscribeParams) error
}

func NewServer(cfg ServerConfig, opts ...ServerOption) *Server {
	s := &Server{
		sessions:                make(map[string]*Session),
		tools:                   make(map[string]Tool),
		listResourcesFn:         cfg.ListResourcesFn,
		readResourceFn:          cfg.ReadResourceFn,
		listResourceTemplatesFn: cfg.ListResourceTemplatesFn,
		listPromptsFn:           cfg.ListPromptsFn,
		getPromptFn:             cfg.GetPromptFn,
		completeFn:              cfg.CompleteFn,
		onClientInitializedFn:   cfg.OnClientInitializedFn,
		onRootsListChangedFn:    cfg.OnRootsListChangedFn,
		onSubscribeResourceFn:   cfg.OnSubscribeResourceFn,
		name:                    cfg.Name,
		version:                 cfg.Version,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// WithStdioTransport returns a ServerOption that configures the server to use stdin/stdout for transport.
func WithStdioTransport() ServerOption {
	return func(s *Server) {
		session := newSession(StdioSessionID, nil)
		handleCtx := context.WithValue(context.Background(), sessionKey, session)
		conn := jsonrpc.NewConn(jsonrpc.ConnConfig{
			ReadWriter:        stdioRW,
			Handler:           s,
			HandleContext:     handleCtx,
			NewRequestID:      session.newRequestID,
			OnResponse:        session.handleResponse,
			AddPendingRequest: session.addPendingRequest,
		})
		session.conn = conn

		s.sessionsMu.Lock()
		s.sessions[session.id] = session
		s.sessionsMu.Unlock()
	}
}

// WithStreamableHTTPTransport returns a ServerOption that configures the server
// to use the Streamable HTTP transport.
//
// See: https://modelcontextprotocol.io/specification/2025-03-26/basic/transports#streamable-http
func WithStreamableHTTPTransport(endpoint url.URL) ServerOption {
	return func(s *Server) {
		s.endpoint = &endpoint
	}
}

// Start starts the MCP server.
func (s *Server) Start(ctx context.Context) {
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()

	for _, session := range s.sessions {
		go session.conn.Listen(ctx)
	}
}

// RegisterTools registers one or more tools with the server.
func (s *Server) RegisterTools(tools ...Tool) {
	s.toolsMu.Lock()
	defer s.toolsMu.Unlock()

	for _, tool := range tools {
		s.tools[tool.Name] = tool
	}

	s.NotifyToolsListChanged(context.Background())
}

func (s *Server) UnregisterTools(names ...string) {
	s.toolsMu.Lock()
	defer s.toolsMu.Unlock()

	for _, name := range names {
		delete(s.tools, name)
	}
}

// NotifyResourcesListChanged sends a notification to all clients that the resources list has changed.
func (s *Server) NotifyResourcesListChanged(ctx context.Context) {
	s.notifyClients(ctx, "notifications/resources/list_changed", nil)
}

// NotifyPromptsListChanged sends a notification to all clients that the prompts list has changed.
func (s *Server) NotifyPromptsListChanged(ctx context.Context) {
	s.notifyClients(ctx, "notifications/prompts/list_changed", nil)
}

// NotifyToolsListChanged sends a notification to all clients that the tools list has changed.
func (s *Server) NotifyToolsListChanged(ctx context.Context) {
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()

	for _, session := range s.sessions {
		if !session.initialized {
			continue
		}
		if session.conn != nil {
			go func() {
				if err := session.conn.Notify(ctx, "notifications/tools/list_changed", nil); err != nil {
					log.Printf("Failed to notify client: %v", err)
				}
			}()
		}
	}
}

// ServeHTTP implements [http.Handler].
func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if s.endpoint == nil {
		http.NotFound(w, req)
		return
	}

	switch req.Method {
	case "POST":
		if req.URL.Query().Has("sessionId") {
			s.handleJSONRPCRequest(w, req)
		} else {
			s.handleStreamableHTTP(w, req)
		}
		return
	case "GET":
		s.handleSSE(w, req)
		return
	case "DELETE":
		s.handleDeleteSession(w, req)
		return
	case "OPTIONS":
		w.Header().Set("Allow", "GET, POST, DELETE, OPTIONS")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleSSE(w http.ResponseWriter, req *http.Request) {
	var session *Session
	sessionID := req.Header.Get("Mcp-Session-Id")

	if sessionID == "" {
		session = newSession("", nil)
		s.sessionsMu.Lock()
		s.sessions[session.id] = session
		s.sessionsMu.Unlock()
	} else {
		s.sessionsMu.RLock()
		var ok bool
		session, ok = s.sessions[sessionID]
		s.sessionsMu.RUnlock()
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			rpcErr := jsonrpc.ErrInvalidRequest.WithData(map[string]any{
				"detail": "Session not found",
			})
			_ = json.NewEncoder(w).Encode(rpcErr)
			return
		}
	}

	stream := session.addStream()

	// We're *not* deriving from the request context here, because the request
	// might timeout or be canceled, and we want to keep handling any in-flight
	// messages.
	handleCtx := context.WithValue(context.Background(), sessionKey, session)
	conn := jsonrpc.NewConn(jsonrpc.ConnConfig{
		ReadWriter:        stream.newClientLocalConn(),
		Handler:           s,
		HandleContext:     handleCtx,
		NewRequestID:      session.newRequestID,
		OnResponse:        session.handleResponse,
		AddPendingRequest: session.addPendingRequest,
	})
	session.conn = conn

	go conn.Listen(req.Context())

	rc := http.NewResponseController(w)
	writeSSEHeaders(w)

	// If the session ID was not provied in the `Mcp-Session-Id` header,
	// we're handling a legacy "SSE transport" request.
	if sessionID == "" {
		session.sseWriter = stream.serverConn
		// As opposed to the "Streamable HTTP" transport, we should close
		// the stream after the request is handled (e.g. when the connection
		// is closed).
		defer stream.close()

		// Announce the endpoint URL that MCP clients should use for subsequent
		// JSON-RPC requests.
		sseEndpoint := *s.endpoint
		q := sseEndpoint.Query()
		q.Set("sessionId", session.id)
		sseEndpoint.RawQuery = q.Encode()

		writeServerSentEvent(w, "endpoint", "", sseEndpoint.String())
		rc.Flush()
	}

	stream.writeEvents(req.Context(), nil, w, rc)
}

func (s *Server) handleStreamableHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	acceptTypes := make([]string, 0)
	for acceptType := range strings.SplitSeq(req.Header.Get("Accept"), ",") {
		acceptTypes = append(acceptTypes, strings.ToLower(strings.TrimSpace(acceptType)))
	}

	if !slices.Contains(acceptTypes, "application/json") || !slices.Contains(acceptTypes, "text/event-stream") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		rpcErr := jsonrpc.ErrInvalidRequest.WithData(map[string]any{
			"detail": "Invalid Accept header: must include 'application/json' and 'text/event-stream'.",
		})
		_ = json.NewEncoder(w).Encode(rpcErr)
		return
	}

	var session *Session
	sessionID := req.Header.Get("Mcp-Session-Id")

	if sessionID == "" {
		session = newSession("", nil)
		s.sessionsMu.Lock()
		s.sessions[session.id] = session
		s.sessionsMu.Unlock()
	} else {
		s.sessionsMu.RLock()
		var ok bool
		session, ok = s.sessions[sessionID]
		s.sessionsMu.RUnlock()
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			rpcErr := jsonrpc.ErrInvalidRequest.WithData(map[string]any{
				"detail": "Session not found",
			})
			_ = json.NewEncoder(w).Encode(rpcErr)
			return
		}
	}

	stream := session.addStream()

	// We're *not* deriving from the request context here, because the request
	// might timeout or be canceled, and we want to keep handling any in-flight
	// messages.
	handleCtx := context.WithValue(context.Background(), sessionKey, session)
	conn := jsonrpc.NewConn(jsonrpc.ConnConfig{
		ReadWriter:        stream.newClientLocalConn(),
		Handler:           s,
		HandleContext:     handleCtx,
		NewRequestID:      session.newRequestID,
		OnResponse:        session.handleResponse,
		AddPendingRequest: session.addPendingRequest,
	})
	go conn.Listen(ctx)

	err := stream.handleHTTPRequest(ctx, w, req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		rpcErr := jsonrpc.ErrInvalidRequest.WithData(map[string]any{
			"detail": err.Error(),
		})
		_ = json.NewEncoder(w).Encode(rpcErr)
		return
	}
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, req *http.Request) {
	sessionID := req.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "Missing session ID", http.StatusBadRequest)
		return
	}

	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	for _, stream := range session.streams {
		stream.close()
	}

	delete(s.sessions, sessionID)

	w.WriteHeader(http.StatusNoContent)
}

func writeServerSentEvent(w io.Writer, event, id string, data any) {
	fmt.Fprintf(w, "event: %s\n", event)
	if id != "" {
		fmt.Fprintf(w, "id: %s\n", id)
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func (s *Server) handleJSONRPCRequest(w http.ResponseWriter, req *http.Request) {
	sessionID := req.URL.Query().Get("sessionId")
	if sessionID == "" {
		http.Error(w, "Missing session ID", http.StatusBadRequest)
		return
	}

	s.sessionsMu.RLock()
	session, ok := s.sessions[sessionID]
	s.sessionsMu.RUnlock()
	if !ok {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	if session.conn == nil {
		http.Error(w, "Session not ready", http.StatusServiceUnavailable)
		return
	}

	// Write the request body to the session's [io.Writer], which will piped
	// as an incoming JSON-RPC request for the session.
	_, err := io.Copy(session.sseWriter, req.Body)
	if err != nil {
		if err != io.EOF {
			log.Printf("Failed to pipe HTTP request body to the session's : %v", err)
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) Handle(ctx context.Context, req *jsonrpc.Request) (any, error) {
	switch req.Method {
	case "initialize":
		return handleCall(ctx, req, s.handleInitializeRequest)
	case "notifications/initialized":
		return nil, handleNotification(ctx, req, s.handleInitializedNotification)
	case "notifications/roots/list_changed":
		return nil, handleNotification(ctx, req, s.handleRootsListChangedNotification)
	case "resources/list":
		return handleCall(ctx, req, s.handleListResourcesRequest)
	case "resources/read":
		return handleCall(ctx, req, s.handleReadResourceRequest)
	case "resources/templates/list":
		return handleCall(ctx, req, s.handleListResourceTemplatesRequest)
	case "resources/subscribe":
		return handleCall(ctx, req, s.handleSubscribeResourceRequest)
	case "tools/list":
		return handleCall(ctx, req, s.handleListToolsRequest)
	case "tools/call":
		return handleCall(ctx, req, s.handleCallToolRequest)
	case "prompts/list":
		return handleCall(ctx, req, s.handleListPromptsRequest)
	case "prompts/get":
		return handleCall(ctx, req, s.handleGetPromptRequest)
	case "completion/complete":
		return handleCall(ctx, req, s.handleCompleteRequest)
	case "ping":
		return s.handlePingRequest(ctx)
	default:
		return nil, jsonrpc.ErrMethodNotFound
	}
}

func handleCall[T any, U any](ctx context.Context, req *jsonrpc.Request, handleFn func(context.Context, T) (*U, error)) (*U, error) {
	if !req.IsCall() {
		return nil, jsonrpc.ErrMissingRequestID
	}

	params, err := decodeParams[T](req.Params)
	if err != nil {
		return nil, err
	}

	return handleFn(ctx, params)
}

func handleNotification(ctx context.Context, req *jsonrpc.Request, handleFn func(context.Context) error) error {
	if req.IsCall() {
		return jsonrpc.ErrNotNotification
	}

	return handleFn(ctx)
}

func (s *Server) handleListToolsRequest(ctx context.Context, params ListToolsParams) (*ListToolsResult, error) {
	s.toolsMu.RLock()
	defer s.toolsMu.RUnlock()

	tools := make([]Tool, 0, len(s.tools))
	for _, tool := range s.tools {
		tools = append(tools, tool)
	}

	return &ListToolsResult{
		Tools: tools,
	}, nil
}

func (s *Server) handleCallToolRequest(ctx context.Context, params CallToolParams) (*CallToolResult, error) {
	s.toolsMu.RLock()

	tool, ok := s.tools[params.Name]
	if !ok {
		s.toolsMu.RUnlock()
		return nil, jsonrpc.ErrInvalidParams.WithData(map[string]any{
			"detail": "Tool not found",
			"name":   params.Name,
		})
	}
	s.toolsMu.RUnlock()

	return tool.HandleFunc(ctx, params.Arguments)
}

func (s *Server) handleInitializeRequest(ctx context.Context, params InitializeParams) (*InitializeResult, error) {
	if err := params.Validate(); err != nil {
		return nil, jsonrpc.ErrInvalidParams.WithData(map[string]any{
			"detail": err.Error(),
		})
	}

	session := sessionFromContext(ctx)

	if session.initialized {
		return nil, jsonrpc.ErrAlreadyInitialized
	}

	if params.ProtocolVersion != ProtocolVersion {
		return nil, jsonrpc.ErrInvalidParams.WithData(map[string]any{
			"detail":    "Unsupported protocol version.",
			"supported": []string{ProtocolVersion},
			"requested": params.ProtocolVersion,
		})
	}

	session.clientCapabilities = params.Capabilities

	serverCapabilities := ServerCapabilities{}
	if s.listResourcesFn != nil {
		serverCapabilities.Resources = &ResourcesCapability{
			Subscribe:   s.onSubscribeResourceFn != nil,
			ListChanged: true, // Should be configurable?
		}
	}
	if s.listPromptsFn != nil {
		serverCapabilities.Prompts = &PromptsCapability{
			ListChanged: true, // Should be configurable?
		}
	}
	if len(s.tools) > 0 {
		serverCapabilities.Tools = &ToolsCapability{
			ListChanged: true, // Should be configurable?
		}
	}
	if s.completeFn != nil {
		serverCapabilities.Completions = &CompletionsCapability{}
	}

	session.serverCapabilities = serverCapabilities

	result := &InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    serverCapabilities,
		ServerInfo: Implementation{
			Name:    s.name,
			Version: s.version,
		},
	}

	return result, nil
}

func (s *Server) handleInitializedNotification(ctx context.Context) error {
	session := sessionFromContext(ctx)

	if session.initialized {
		return jsonrpc.ErrAlreadyInitialized
	}

	session.initialized = true

	if s.onClientInitializedFn != nil {
		s.onClientInitializedFn(ctx, session)
	}

	return nil
}

func (s *Server) handleRootsListChangedNotification(ctx context.Context) error {
	session := sessionFromContext(ctx)

	if !session.initialized {
		return jsonrpc.ErrNotInitialized
	}

	if s.onRootsListChangedFn != nil {
		s.onRootsListChangedFn(ctx, session)
	}

	return nil
}

func (s *Server) handleListResourcesRequest(ctx context.Context, req ListResourcesParams) (*ListResourcesResult, error) {
	if s.listResourcesFn == nil {
		return nil, fmt.Errorf("list resources not supported")
	}

	result, err := s.listResourcesFn(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("list resources failed: %w", err)
	}

	return result, nil
}

func (s *Server) handleListResourceTemplatesRequest(ctx context.Context, req ListResourceTemplatesParams) (*ListResourceTemplatesResult, error) {
	if s.listResourceTemplatesFn == nil {
		return nil, fmt.Errorf("list resource templates not supported")
	}

	result, err := s.listResourceTemplatesFn(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("list resource templates failed: %w", err)
	}

	return result, nil
}

func (s *Server) handleSubscribeResourceRequest(ctx context.Context, params ResourceSubscribeParams) (*EmptyResult, error) {
	session := sessionFromContext(ctx)

	if s.onSubscribeResourceFn == nil {
		return nil, jsonrpc.ErrFeatureNotSupported.WithData(map[string]string{
			"feature": "resources/subscribe",
		})
	}

	err := s.onSubscribeResourceFn(ctx, session, params)
	if err != nil {
		return nil, fmt.Errorf("subscribe resource failed: %w", err)
	}

	return &EmptyResult{}, nil
}

func (s *Server) handleReadResourceRequest(ctx context.Context, req ReadResourceParams) (*ReadResourceResult, error) {
	if s.readResourceFn == nil {
		return nil, fmt.Errorf("read resource not supported")
	}

	if err := readResourceParamsValSchema.Validate(req); err != nil {
		return nil, jsonrpc.ErrInvalidParams.WithData(map[string]string{
			"detail": err.Error(),
		})
	}
	result, err := s.readResourceFn(ctx, req)
	if err != nil {
		log.Printf("Read resource failed: %v", err)
		return nil, jsonrpc.ErrInternal
	}

	return result, nil
}

func (s *Server) handleListPromptsRequest(ctx context.Context, req ListPromptsParams) (*ListPromptsResult, error) {
	if s.listPromptsFn == nil {
		return nil, fmt.Errorf("list prompts not supported")
	}

	result, err := s.listPromptsFn(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("list prompts failed: %w", err)
	}

	return result, nil
}

func (s *Server) handleGetPromptRequest(ctx context.Context, req GetPromptParams) (*GetPromptResult, error) {
	if s.getPromptFn == nil {
		return nil, fmt.Errorf("get prompt not supported")
	}

	if err := getPromptParamsValSchema.Validate(req); err != nil {
		return nil, jsonrpc.ErrInvalidParams.WithData(map[string]string{
			"detail": err.Error(),
		})
	}

	result, err := s.getPromptFn(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("get prompt failed: %w", err)
	}

	return result, nil
}

func (s *Server) handleCompleteRequest(ctx context.Context, params CompleteParams) (*CompleteResult, error) {
	if s.completeFn == nil {
		return nil, jsonrpc.ErrFeatureNotSupported.WithData(map[string]string{
			"feature": "completion/complete",
		})
	}

	if err := params.Validate(); err != nil {
		return nil, jsonrpc.ErrInvalidParams.WithData(map[string]any{
			"detail": err.Error(),
		})
	}

	return s.completeFn(ctx, params)
}

func (s *Server) handlePingRequest(_ context.Context) (*struct{}, error) {
	return &struct{}{}, nil
}

func decodeParams[T any](data []byte) (v T, err error) {
	if len(data) == 0 {
		return v, nil
	}
	if err = json.Unmarshal(data, &v); err != nil {
		return v, jsonrpc.ErrInvalidParams.WithData(map[string]string{
			"detail": err.Error(),
		})
	}
	return v, nil
}

func (s *Server) notifyClients(ctx context.Context, method string, params any) {
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()

	for _, session := range s.sessions {
		go func() {
			if err := session.conn.Notify(ctx, method, params); err != nil {
				log.Printf("Failed to notify client: %v", err)
			}
		}()
	}
}

// CreateTool creates a new [Tool] with type-safe parameters from a [ToolDef].
func CreateTool[P any](toolDef ToolDef[P]) Tool {
	var p P
	schema := jsonschemaReflector.Reflect(p)

	return Tool{
		Name:        toolDef.Name,
		Description: toolDef.Description,
		InputSchema: schema,
		HandleFunc:  ToolHandler(*schema, toolDef.HandleFunc),
	}
}

func ToolHandler[P any](schema jsonschema.Schema, handleFunc ToolHandleFunc[P]) func(ctx context.Context, rawArgs json.RawMessage) (*CallToolResult, error) {
	// Validate the raw arguments against the tool's input schema.
	valSchema, err := valtorjsonschema.ParseJSONSchema[any](schema)
	if err != nil {
		// Parsing a JSON schema to a validation schema is generally error free
		// but it's possible that the schema contains invalid JSON number values.
		// TODO: Avoid panic.
		panic(err)
	}
	return func(ctx context.Context, rawArgs json.RawMessage) (*CallToolResult, error) {
		var args P
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return nil, jsonrpc.ErrInvalidParams.WithData(map[string]any{
				"detail": "Failed to JSON decode tool arguments",
				"error":  err.Error(),
			})
		}

		if valSchema != nil {
			valArgs := map[string]any{}
			err := json.Unmarshal(rawArgs, &valArgs)
			if err != nil {
				return nil, jsonrpc.ErrInvalidParams.WithData(map[string]any{
					"detail": "Failed to JSON decode tool arguments for validation",
					"error":  err.Error(),
				})
			}
			if err := valSchema.Validate(valArgs); err != nil {
				return nil, jsonrpc.ErrInvalidParams.WithData(map[string]any{
					"detail": "Invalid tool arguments",
					"error":  err.Error(),
				})
			}
		}

		return handleFunc(ctx, args), nil
	}
}

func generateSessionID() string {
	bytes := make([]byte, 16)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func sessionFromContext(ctx context.Context) *Session {
	return ctx.Value(sessionKey).(*Session)
}
