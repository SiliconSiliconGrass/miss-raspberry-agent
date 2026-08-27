// Command moderator evaluates a single comment with the comment-moderator
// agent and prints the structured verdict as JSON.
//
// Usage:
//
//	MODEL_API_KEY=... [MODEL_BASE_URL=...] [MODEL_NAME=...] moderator [comment-file]
//
// The comment is read from the file given as argument, or from stdin.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"miss-raspberry-agent/internal/agent"
	"miss-raspberry-agent/internal/config"
	"miss-raspberry-agent/internal/model"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	comment, err := readComment(os.Args[1:])
	if err != nil {
		return err
	}
	if err := agent.ValidateComment(comment); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx := context.Background()

	chatModel, err := model.New(ctx, cfg.Model)
	if err != nil {
		return fmt.Errorf("construct chat model: %w", err)
	}

	registry, err := agent.NewRegistry(chatModel)
	if err != nil {
		return fmt.Errorf("build agents: %w", err)
	}

	mod, err := registry.Get(agent.RoleCommentModerator)
	if err != nil {
		return err
	}

	verdict, err := mod.Run(ctx, comment)
	if err != nil {
		return err
	}

	out, err := json.MarshalIndent(verdict, "", "  ")
	if err != nil {
		return fmt.Errorf("encode verdict: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

func readComment(args []string) (string, error) {
	var r io.Reader = os.Stdin
	if len(args) > 1 {
		return "", fmt.Errorf("usage: %s [comment-file] (< comment on stdin)", os.Args[0])
	}
	if len(args) == 1 {
		f, err := os.Open(args[0])
		if err != nil {
			return "", fmt.Errorf("open comment file: %w", err)
		}
		defer f.Close()
		r = f
	}

	raw, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("read comment: %w", err)
	}
	return string(raw), nil
}
