package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PingInput struct {
	Name string `json:"name" jsonschema:"who to greet"`
}

type PingOutput struct {
	Message string `json:"message"`
}

type FileReadInput struct {
	Filename string `json:"filename" jsonschema:"the file to read"`
}

type FileReadOutput struct {
	Content string `json:"content"`
}

type SearchInput struct {
	Filename string `json:"filename" jsonschema:"the file to read"`
	Query    string `json:"query" jsonschema:"text to search for (matched literally, case-insensitive)"`
	Regex    bool   `json:"regex,omitempty" jsonschema:"set true only if query is a regular expression"`
}

type SearchOutput struct {
	Content string `json:"content"`
}

const vaultRoot = "/home/zach/nomadic/applied-ai-brain/"

func safePath(root, filename string) (string, error) {
	if filepath.IsAbs(filename) {
		return "", errors.New("error:filepath is abs val")
	}

	joined := filepath.Join(root, filename)

	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", errors.New("note not found")
	}

	rel, err := filepath.Rel(root, resolved)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", errors.New("path escapes the vault")
	}

	return resolved, nil
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

	mcp.AddTool(server, &mcp.Tool{Name: "read_file", Description: "Read the contents of a file"},
		func(ctx context.Context, req *mcp.CallToolRequest, input FileReadInput) (*mcp.CallToolResult, FileReadOutput, error) {
			//using helper function of safepath to pass safepath to model
			path, err := safePath(vaultRoot, input.Filename)

			//error handling after safepath return
			if err != nil {
				return nil, FileReadOutput{}, err
			}

			//reading file of safepath
			data, err := os.ReadFile(path)

			if err != nil {
				return nil, FileReadOutput{}, err
			}

			return nil, FileReadOutput{Content: string(data)}, nil
		})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_notes",
		Description: "Search the notes for a specific pattern",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
		//build command for searching the notes
		cmd := exec.CommandContext(
			ctx,
			"rg",
			"--json",
			"-i",
			"-F",
			"--glob",
			"*.md",
			"--",
			input.Query,
			vaultRoot)

		out, err := cmd.Output()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
					return nil, SearchOutput{}, nil
				}
			return nil, SearchOutput{}, err
		}
	
		_ = out
		return nil, SearchOutput{}, nil
	})

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
