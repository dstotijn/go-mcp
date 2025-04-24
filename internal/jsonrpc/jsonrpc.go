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

package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

const version = "2.0"

// ID represents a JSON-RPC 2.0 request identifier, which can be a string,
// number, or null value according to the specification.
type ID json.RawMessage

// NewStringID creates a new ID from a string value.
func NewStringID(id string) *ID {
	raw, _ := json.Marshal(id)
	idVal := ID(raw)
	return &idVal
}

// NewNumberID creates a new ID from a number value.
func NewNumberID(id int64) *ID {
	raw, _ := json.Marshal(id)
	idVal := ID(raw)
	return &idVal
}

// String returns the string representation of the ID.
func (id ID) String() string {
	return string(id)
}

// MarshalJSON implements the json.Marshaler interface.
func (id ID) MarshalJSON() ([]byte, error) {
	if len(id) == 0 {
		return []byte("null"), nil
	}
	return []byte(id), nil
}

// UnmarshalJSON implements the json.Unmarshaler interface.
// ID can be a string, integer or null.
func (id *ID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*id = nil
		return nil
	}

	// Validate that the ID is either a string or an integer
	var strVal string
	if err := json.Unmarshal(data, &strVal); err != nil {
		var intVal int
		if err := json.Unmarshal(data, &intVal); err != nil {
			return errors.New("ID must be a string, integer, or null")
		}
	}

	*id = make([]byte, len(data))
	copy(*id, data)
	return nil
}

// Request represents a JSON-RPC 2.0 request object.
type Request struct {
	// JSONRPC is a string specifying the version of the JSON-RPC protocol. Must
	// be exactly "2.0".
	JSONRPC string `json:"jsonrpc"`
	// ID is an identifier established by the client. If not included, the
	// request is considered a notification.
	ID ID `json:"id,omitempty"`
	// Method is a string containing the name of the method to be invoked.
	Method string `json:"method"`
	// Params holds the parameter values to be used during the invocation of the
	// method. This member may be omitted.
	Params json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response object.
type Response struct {
	// JSONRPC is a string specifying the version of the JSON-RPC protocol. Must
	// be exactly "2.0".
	JSONRPC string `json:"jsonrpc"`
	// ID is the same as the value of the id member in the Request object. If
	// there was an error in detecting the id in the Request, it must be Null.
	ID ID `json:"id"`
	// Result contains the result of the RPC call if successful. This field must
	// not exist if there was an error.
	Result json.RawMessage `json:"result,omitempty"`
	// Error contains error information if an error occurred. This field must
	// not exist if the call was successful.
	Error *Error `json:"error,omitempty"`
}

// Error represents a JSON-RPC 2.0 error object.
type Error struct {
	// Code is a number that indicates the error type that occurred.
	Code int `json:"code"`

	// Message is a string providing a short description of the error.
	Message string `json:"message"`

	// Data is a primitive or structured value that contains additional
	// information about the error. This may be omitted.
	Data any `json:"data,omitempty"`
}

// Error implements the error interface.
func (e Error) Error() string {
	return fmt.Sprintf("jsonrpc error: code=%d message=%s data=%v", e.Code, e.Message, e.Data)
}

func (e Error) WithData(data any) *Error {
	e.Data = data
	return &e
}

// Standard errors defined by the JSON-RPC 2.0 spec.
var (
	ErrParse          = Error{Code: -32700, Message: "Parse error"}
	ErrInvalidRequest = Error{Code: -32600, Message: "Invalid Request"}
	ErrMethodNotFound = Error{Code: -32601, Message: "Method not found"}
	ErrInvalidParams  = Error{Code: -32602, Message: "Invalid params"}
	ErrInternal       = Error{Code: -32603, Message: "Internal error"}
)

// Custom errors, using the JSON-RPC 2.0 code space for application specific
// errors. We arbitrarily/subjectively start in the double digits for
// readability, in case this needs to be extended in the future to more than 9
// codes.
var (
	ErrNotInitialized      = Error{Code: 10, Message: "Client not initialized"}
	ErrAlreadyInitialized  = Error{Code: 11, Message: "Client already initialized"}
	ErrNotNotification     = Error{Code: 12, Message: "Message is not a notification"}
	ErrMissingRequestID    = Error{Code: 13, Message: "Message is missing a valid ID"}
	ErrFeatureNotSupported = Error{Code: 14, Message: "Feature not supported"}
)

// Handler defines an interface for handling JSON-RPC 2.0 requests.
type Handler interface {
	// Handle processes a JSON-RPC 2.0 request and returns a result and an error.
	// The result will be marshaled as the response result. If an error occurs
	// during handling, it will be returned as the second return value.
	Handle(ctx context.Context, req *Request) (any, error)
}

// HandlerFunc is a function type that implements the Handler interface.
type HandlerFunc func(ctx context.Context, req *Request) (any, error)

// Handle calls the handler function.
func (f HandlerFunc) Handle(ctx context.Context, req *Request) (any, error) {
	return f(ctx, req)
}

// Conn represents a JSON-RPC 2.0 connection.
//
// It's used to send and receive JSON-RPC 2.0 requests. Connections are transport agnostic.
type Conn struct {
	// ReadWriter is used to read and write JSON-RPC 2.0 messages.
	rw io.ReadWriter

	// Handler processes incoming requests.
	h Handler

	// Mutex for synchronizing writes to the connection.
	writeMu sync.Mutex

	// ID counter for outgoing requests.
	idCounter int64

	// Map of pending requests.
	pending   map[string]chan *Response
	pendingMu sync.Mutex

	// Encoder for writing JSON messages.
	enc *json.Encoder

	// Decoder for reading JSON messages.
	dec *json.Decoder
}

// NewConn creates a new JSON-RPC 2.0 connection.
func NewConn(rw io.ReadWriter, h Handler) *Conn {
	return &Conn{
		rw:      rw,
		h:       h,
		pending: make(map[string]chan *Response),
		enc:     json.NewEncoder(rw),
		dec:     json.NewDecoder(rw),
	}
}

// Listen starts listening for incoming JSON-RPC 2.0 messages. It processes
// requests and sends responses until the context is canceled or an error
// occurs.
func (c *Conn) Listen(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			var msg json.RawMessage
			if err := json.NewDecoder(c.rw).Decode(&msg); err != nil {
				if errors.Is(err, io.EOF) {
					return err
				}
				if errors.Is(err, context.Canceled) {
					return err
				}
				go c.sendError(nil, ErrParse)
				continue
			}

			// Handle the incoming message in a new goroutine, which we can
			// safely do because the response to a request (if applicable) will
			// contain the request ID, so responses can be sent back out of
			// order.
			go c.handleIncoming(ctx, msg)
		}
	}
}

// handleIncoming processes a JSON-RPC 2.0 message. It handles both single
// messages and batch messages.
func (c *Conn) handleIncoming(ctx context.Context, msg json.RawMessage) {
	// Try to parse as a batch first
	var batch []json.RawMessage
	err := json.Unmarshal(msg, &batch)
	if err == nil && len(batch) == 0 {
		// Empty batch.
		return
	}
	if err == nil && len(batch) > 0 {
		// Process batch
		responses := make([]*Response, 0, len(batch))
		for _, item := range batch {
			resp := c.processMessage(ctx, item)
			if resp != nil {
				responses = append(responses, resp)
			}
		}

		// Send batch response if there are any responses
		if len(responses) > 0 {
			c.writeMu.Lock()
			defer c.writeMu.Unlock()
			_ = c.enc.Encode(responses)
		}
		return
	}

	// Not a batch, process as a single message.
	resp := c.processMessage(ctx, msg)
	if resp != nil {
		c.writeMu.Lock()
		defer c.writeMu.Unlock()
		_ = c.enc.Encode(resp)
	}
}

// processMessage handles a single JSON-RPC 2.0 message item, which could be
// either a request or a response. Returns a response if one should be sent back.
func (c *Conn) processMessage(ctx context.Context, rawMsg json.RawMessage) *Response {
	// Try to parse the msg
	var msg struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      ID              `json:"id,omitempty"`
		Method  string          `json:"method,omitempty"`
		Result  json.RawMessage `json:"result,omitempty"`
		Error   *Error          `json:"error,omitempty"`
	}

	if err := json.Unmarshal(rawMsg, &msg); err != nil || msg.JSONRPC != version {
		// Invalid message
		if len(msg.ID) > 0 {
			return &Response{
				JSONRPC: version,
				Error:   &ErrInvalidRequest,
				ID:      msg.ID,
			}
		}
		return nil
	}

	// Check if it's a response (has ID and either Result or Error, but no Method)
	if len(msg.ID) > 0 && msg.Method == "" && (len(msg.Result) > 0 || msg.Error != nil) {
		var resp Response
		if err := json.Unmarshal(rawMsg, &resp); err == nil {
			c.handleResponse(&resp)
		}
		return nil
	}

	// It's a request or notification
	var req Request
	if err := json.Unmarshal(rawMsg, &req); err != nil {
		if len(msg.ID) > 0 {
			return &Response{
				JSONRPC: version,
				Error:   &ErrInvalidRequest,
				ID:      msg.ID,
			}
		}
		return nil
	}

	// Process valid request
	resp, err := c.processRequest(ctx, &req)
	if err != nil && len(req.ID) > 0 {
		return &Response{
			JSONRPC: version,
			Error:   &ErrInternal,
			ID:      req.ID,
		}
	}
	return resp
}

// handleResponse processes a JSON-RPC 2.0 response.
func (c *Conn) handleResponse(resp *Response) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	if ch, ok := c.pending[resp.ID.String()]; ok {
		ch <- resp
		delete(c.pending, resp.ID.String())
	}
}

// processRequest handles a JSON-RPC 2.0 request and returns a response and an error.
// If the request is a notification, it returns nil for the response.
func (c *Conn) processRequest(ctx context.Context, req *Request) (*Response, error) {
	// Check for valid JSON-RPC 2.0 request
	if req.JSONRPC != version {
		return &Response{
			JSONRPC: version,
			Error:   &ErrInvalidRequest,
			ID:      req.ID,
		}, nil
	}

	// Process the request with the handler
	result, err := c.h.Handle(ctx, req)

	// For notifications, no response should be sent
	if len(req.ID) == 0 {
		return nil, err
	}

	// If there was an error, create an error response
	if err != nil {
		// Check if it's already a JSON-RPC error
		var rpcErr *Error
		if errors.As(err, &rpcErr) {
			return &Response{
				JSONRPC: version,
				Error:   rpcErr,
				ID:      req.ID,
			}, nil
		}

		// Convert generic error to internal error.
		internalErr := ErrInternal.WithData(map[string]string{
			"detail": err.Error(),
		})

		return &Response{
			JSONRPC: version,
			Error:   internalErr,
			ID:      req.ID,
		}, nil
	}

	// Marshal the result
	var resultJSON json.RawMessage
	if result != nil {
		var err error
		resultJSON, err = json.Marshal(result)
		if err != nil {
			return &Response{
				JSONRPC: version,
				Error: ErrInternal.WithData(map[string]string{
					"detail": err.Error(),
				}),
				ID: req.ID,
			}, nil
		}
	}

	// Create the response
	return &Response{
		JSONRPC: version,
		Result:  resultJSON,
		ID:      req.ID,
	}, nil
}

// Call makes a JSON-RPC 2.0 method call and waits for the response.
func (c *Conn) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	// Create a new request with a unique ID
	id := atomic.AddInt64(&c.idCounter, 1)
	rawID := json.RawMessage(fmt.Sprintf("%d", id))

	var paramsJSON json.RawMessage
	if params != nil {
		var err error
		paramsJSON, err = json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
	}

	req := &Request{
		JSONRPC: version,
		Method:  method,
		Params:  paramsJSON,
		ID:      ID(rawID),
	}

	// Create a channel to receive the response
	respChan := make(chan *Response, 1)
	c.pendingMu.Lock()
	c.pending[req.ID.String()] = respChan
	c.pendingMu.Unlock()

	// Ensure cleanup of pending request in all cases.
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, req.ID.String())
		c.pendingMu.Unlock()
	}()

	// Send the request
	c.writeMu.Lock()
	err := c.enc.Encode(req)
	c.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Wait for the response or context cancellation.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respChan:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// Notify sends a JSON-RPC 2.0 notification.
func (c *Conn) Notify(ctx context.Context, method string, params any) error {
	var paramsJSON json.RawMessage
	if params != nil {
		var err error
		paramsJSON, err = json.Marshal(params)
		if err != nil {
			return fmt.Errorf("failed to marshal params: %w", err)
		}
	}

	req := &Request{
		JSONRPC: version,
		Method:  method,
		Params:  paramsJSON,
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.enc.Encode(req); err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}

	return nil
}

// sendError sends an error response.
func (c *Conn) sendError(id ID, err Error) {
	resp := &Response{
		JSONRPC: version,
		Error:   &err,
		ID:      id,
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.enc.Encode(resp)
}

// BatchRequest represents a single request within a batch call.
type BatchRequest struct {
	// Method is the name of the method to call.
	Method string

	// Params contains the parameters for the method call.
	Params any
}

// BatchResponse represents a response to a batch request.
type BatchResponse struct {
	// Result contains the result of the RPC call if successful.
	Result json.RawMessage

	// Error contains error information if an error occurred.
	Error *Error
}

// CallBatch makes multiple JSON-RPC 2.0 method calls in a single batch request
// and waits for all responses.
func (c *Conn) CallBatch(ctx context.Context, requests []BatchRequest) ([]BatchResponse, error) {
	if len(requests) == 0 {
		return nil, errors.New("empty batch request")
	}

	// Create batch of requests with unique IDs
	batch := make([]Request, len(requests))
	respChans := make(map[string]chan *Response, len(requests))
	rawIDs := make([]json.RawMessage, len(requests))

	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	for i, req := range requests {
		// Create a unique ID for each request
		id := atomic.AddInt64(&c.idCounter, 1)
		rawID := json.RawMessage(fmt.Sprintf("%d", id))
		rawIDs[i] = rawID

		var paramsJSON json.RawMessage
		if req.Params != nil {
			var err error
			paramsJSON, err = json.Marshal(req.Params)
			if err != nil {
				// Clean up any channels we've already created
				for j := 0; j < i; j++ {
					delete(c.pending, string(rawIDs[j]))
				}
				return nil, fmt.Errorf("failed to marshal params for request %d: %w", i, err)
			}
		}

		idVal := ID(rawID)
		batch[i] = Request{
			JSONRPC: version,
			Method:  req.Method,
			Params:  paramsJSON,
			ID:      idVal,
		}

		// Create a channel to receive the response
		respChan := make(chan *Response, 1)
		respChans[string(rawID)] = respChan
		c.pending[string(rawID)] = respChan
	}

	// Send the batch request
	c.writeMu.Lock()
	err := c.enc.Encode(batch)
	c.writeMu.Unlock()

	if err != nil {
		// Clean up all pending requests
		for i := 0; i < len(requests); i++ {
			delete(c.pending, string(rawIDs[i]))
		}
		return nil, fmt.Errorf("failed to send batch request: %w", err)
	}

	// Wait for all responses or context cancellation
	responses := make([]BatchResponse, len(requests))
	responsesReceived := 0

	for i, rawID := range rawIDs {
		select {
		case <-ctx.Done():
			// Clean up remaining pending requests
			for j := i; j < len(requests); j++ {
				delete(c.pending, string(rawIDs[j]))
			}
			return nil, ctx.Err()

		case resp := <-respChans[string(rawID)]:
			delete(c.pending, string(rawID))

			if resp.Error != nil {
				responses[i] = BatchResponse{Error: resp.Error}
			} else {
				responses[i] = BatchResponse{Result: resp.Result}
			}

			responsesReceived++
		}
	}

	return responses, nil
}

// IsCall returns true if the request is a call that requires a response.
// If the request has a non-empty ID, it's a call; otherwise, it's a notification.
func (r *Request) IsCall() bool {
	return len(r.ID) > 0
}
