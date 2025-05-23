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

	"github.com/dstotijn/go-mcp/internal/jsonrpc"
)

var (
	// ErrSamplingNotSupported is returned when sampling is not supported by the client.
	ErrSamplingNotSupported = errors.New("sampling not supported")
	// ErrRootsNotSupported is returned when roots are not supported by the client.
	ErrRootsNotSupported = errors.New("roots not supported")
)

// Session represents an identifiable connection between an MCP server and an MCP client.
// The underlying connection uses JSON-RPC. It's transport agnostic.
type Session struct {
	sseWriter          io.Writer
	conn               *jsonrpc.Conn
	sessionID          string
	initialized        bool
	clientCapabilities ClientCapabilities
	serverCapabilities ServerCapabilities
}

// ID returns the session identifier.
func (s *Session) ID() string {
	return s.sessionID
}

// ClientCapabilities returns the client capabilities for this session.
func (s *Session) ClientCapabilities() ClientCapabilities {
	return s.clientCapabilities
}

// ServerCapabilities returns the server capabilities for this session.
func (s *Session) ServerCapabilities() ServerCapabilities {
	return s.serverCapabilities
}

// ListRoots lists the roots of an MCP client.
func (s *Session) ListRoots(ctx context.Context, params *ListRootsParams) (*ListRootsResult, error) {
	if s.clientCapabilities.Roots == nil {
		return nil, ErrRootsNotSupported
	}

	resp, err := s.conn.Call(ctx, "roots/list", params)
	if err != nil {
		return nil, fmt.Errorf("mcp: failed to call list roots: %w", err)
	}

	result := &ListRootsResult{}
	if err := json.Unmarshal(resp, result); err != nil {
		return nil, fmt.Errorf("mcp: failed to unmarshal list roots result: %w", err)
	}

	return result, nil
}

// CreateSamplingMessage calls an MCP client to create a message.
func (s *Session) CreateSamplingMessage(ctx context.Context, params *CreateSamplingMessageParams) (*CreateSamplingMessageResult, error) {
	if s.clientCapabilities.Sampling == nil {
		return nil, ErrSamplingNotSupported
	}

	resp, err := s.conn.Call(ctx, "sampling/createMessage", params)
	if err != nil {
		return nil, fmt.Errorf("mcp: failed to call create message: %w", err)
	}

	result := &CreateSamplingMessageResult{}
	if err := json.Unmarshal(resp, result); err != nil {
		return nil, fmt.Errorf("mcp: failed to unmarshal create message result: %w", err)
	}

	return result, nil
}

// NotifyResourceUpdated sends a notification to a client that a resource has been updated.
func (s *Session) NotifyResourceUpdated(ctx context.Context, params ResourceUpdatedNotificationParams) error {
	return s.conn.Notify(ctx, "notifications/resources/updated", params)
}
