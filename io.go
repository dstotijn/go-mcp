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
	"io"
	"os"
)

const StdioSessionID = "stdio"

var stdioRW = &stdioReadWriter{}

// stdioReadWriter implements [io.ReadWriter].
type stdioReadWriter struct{}

// Read intercepts the standard Read call to make it interruptible
func (s *stdioReadWriter) Read(p []byte) (n int, err error) {
	return os.Stdin.Read(p)
}

func (s *stdioReadWriter) Write(p []byte) (n int, err error) {
	return os.Stdout.Write(p)
}

type localConn struct {
	send chan<- []byte
	recv <-chan []byte
}

func (c localConn) Read(p []byte) (n int, err error) {
	msg, ok := <-c.recv
	if ok {
		n = copy(p, msg)
		return n, nil
	}
	return 0, io.EOF
}

func (c localConn) Write(p []byte) (n int, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = io.EOF
		}
	}()
	c.send <- p
	return len(p), nil
}

func (c localConn) Close() error {
	close(c.send)
	return nil
}

func localPipe() (c1, c2 localConn) {
	a := make(chan []byte)
	b := make(chan []byte)

	c1 = localConn{send: a, recv: b}
	c2 = localConn{send: b, recv: a}

	return c1, c2
}
