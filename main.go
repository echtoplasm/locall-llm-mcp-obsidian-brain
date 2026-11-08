package main

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"log"
)

type PingInput struct {
	Name string `json:"name" jsonschema:"who to greet"`
}

type PingOutput struct {
	Message string `json:"message"`
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "vault-mcp", Version: "0.1.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{Name: "ping", Description: "Say hello"},
		func(
			context context.Context,
			request *mcp.CallToolRequest,
			input PingInput) (*mcp.CallToolResult, PingOutput, error) {
			return nil, PingOutput{Message: "hello " + input.Name}, nil
		})

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
