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

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/dstotijn/go-mcp"
)

var (
	httpAddr string
	useStdio bool
	useSSE   bool
)

func main() {
	flag.StringVar(&httpAddr, "http", ":8080", "HTTP listen address for JSON-RPC over HTTP")
	flag.BoolVar(&useStdio, "stdio", true, "Enable stdio transport")
	flag.BoolVar(&useSSE, "sse", false, "Enable SSE transport")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	transports := []string{}

	opts := []mcp.ServerOption{}

	if useStdio {
		transports = append(transports, "stdio")
		opts = append(opts, mcp.WithStdioTransport())
	}

	var sseURL url.URL

	if useSSE {
		transports = append(transports, "sse")

		host := "localhost"

		hostPart, port, err := net.SplitHostPort(httpAddr)
		if err != nil {
			log.Fatalf("Failed to split host and port: %v", err)
		}

		if hostPart != "" {
			host = hostPart
		}

		sseURL = url.URL{
			Scheme: "http",
			Host:   host + ":" + port,
		}

		opts = append(opts, mcp.WithSSETransport(sseURL))
	}

	mcpServer := mcp.NewServer(mcp.ServerConfig{
		ListResourcesFn:         handleListResourcesRequest,
		ReadResourceFn:          handleReadResourceRequest,
		ListResourceTemplatesFn: handleListResourceTemplatesRequest,
		ListPromptsFn:           handleListPromptsRequest,
		OnRootsListChangedFn:    handleRootsListChanged,
		GetPromptFn:             handleGetPromptRequest,
		OnClientInitializedFn:   handleClientInitialized,
		OnSubscribeResourceFn:   handleSubscribeResource,
	}, opts...)

	mcpServer.Start(ctx)

	type getWeatherArgs struct {
		Location string `json:"location"`
	}

	mcpServer.RegisterTools(mcp.CreateTool(mcp.ToolDef[getWeatherArgs]{
		Name:        "get_weather",
		Description: "Get current weather information for a location",
		HandleFunc: func(ctx context.Context, args getWeatherArgs) *mcp.CallToolResult {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Text: fmt.Sprintf("The weather in %v is sunny.", args.Location),
					},
				},
			}
		},
	}))

	httpServer := &http.Server{
		Addr:    httpAddr,
		Handler: mcpServer,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
	}

	if useSSE {
		go func() {
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("HTTP server error: %v", err)
			}
		}()
	}

	log.Printf("MCP server started, using transports:%v", transports)
	if useSSE {
		log.Printf("SSE transport endpoint: %v", sseURL.String())
	}

	// Wait for interrupt signal.
	<-ctx.Done()
	// Restore signal, allowing "force quit".
	stop()

	timeout := 5 * time.Second
	cancelContext, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Printf("Shutting down server (waiting %s). Press Ctrl+C to force quit.", timeout)

	var wg sync.WaitGroup

	if useSSE {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := httpServer.Shutdown(cancelContext); err != nil && !errors.Is(err, context.DeadlineExceeded) {
				log.Printf("HTTP server shutdown error: %v", err)
			}
		}()
	}

	wg.Wait()
}

func handleListResourcesRequest(ctx context.Context, req mcp.ListResourcesParams) (*mcp.ListResourcesResult, error) {
	return &mcp.ListResourcesResult{
		Resources: []mcp.Resource{
			{
				Name:        "Foobar",
				URI:         "file:///Users/janedoe/Desktop/foobar.txt",
				MimeType:    "text/plain",
				Description: "A text file containing the foobar resource.",
				Annotations: &mcp.Annotations{
					Audience: []mcp.Role{mcp.RoleAssistant},
					Priority: json.Number("1"),
				},
				Size: 42,
			},
		},
		Meta: map[string]any{
			"total": 100,
		},
	}, nil
}

func handleReadResourceRequest(ctx context.Context, req mcp.ReadResourceParams) (*mcp.ReadResourceResult, error) {
	return &mcp.ReadResourceResult{
		Contents: []mcp.Content{
			mcp.TextResourceContents{
				Text: "Hello, world!",
				ResourceContents: mcp.ResourceContents{
					URI:      "file:///Users/janedoe/Desktop/foobar.txt",
					MimeType: "text/plain",
				},
			},
		},
	}, nil
}

func handleListResourceTemplatesRequest(ctx context.Context, req mcp.ListResourceTemplatesParams) (*mcp.ListResourceTemplatesResult, error) {
	return &mcp.ListResourceTemplatesResult{
		ResourceTemplates: []mcp.ResourceTemplate{
			{
				Name:        "Desktop Files",
				Description: "Access files in the user's desktop directory.",
				URITemplate: "file:///Users/janedoe/Desktop/{path}",
			},
		},
	}, nil
}

func handleListPromptsRequest(ctx context.Context, req mcp.ListPromptsParams) (*mcp.ListPromptsResult, error) {
	return &mcp.ListPromptsResult{
		Prompts: []mcp.Prompt{
			{
				Name:        "code_review",
				Description: "Asks the LLM to analyze code quality and suggest improvements",
				Arguments: []mcp.PromptArgument{
					{
						Name:        "code",
						Description: "The code to review",
						Required:    true,
					},
				},
			},
		},
	}, nil
}

func handleGetPromptRequest(ctx context.Context, req mcp.GetPromptParams) (*mcp.GetPromptResult, error) {
	switch req.Name {
	case "code_review":
		code, ok := req.Arguments["code"]
		if !ok {
			return nil, errors.New("code argument is required")
		}
		return &mcp.GetPromptResult{
			Description: "Code review prompt",
			Messages: []mcp.PromptMessage{
				{
					Role: mcp.RoleUser,
					Content: mcp.TextContent{
						Text: "Please review this Python code:\n" + code,
					},
				},
			},
		}, nil
	}
	return nil, errors.New("prompt not found")
}

func handleRootsListChanged(ctx context.Context, session mcp.Session) {
	log.Printf("Roots list changed. Session ID: %s", session.ID())
	roots, err := session.ListRoots(ctx, &mcp.ListRootsParams{})
	if err != nil {
		log.Printf("Failed to list roots: %v", err)
	}
	log.Printf("Listed roots: %v", roots)
}

func handleClientInitialized(ctx context.Context, conn mcp.Session) {
	time.Sleep(1 * time.Second)
	go func() {
		result, err := conn.CreateSamplingMessage(context.Background(), &mcp.CreateSamplingMessageParams{
			Messages: []mcp.SamplingMessage{
				{
					Role:    mcp.RoleUser,
					Content: mcp.TextContent{Text: "What is the capital of France?"},
				},
			},
		})
		if err != nil {
			log.Printf("Failed to create sampling message: %v", err)
		}
		log.Printf("Created sampling message: %v", result)
	}()
}

func handleSubscribeResource(ctx context.Context, session mcp.Session, params mcp.ResourceSubscribeParams) error {
	log.Printf("Subscribed to resource: %v", params)
	return nil
}
