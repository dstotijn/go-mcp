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
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/dstotijn/go-mcp/internal/jsonrpc"
)

type streamEvent struct {
	id   string
	data json.RawMessage
}

type stream struct {
	id         string
	sessionID  string
	clientConn localConn
	serverConn localConn
	events     []streamEvent
}

func newStream(sessionID string) *stream {
	return &stream{
		id:        generateStreamID(),
		sessionID: sessionID,
	}
}

func (s *stream) newClientLocalConn() localConn {
	client, server := localPipe()
	s.clientConn = client
	s.serverConn = server
	return client
}

func (s *stream) close() {
	_ = s.serverConn.Close()
}

func (s *stream) handleHTTPRequest(ctx context.Context, w http.ResponseWriter, req *http.Request) error {
	var jsonBody json.RawMessage
	err := json.NewDecoder(req.Body).Decode(&jsonBody)
	if err != nil {
		return fmt.Errorf("failed to decode JSON from request body: %w", err)
	}

	messages, _, err := jsonrpc.ParseRawJSONMessage(jsonBody)
	if err != nil {
		return fmt.Errorf("failed to parse JSON from request body: %w", err)
	}

	onlyResponsesAndNotifications := true
	// Track non-notification requests, so we can later know when to end the
	// SSE connection.
	inReqs := make(map[string]jsonrpc.Message)
	for _, msg := range messages {
		if msg.IsResponse() || !msg.IsNotification() {
			onlyResponsesAndNotifications = false
		}
		if msg.IsRequest() && !msg.IsNotification() {
			inReqs[msg.ID.String()] = msg
		}
	}

	bodyBuf := bytes.NewBuffer(jsonBody)
	_, _ = io.Copy(s.serverConn, bodyBuf)

	if onlyResponsesAndNotifications {
		w.WriteHeader(http.StatusAccepted)
		return nil
	}

	rc := http.NewResponseController(w)
	writeSSEHeaders(w)
	s.writeEvents(ctx, inReqs, w, rc)

	return nil
}

func (s *stream) writeEvents(ctx context.Context, inReqs map[string]jsonrpc.Message, w http.ResponseWriter, rc *http.ResponseController) {
	resHandledCount := 0

	for {
		// Return once each request has yielded a response.
		if len(inReqs) > 0 && len(inReqs) == resHandledCount {
			return
		}
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-s.serverConn.recv:
			if !ok {
				return
			}
			var parsedMsg jsonrpc.Message
			event := streamEvent{
				id:   generateEventID(),
				data: msg,
			}
			s.events = append(s.events, event)
			err := json.Unmarshal(msg, &parsedMsg)
			if err != nil {
				// TODO: Write internal server error.
				continue
			}
			// If the initialize request has been handled and yielded a
			// non-error response, write the session ID as a HTTP header.
			//
			// Note: This is somewhat brittle as we assume the initialize
			// response is the first response handled by the server -- if not,
			// we'll be erroneously writing a HTTP header after writing the body
			// has already started.
			if inReq, ok := inReqs[parsedMsg.ID.String()]; ok {
				if inReq.Method == "initialize" && parsedMsg.IsResponse() && parsedMsg.Error == nil {
					w.Header().Set("Mcp-Session-Id", s.sessionID)
				}
			}
			writeServerSentEvent(w, "message", event.id, string(event.data))
			rc.Flush()
			if parsedMsg.IsResponse() {
				if _, ok := inReqs[parsedMsg.ID.String()]; ok {
					resHandledCount++
				}
			}
		}
	}
}

func generateStreamID() string {
	bytes := make([]byte, 16)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func generateEventID() string {
	bytes := make([]byte, 16)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func writeSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
}
