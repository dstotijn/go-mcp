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
	"errors"
	"fmt"
	"io"
	"os/exec"

	"github.com/dstotijn/go-mcp/internal/jsonrpc"
)

// ErrTransportNotSet is returned when a client operation is attempted without configuring a transport.
var ErrTransportNotSet = errors.New("transport not set")

// Client represents an MCP client.
type Client struct {
	session                        *Session
	rwc                            io.ReadWriteCloser
	connectFn                      func(ctx context.Context, c *Client) error
	onCreateSamplingMessageRequest func(ctx context.Context, params CreateSamplingMessageParams) (*CreateSamplingMessageResult, error)
}

// ClientConfig contains configuration options for creating a new MCP client.
type ClientConfig struct {
	OnCreateSamplingMessageRequest func(ctx context.Context, params CreateSamplingMessageParams) (*CreateSamplingMessageResult, error)
}

// ClientOption represents a function that modifies a Client.
type ClientOption func(*Client)

// StdioClientTransportConfig contains configuration for a stdio-based client transport.
type StdioClientTransportConfig struct {
	Command string
	Args    []string
}

// NewClient creates a new MCP client with the provided configuration and options.
func NewClient(cfg ClientConfig, opts ...ClientOption) *Client {
	c := &Client{
		onCreateSamplingMessageRequest: cfg.OnCreateSamplingMessageRequest,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// WithStdioClientTransport returns a ClientOption that configures a stdio-based transport.
func WithStdioClientTransport(config StdioClientTransportConfig) ClientOption {
	return func(c *Client) {
		c.connectFn = func(ctx context.Context, c *Client) error {
			cmd := exec.Command(config.Command, config.Args...)

			stdin, err := cmd.StdinPipe()
			if err != nil {
				return fmt.Errorf("failed to get stdin pipe: %w", err)
			}

			stdout, err := cmd.StdoutPipe()
			if err != nil {
				return fmt.Errorf("failed to get stdout pipe: %w", err)
			}

			c.rwc = &cmdReadWriter{
				cmd:    cmd,
				stdin:  stdin,
				stdout: stdout,
				ctx:    ctx,
			}

			if err := cmd.Start(); err != nil {
				return fmt.Errorf("mcp: failed to start command: %w", err)
			}

			return nil
		}
	}
}

// Connect establishes a connection to the MCP server using the transport that
// was configured when creating the client.
func (c *Client) Connect(ctx context.Context) error {
	if c.connectFn == nil {
		return ErrTransportNotSet
	}

	if err := c.connectFn(ctx, c); err != nil {
		return err
	}

	conn := jsonrpc.NewConn(c.rwc, c)
	c.session = &Session{
		conn: conn,
	}

	go conn.Listen(ctx)

	return nil
}

// Disconnect closes the connection to the MCP server.
func (c *Client) Disconnect() error {
	if c.rwc == nil {
		return ErrTransportNotSet
	}

	err := c.rwc.Close()
	if err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("mcp: failed to disconnect: %w", err)
	}

	return nil
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.session.conn.Call(ctx, method, params)
}

// Handle implements [jsonrpc.Handler].
func (c *Client) Handle(ctx context.Context, req *jsonrpc.Request) (any, error) {
	switch req.Method {
	case "sampling/createMessage":
		return handleCall(ctx, req, c.handleCreateSamplingMessageRequest)
	default:
		return nil, jsonrpc.ErrMethodNotFound
	}
}

func (c *Client) handleCreateSamplingMessageRequest(ctx context.Context, params CreateSamplingMessageParams) (*CreateSamplingMessageResult, error) {
	if c.onCreateSamplingMessageRequest != nil {
		return c.onCreateSamplingMessageRequest(ctx, params)
	}

	return nil, jsonrpc.ErrInvalidParams.WithData(map[string]any{
		"details": "createSamplingMessage is not supported",
	})
}

// Initialize sends an initialize request to the server.
func (c *Client) Initialize(ctx context.Context, params InitializeParams) (*InitializeResult, error) {
	if c.onCreateSamplingMessageRequest != nil {
		params.Capabilities.Sampling = map[string]any{}
	}
	result, err := convertResult[InitializeResult](c.call(ctx, "initialize", params))
	if err != nil {
		return nil, fmt.Errorf("mcp: failed to initialize: %w", err)
	}

	return result, nil
}

// NotifyInitialized sends an initialized notification to the server.
func (c *Client) NotifyInitialized(ctx context.Context) error {
	_, err := c.call(ctx, "notifications/initialized", nil)
	if err != nil {
		return fmt.Errorf("mcp: failed to send initialized notification: %w", err)
	}
	return nil
}

// NotifyRootsListChanged sends a roots list changed notification to the server.
func (c *Client) NotifyRootsListChanged(ctx context.Context) error {
	_, err := c.call(ctx, "notifications/roots/list_changed", nil)
	if err != nil {
		return fmt.Errorf("mcp: failed to send roots list changed notification: %w", err)
	}
	return nil
}

// ListResources requests a list of resources from the server.
func (c *Client) ListResources(ctx context.Context, params ListResourcesParams) (*ListResourcesResult, error) {
	result, err := convertResult[ListResourcesResult](c.call(ctx, "resources/list", params))
	if err != nil {
		return nil, fmt.Errorf("mcp: failed to list resources: %w", err)
	}

	return result, nil
}

// ReadResource requests to read a specific resource from the server.
func (c *Client) ReadResource(ctx context.Context, params ReadResourceParams) (*ReadResourceResult, error) {
	result, err := convertResult[ReadResourceResult](c.call(ctx, "resources/read", params))
	if err != nil {
		return nil, fmt.Errorf("mcp: failed to read resource: %w", err)
	}

	return result, nil
}

// ListResourceTemplates requests a list of resource templates from the server.
func (c *Client) ListResourceTemplates(ctx context.Context, params ListResourceTemplatesParams) (*ListResourceTemplatesResult, error) {
	result, err := convertResult[ListResourceTemplatesResult](c.call(ctx, "resources/templates/list", params))
	if err != nil {
		return nil, fmt.Errorf("mcp: failed to list resource templates: %w", err)
	}

	return result, nil
}

// SubscribeResource subscribes to changes for a specific resource.
func (c *Client) SubscribeResource(ctx context.Context, params ResourceSubscribeParams) error {
	_, err := c.call(ctx, "resources/subscribe", params)
	if err != nil {
		return fmt.Errorf("mcp: failed to subscribe to resource: %w", err)
	}

	return nil
}

// ListTools requests a list of available tools from the server.
func (c *Client) ListTools(ctx context.Context, params ListToolsParams) (*ListToolsResult, error) {
	result, err := convertResult[ListToolsResult](c.call(ctx, "tools/list", params))
	if err != nil {
		return nil, fmt.Errorf("mcp: failed to list tools: %w", err)
	}

	return result, nil
}

// CallTool calls a specific tool on the server.
func (c *Client) CallTool(ctx context.Context, name string, args any) (*CallToolResult, error) {
	jsonArgs, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("mcp: failed to marshal tool arguments: %w", err)
	}

	result, err := convertResult[CallToolResult](c.call(ctx, "tools/call", CallToolParams{
		Name:      name,
		Arguments: jsonArgs,
	}))
	if err != nil {
		return nil, fmt.Errorf("mcp: failed to call tool: %w", err)
	}

	return result, nil
}

// ListPrompts requests a list of available prompts from the server.
func (c *Client) ListPrompts(ctx context.Context, params ListPromptsParams) (*ListPromptsResult, error) {
	result, err := convertResult[ListPromptsResult](c.call(ctx, "prompts/list", params))
	if err != nil {
		return nil, fmt.Errorf("mcp: failed to list prompts: %w", err)
	}

	return result, nil
}

// GetPrompt requests a specific prompt from the server.
func (c *Client) GetPrompt(ctx context.Context, params GetPromptParams) (*GetPromptResult, error) {
	result, err := convertResult[GetPromptResult](c.call(ctx, "prompts/get", params))
	if err != nil {
		return nil, fmt.Errorf("mcp: failed to get prompt: %w", err)
	}

	return result, nil
}

// Ping sends a ping request to the server.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.call(ctx, "ping", nil)
	if err != nil {
		return fmt.Errorf("mcp: failed to ping server: %w", err)
	}
	return nil
}

func convertResult[T any](rawResult json.RawMessage, err error) (*T, error) {
	var result T

	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(rawResult, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	return &result, nil
}
