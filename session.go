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
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"sync/atomic"

	"github.com/dstotijn/go-mcp/internal/jsonrpc"
)

var (
	// ErrSamplingNotSupported is returned when sampling is not supported by the client.
	ErrSamplingNotSupported = errors.New("sampling not supported")
	// ErrRootsNotSupported is returned when roots are not supported by the client.
	ErrRootsNotSupported = errors.New("roots not supported")
	// ErrListenerConn is returned when a client is connected via a
	// transport that has no standalone connection initialized. Typically, this
	// happens when using Streamable HTTP and the client has not started listing
	// for messages via SSE.
	ErrListenerConn = errors.New("no listener connection")
)

// Session represents identifiable interactions between an MCP server and an MCP
// client, which may be transported over one or more underlying connections.
type Session struct {
	// Writer for the "standalone" SSE stream (e.g. the deprecated "sse" transport).
	sseWriter io.Writer

	streams   map[string]*stream
	streamsMu sync.RWMutex

	// ID counter for creating unique outgoing JSON-RPC request IDs.
	idCounter int64

	pending   map[string]chan jsonrpc.Response
	pendingMu sync.Mutex

	id                 string
	initialized        bool
	clientCapabilities ClientCapabilities
	serverCapabilities ServerCapabilities

	conn *jsonrpc.Conn
}

func newSession(id string, conn *jsonrpc.Conn) *Session {
	return &Session{
		id:      cmp.Or(id, generateSessionID()),
		pending: make(map[string]chan jsonrpc.Response),
		streams: make(map[string]*stream),
		conn:    conn,
	}
}

// ID returns the session identifier.
func (s *Session) ID() string {
	return s.id
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

	if s.conn == nil {
		return nil, ErrListenerConn
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

	if s.conn == nil {
		return nil, ErrListenerConn
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
	if s.conn == nil {
		return ErrListenerConn
	}

	return s.conn.Notify(ctx, "notifications/resources/updated", params)
}

func (s *Session) addStream() *stream {
	stream := newStream(s.id)
	s.streamsMu.Lock()
	s.streams[stream.id] = stream
	s.streamsMu.Unlock()
	return stream
}

func (s *Session) newRequestID() jsonrpc.ID {
	return jsonrpc.ID(fmt.Sprint(atomic.AddInt64(&s.idCounter, 1)))
}

func (s *Session) addPendingRequest(reqID jsonrpc.ID) (respChan chan jsonrpc.Response, cancelFn func()) {
	respChan = make(chan jsonrpc.Response, 1)
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	s.pending[reqID.String()] = respChan

	cancelFn = func() {
		s.pendingMu.Lock()
		// TODO: Close channel needed?
		delete(s.pending, reqID.String())
		s.pendingMu.Unlock()
	}

	return respChan, cancelFn
}

func (s *Session) handleResponse(res jsonrpc.Response) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()

	if ch, ok := s.pending[res.ID.String()]; ok {
		ch <- res
		delete(s.pending, res.ID.String())
	} else {
		log.Printf("Received response (id: %q) for non pending request.", res.ID.String())
	}
}
