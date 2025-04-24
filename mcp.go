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
	"encoding/json"

	"github.com/dstotijn/go-mcp/internal/jsonrpc"
	"github.com/invopop/jsonschema"
)

// Role represents the sender or recipient of messages and data in a conversation.
type Role string

const (
	// RoleAssistant represents the AI assistant in a conversation.
	RoleAssistant Role = "assistant"
	// RoleUser represents the human user in a conversation.
	RoleUser Role = "user"
)

// Implementation describes the name and version of an MCP implementation.
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ClientCapabilities represents the capabilities that a client supports.
type ClientCapabilities struct {
	Experimental map[string]any   `json:"experimental,omitempty"`
	Roots        *RootsCapability `json:"roots,omitempty"`
	Sampling     map[string]any   `json:"sampling,omitempty"`
}

// RootsCapability represents capabilities related to root operations.
type RootsCapability struct {
	ListChanged bool `json:"listChanged"`
}

// ServerCapabilities represents the capabilities that a server supports.
type ServerCapabilities struct {
	Experimental map[string]any       `json:"experimental,omitempty"`
	Logging      map[string]any       `json:"logging,omitempty"`
	Prompts      *PromptsCapability   `json:"prompts,omitempty"`
	Resources    *ResourcesCapability `json:"resources,omitempty"`
	Tools        *ToolsCapability     `json:"tools,omitempty"`
}

// ModelPreferences represents the server's preferences for model selection.
type ModelPreferences struct {
	CostPriority         json.Number `json:"costPriority,omitempty"`
	IntelligencePriority json.Number `json:"intelligencePriority,omitempty"`
	SpeedPriority        json.Number `json:"speedPriority,omitempty"`
	Hints                []ModelHint `json:"hints,omitempty"`
}

// ModelHint provides hints for model selection.
type ModelHint struct {
	Name string `json:"name,omitempty"`
}

// Resource represents a known resource that the server can read.
type Resource struct {
	Name        string       `json:"name"`
	URI         string       `json:"uri"`
	Size        int          `json:"size,omitempty"`
	Description string       `json:"description,omitempty"`
	MimeType    string       `json:"mimeType,omitempty"`
	Annotations *Annotations `json:"annotations,omitempty"`
}

// Annotations provides metadata about how objects should be used or displayed.
type Annotations struct {
	Audience []Role      `json:"audience,omitempty"`
	Priority json.Number `json:"priority,omitempty"`
}

// TextContent represents text content in messages.
type TextContent struct {
	Text        string       `json:"text"`
	Annotations *Annotations `json:"annotations,omitempty"`
}

func (tc TextContent) MarshalJSON() ([]byte, error) {
	type alias TextContent
	return json.Marshal(struct {
		Type string `json:"type"`
		alias
	}{
		Type:  "text",
		alias: alias(tc),
	})
}

// ImageContent represents image content in messages.
type ImageContent struct {
	Data        []byte       `json:"data"`
	MimeType    string       `json:"mimeType"`
	Annotations *Annotations `json:"annotations,omitempty"`
}

func (ic ImageContent) MarshalJSON() ([]byte, error) {
	type alias ImageContent
	return json.Marshal(struct {
		Type string `json:"type"`
		alias
	}{
		Type:  "image",
		alias: alias(ic),
	})
}

// Root represents a root directory or file that the server can operate on.
type Root struct {
	Name string `json:"name,omitempty"`
	URI  string `json:"uri"`
}

// ProgressToken is used to associate progress notifications with the original request.
type ProgressToken json.RawMessage // Can be string or integer

// InitializeParams represents the parameters for an initialize request.
type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      Implementation     `json:"clientInfo"`
}

// InitializeResult represents the server's response to an initialize request.
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      Implementation     `json:"serverInfo"`
	Instructions    string             `json:"instructions,omitempty"`
	Meta            map[string]any     `json:"_meta,omitempty"`
}

// LoggingLevel represents the severity of a log message.
type LoggingLevel string

const (
	LoggingLevelEmergency LoggingLevel = "emergency"
	LoggingLevelAlert     LoggingLevel = "alert"
	LoggingLevelCritical  LoggingLevel = "critical"
	LoggingLevelError     LoggingLevel = "error"
	LoggingLevelWarning   LoggingLevel = "warning"
	LoggingLevelNotice    LoggingLevel = "notice"
	LoggingLevelInfo      LoggingLevel = "info"
	LoggingLevelDebug     LoggingLevel = "debug"
)

// Tool defines a tool that the client can call.
type Tool struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	InputSchema *jsonschema.Schema `json:"inputSchema"`

	// HandleFunc is used for MCP servers to handle an incoming tool call.
	HandleFunc func(ctx context.Context, args json.RawMessage) (*CallToolResult, error) `json:"-"`
}

// SamplingMessage represents a message in an LLM conversation.
type SamplingMessage struct {
	Content Content `json:"content"`
	Role    Role    `json:"role"`
}

// EmbeddedResource represents a resource embedded into a prompt or tool call result.
type EmbeddedResource struct {
	Resource    ResourceContents `json:"resource"`
	Annotations *Annotations     `json:"annotations,omitempty"`
}

func (e EmbeddedResource) MarshalJSON() ([]byte, error) {
	type alias EmbeddedResource
	return json.Marshal(struct {
		Type string `json:"type"`
		alias
	}{
		Type:  "resource",
		alias: alias(e),
	})
}

type ResourceContents struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
}

// ResourceContents represents either text or blob resource content.
type TextResourceContents struct {
	Text string `json:"text,omitempty"`
	ResourceContents
}

func (t TextResourceContents) MarshalJSON() ([]byte, error) {
	type alias TextResourceContents
	return json.Marshal(struct {
		Type string `json:"type"`
		alias
	}{
		Type:  "text",
		alias: alias(t),
	})
}

type BlobResourceContents struct {
	Blob string `json:"blob,omitempty"`
	ResourceContents
}

func (b BlobResourceContents) MarshalJSON() ([]byte, error) {
	type alias BlobResourceContents
	return json.Marshal(struct {
		Type string `json:"type"`
		alias
	}{
		Type:  "blob",
		alias: alias(b),
	})
}

// CallToolResult represents the server's response to a tool call.
type CallToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError"`
}

// CompleteResult represents the server's response to a completion request.
type CompleteResult struct {
	Result
	Completion CompletionResult `json:"completion"`
}

// CompletionResult contains completion values and metadata.
type CompletionResult struct {
	HasMore bool     `json:"hasMore,omitempty"`
	Total   int      `json:"total,omitempty"`
	Values  []string `json:"values"`
}

// CreateSamplingMessageResult represents the client's response to a sampling request.
type CreateSamplingMessageResult struct {
	Role       Role    `json:"role"`
	Content    Content `json:"content"`
	Model      string  `json:"model"`
	StopReason string  `json:"stopReason,omitempty"`
}

// GetPromptResult represents the server's response to a prompt request.
type GetPromptResult struct {
	Messages    []PromptMessage `json:"messages"`
	Description string          `json:"description,omitempty"`
}

// PromptMessage represents a message in a prompt template.
type PromptMessage struct {
	Content Content `json:"content"`
	Role    Role    `json:"role"`
}

// ListToolsResult represents the server's response to a tools list request.
type ListToolsResult struct {
	Tools      []Tool `json:"tools"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// ListRootsResult represents the client's response to a roots list request.
type ListRootsResult struct {
	Roots []Root         `json:"roots"`
	Meta  map[string]any `json:"_meta,omitempty"`
}

// Content represents either text or image content.
// TODO: Fix
type Content any

// Prompt represents a prompt or prompt template that the server offers.
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument describes an argument that a prompt can accept.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// ResourceReference represents a reference to a resource.
type ResourceReference struct {
	URI string `json:"uri"`
}

// ResourceTemplate represents a template description for resources available on the server.
type ResourceTemplate struct {
	Name        string       `json:"name"`
	URITemplate string       `json:"uriTemplate"`
	Description string       `json:"description,omitempty"`
	MimeType    string       `json:"mimeType,omitempty"`
	Annotations *Annotations `json:"annotations,omitempty"`
}

// ListResourceTemplatesResult represents the server's response to a resource templates list request.
type ListResourceTemplatesResult struct {
	ResourceTemplates []ResourceTemplate `json:"resourceTemplates"`
	NextCursor        string             `json:"nextCursor,omitempty"`
	Meta              map[string]any     `json:"_meta,omitempty"`
}

// ListResourcesResult represents the server's response to a resources list request.
type ListResourcesResult struct {
	Resources  []Resource     `json:"resources"`
	NextCursor string         `json:"nextCursor,omitempty"`
	Meta       map[string]any `json:"_meta,omitempty"`
}

// ListPromptsResult represents the server's response to a prompts list request.
type ListPromptsResult struct {
	Result
	Prompts    []Prompt `json:"prompts"`
	NextCursor string   `json:"nextCursor,omitempty"`
}

// ReadResourceResult represents the server's response to a resource read request.
type ReadResourceResult struct {
	Contents []Content      `json:"contents"`
	Meta     map[string]any `json:"_meta,omitempty"`
}

// Result represents a basic result with only metadata.
type Result struct{}

// CancelledNotificationParams represents the parameters for a cancelled notification.
type CancelledNotificationParams struct {
	RequestID RequestID `json:"requestId"`
	Reason    string    `json:"reason,omitempty"`
}

// ProgressNotificationParams represents the parameters for a progress notification.
type ProgressNotificationParams struct {
	Token      ProgressToken `json:"token"`
	Message    string        `json:"message,omitempty"`
	Percentage json.Number   `json:"percentage,omitempty"`
	RequestID  RequestID     `json:"requestId"`
}

// ResourceUpdatedNotificationParams represents the parameters for a resource updated notification.
type ResourceUpdatedNotificationParams struct {
	URI string `json:"uri"`
}

// LoggingMessageNotificationParams represents the parameters for a logging message notification.
type LoggingMessageNotificationParams struct {
	Level   LoggingLevel `json:"level"`
	Message string       `json:"message"`
	Data    any          `json:"data,omitempty"`
}

// NotificationParams represents the generic parameters structure used by several notifications.
type NotificationParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
}

// RequestID uniquely identifies a request in JSON-RPC.
type RequestID jsonrpc.ID

// PingParams represents parameters for a ping request.
type PingParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
}

// ListResourcesParams represents parameters for a list resources request.
type ListResourcesParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// ListResourceTemplatesParams represents parameters for a list resource templates request.
type ListResourceTemplatesParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// ReadResourceParams represents parameters for a read resource request.
type ReadResourceParams struct {
	URI string `json:"uri"`
}

// ResourceSubscribeParams represents parameters for a resource subscription request.
type ResourceSubscribeParams struct {
	URI string `json:"uri"`
}

// ResourceUnsubscribeParams represents parameters for a resource unsubscribe request.
type ResourceUnsubscribeParams struct {
	URI string `json:"uri"`
}

// ListPromptsParams represents parameters for a list prompts request.
type ListPromptsParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// GetPromptParams represents parameters for a get prompt request.
type GetPromptParams struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

// ListToolsParams represents parameters for a list tools request.
type ListToolsParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// CallToolParams represents parameters for a tool call request.
type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// SetLevelParams represents parameters for a set logging level request.
type SetLevelParams struct {
	Level LoggingLevel `json:"level"`
}

// CompleteParams represents parameters for a completion request.
type CompleteParams struct {
	Argument CompleteArgument  `json:"argument"`
	Ref      CompleteReference `json:"ref"`
}

// CompleteArgument represents an argument for completion.
type CompleteArgument struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// CompleteReference represents a reference for completion.
// TODO: Fix
type CompleteReference struct{}

// IncludeContext represents which servers' context to include in a message.
type IncludeContext string

const (
	IncludeContextAllServers IncludeContext = "allServers"
	IncludeContextNone       IncludeContext = "none"
	IncludeContextThisServer IncludeContext = "thisServer"
)

// CreateSamplingMessageParams represents parameters for a create sampling message request.
type CreateSamplingMessageParams struct {
	MaxTokens        int               `json:"maxTokens"`
	Messages         []SamplingMessage `json:"messages"`
	IncludeContext   IncludeContext    `json:"includeContext,omitempty"`
	Metadata         map[string]any    `json:"metadata,omitempty"`
	ModelPreferences *ModelPreferences `json:"modelPreferences,omitempty"`
	StopSequences    []string          `json:"stopSequences,omitempty"`
	SystemPrompt     string            `json:"systemPrompt,omitempty"`
	Temperature      json.Number       `json:"temperature,omitempty"`
}

// ListRootsParams represents parameters for a list roots request.
type ListRootsParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
}

// EmbeddedResourceContents represents the contents of an embedded resource.
type EmbeddedResourceContents struct {
	Resource ResourceContents `json:"resource"`
}

func (e EmbeddedResourceContents) MarshalJSON() ([]byte, error) {
	type alias EmbeddedResourceContents
	return json.Marshal(struct {
		Type string `json:"type"`
		alias
	}{
		Type:  "resource",
		alias: alias(e),
	})
}

// LoggingCapability represents logging-related server capabilities.
type LoggingCapability struct {
	Enabled bool `json:"enabled"`
}

// PromptsCapability represents prompt-related server capabilities.
type PromptsCapability struct {
	ListChanged bool `json:"listChanged"`
}

// ResourcesCapability represents resource-related server capabilities.
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe"`
	ListChanged bool `json:"listChanged"`
}

// ToolsCapability represents tool-related server capabilities.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged"`
}

type RootsListChangedNotificationParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
}

// ToolDef represents a tool definition, used for registering tools with the server.
type ToolDef[P any] struct {
	Name        string
	Description string
	HandleFunc  ToolHandleFunc[P]
}

// ToolHandleFunc is a function that handles a tool call with type-safe parameters.
type ToolHandleFunc[P any] func(ctx context.Context, params P) *CallToolResult

type ToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// EmptyResult represents a result with no content.
type EmptyResult struct{}
