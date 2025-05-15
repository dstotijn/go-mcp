# go-mcp

[![GitHub tag (latest
SemVer)](https://img.shields.io/github/v/tag/dstotijn/go-mcp?label=go%20module)](https://github.com/dstotijn/go-mcp/tags)
[![Go
Reference](https://pkg.go.dev/badge/github.com/dstotijn/go-mcp.svg)](https://pkg.go.dev/github.com/dstotijn/go-mcp)
[![GitHub](https://img.shields.io/github/license/dstotijn/go-mcp)](LICENSE)
[![Go Report
Card](https://goreportcard.com/badge/github.com/dstotijn/go-mcp)](https://goreportcard.com/report/github.com/dstotijn/go-mcp)

Go library for implementing the [Model Context
Protocol](https://modelcontextprotocol.io/) (MCP).

## Features

- [x] Supports protocol revision [2025-03-26](https://spec.modelcontextprotocol.io/specification/2025-03-26/)
- [x] Server support
- [x] Client support
- [x] Type safe RPC handlers without reflection
- [x] Built-in validation of tool arguments

## Installation

```
go get github.com/dstotijn/go-mcp
```

## Usage

See [examples/server/main.go](/examples/server/main.go) for a detailed example
of a server implementation.

## License

[Apache License 2.0](/LICENSE)

© 2025 David Stotijn
