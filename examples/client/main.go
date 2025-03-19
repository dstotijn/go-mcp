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
	"log"

	"github.com/dstotijn/go-mcp"
)

func main() {
	ctx := context.Background()

	client := mcp.NewClient(
		mcp.ClientConfig{
			OnCreateSamplingMessageRequest: func(ctx context.Context, params mcp.CreateSamplingMessageParams) (*mcp.CreateSamplingMessageResult, error) {
				return &mcp.CreateSamplingMessageResult{
					Role:       mcp.RoleUser,
					Content:    mcp.TextContent{Text: "Hello, world!"},
					Model:      "gpt-4o",
					StopReason: "stop_reason",
				}, nil
			},
		},
		mcp.WithStdioClientTransport(mcp.StdioClientTransportConfig{
			Command: "npx",
			Args:    []string{"-y", "@modelcontextprotocol/server-everything"},
		}),
	)

	if err := client.Connect(ctx); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	// Initialize the client.
	initResult, err := client.Initialize(ctx, mcp.InitializeParams{
		ProtocolVersion: "0.1",
		ClientInfo: mcp.Implementation{
			Name:    "go-mcp-example-client",
			Version: "0.1.0",
		},
	})
	if err != nil {
		log.Fatalf("Failed to initialize: %v", err)
	}
	log.Printf("Initialize result: %+v", initResult)

	// Send initialized notification.
	if err := client.NotifyInitialized(ctx); err != nil {
		log.Printf("Failed to send initialized notification: %v", err)
	} else {
		log.Println("Sent initialized notification")
	}

	// Send roots list changed notification.
	if err := client.NotifyRootsListChanged(ctx); err != nil {
		log.Printf("Failed to send roots list changed notification: %v", err)
	} else {
		log.Println("Sent roots list changed notification")
	}

	// List resources.
	resources, err := client.ListResources(ctx, mcp.ListResourcesParams{})
	if err != nil {
		log.Fatalf("Failed to list resources: %v", err)
	}
	log.Printf("Resources: %+v", resources)

	// Read resource (if any resources are available).
	if len(resources.Resources) > 0 {
		resource, err := client.ReadResource(ctx, mcp.ReadResourceParams{
			URI: resources.Resources[0].URI,
		})
		if err != nil {
			log.Printf("Failed to read resource: %v", err)
		} else {
			log.Printf("Resource: %+v", resource)
		}
	}

	// List resource templates.
	templates, err := client.ListResourceTemplates(ctx, mcp.ListResourceTemplatesParams{})
	if err != nil {
		log.Fatalf("Failed to list resource templates: %v", err)
	}
	log.Printf("Resource templates: %+v", templates)

	// Subscribe to a resource (if any resources are available).
	if len(resources.Resources) > 0 {
		go func() {
			err := client.SubscribeResource(ctx, mcp.ResourceSubscribeParams{
				URI: resources.Resources[0].URI,
			})
			if err != nil {
				log.Printf("Failed to subscribe to resource: %v", err)
			} else {
				log.Println("Subscribed to resource")
			}
		}()
	}

	// List tools.
	tools, err := client.ListTools(ctx, mcp.ListToolsParams{})
	if err != nil {
		log.Fatalf("Failed to list tools: %v", err)
	}
	log.Printf("Tools: %+v", tools)

	// Call a tool (if any tools are available).
	if len(tools.Tools) > 0 {
		type addArgs struct {
			A int `json:"a"`
			B int `json:"b"`
		}
		toolResult, err := client.CallTool(ctx, "add", addArgs{
			A: 1,
			B: 2,
		})
		if err != nil {
			log.Printf("Failed to call tool: %v", err)
		} else {
			log.Printf("Tool result: %+v", toolResult)
		}
	}

	// List prompts.
	prompts, err := client.ListPrompts(ctx, mcp.ListPromptsParams{})
	if err != nil {
		log.Fatalf("Failed to list prompts: %v", err)
	}
	log.Printf("Prompts: %+v", prompts)

	// Get a prompt (if any prompts are available).
	if len(prompts.Prompts) > 0 {
		prompt, err := client.GetPrompt(ctx, mcp.GetPromptParams{
			Name: prompts.Prompts[0].Name,
		})
		if err != nil {
			log.Printf("Failed to get prompt: %v", err)
		} else {
			log.Printf("Prompt: %+v", prompt)
		}
	}

	// Ping the server.
	if err := client.Ping(ctx); err != nil {
		log.Fatalf("Failed to ping server: %v", err)
	}
	log.Println("Pinged server successfully.")

	// Explicitly disconnect from the server, to test if the client's transport
	// can be closed.
	if err := client.Disconnect(); err != nil {
		log.Printf("Failed to disconnect: %v", err)
	} else {
		log.Println("Disconnected from server.")
	}
}
