package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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
	Filename string `json:"filename" jsonschema:"vault-relative path of the note to read, e.g. 'Projects/Proxmox.md' (as returned by search_notes)"`
}

type FileReadOutput struct {
	Content string `json:"content"`
}

type SearchInput struct {
	Query string `json:"query" jsonschema:"text to search for (matched literally, case-insensitive)"`
	Regex bool   `json:"regex,omitempty" jsonschema:"set true only if query is a regular expression"`
}

type SearchHit struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type SearchOutput struct {
	Results []SearchHit `json:"results"`
}

// rgEvent mirrors the parts of `rg --json` output we care about.
type rgEvent struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
	} `json:"data"`
}

const (
	vaultRoot   = "/home/zach/nomadic/applied-ai-brain"
	searchLimit = 20
)

// safePath turns an untrusted, vault-relative filename into a real path
// that is guaranteed to be inside root, or returns an error.
func safePath(root, filename string) (string, error) {
	if filepath.IsAbs(filename) {
		return "", errors.New("absolute paths are not allowed; use a path relative to the vault")
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

// parseRgOutput turns ripgrep's JSON Lines output into search hits,
// with paths relative to root and at most limit results.
func parseRgOutput(out []byte, root string, limit int) ([]SearchHit, error) {
	hits := []SearchHit{}

	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // allow long lines (up to 1 MB)

	for scanner.Scan() {
		var ev rgEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type != "match" {
			continue
		}

		rel, err := filepath.Rel(root, ev.Data.Path.Text)
		if err != nil {
			rel = ev.Data.Path.Text
		}

		text := strings.TrimSpace(ev.Data.Lines.Text)
		if len(text) > 300 {
			text = text[:300] + "…"
		}

		hits = append(hits, SearchHit{
			Path: filepath.ToSlash(rel),
			Line: ev.Data.LineNumber,
			Text: text,
		})

		if len(hits) >= limit {
			break
		}
	}

	return hits, scanner.Err()
}

func main() {
	log.SetOutput(os.Stderr) // stdout is the MCP transport; never log there

	server := mcp.NewServer(&mcp.Implementation{Name: "vault-mcp", Version: "0.1.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{Name: "ping", Description: "Say hello"},
		func(ctx context.Context, req *mcp.CallToolRequest, input PingInput) (*mcp.CallToolResult, PingOutput, error) {
			return nil, PingOutput{Message: "hello " + input.Name}, nil
		})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_file",
		Description: "Read the full contents of one note from the vault. Cite the note path when you use its contents.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input FileReadInput) (*mcp.CallToolResult, FileReadOutput, error) {
		// every path from the model goes through safePath before touching disk
		path, err := safePath(vaultRoot, input.Filename)
		if err != nil {
			return nil, FileReadOutput{}, err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil, FileReadOutput{}, err
		}

		return nil, FileReadOutput{Content: string(data)}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_notes",
		Description: "Search all notes for text (case-insensitive). Returns matching lines with note paths; call read_file on the best matches.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
		if strings.TrimSpace(input.Query) == "" {
			return nil, SearchOutput{}, errors.New("query is empty")
		}

		args := []string{
			"--json",
			"-i",
			"--max-count", "3", // at most 3 hits per note so one note can't hog the results
			"--glob", "*.md",
			"--glob", "!.obsidian/**",
			"--glob", "!.trash/**",
		}
		if !input.Regex {
			args = append(args, "-F") // literal match unless the model explicitly asks for regex
		}
		args = append(args, "--", input.Query, vaultRoot) // "--" so the query can never be parsed as a flag

		cmd := exec.CommandContext(ctx, "rg", args...)

		out, err := cmd.Output()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
				return nil, SearchOutput{Results: []SearchHit{}}, nil // rg exits 1 on no matches
			}
			return nil, SearchOutput{}, err
		}

		hits, err := parseRgOutput(out, vaultRoot, searchLimit)
		if err != nil {
			return nil, SearchOutput{}, err
		}

		return nil, SearchOutput{Results: hits}, nil
	})

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
