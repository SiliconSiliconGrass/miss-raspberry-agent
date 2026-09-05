// Command main is the project's main entry point: it starts the NapCat client,
// builds the chat model and main_agent, wires qualifying QQ messages into the
// agent's todo queue, and then lets the agent process that queue.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/joho/godotenv"

	"miss-raspberry-agent/internal/agent/main_agent"
	"miss-raspberry-agent/internal/config"
	"miss-raspberry-agent/internal/napcat"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// Load .env if it exists; ignore it otherwise (env vars are injected by the deployment environment).
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Directly construct an OpenAI-compatible chat model without forcing JSON output.
	chatModel, err := einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
		APIKey:  cfg.Model.APIKey,
		BaseURL: cfg.Model.BaseURL,
		Model:   cfg.Model.Name,
	})
	if err != nil {
		return fmt.Errorf("construct chat model: %w", err)
	}

	client := napcat.NewClient(&napcat.NapcatClientConfig{
		WebSocketURL:  cfg.Napcat.WebSocketURL,
		AccessToken:   cfg.Napcat.AccessToken,
		NickName:      []string{"bot"},
		CommandPrefix: "/",
		SuperUsers:    []int64{},
	})

	agent, err := main_agent.NewMainAgent(ctx, chatModel, client, client)
	if err != nil {
		return fmt.Errorf("build main agent: %w", err)
	}

	// Route qualifying QQ messages into the agent's own todo queue before the client starts,
	// so no message is dropped during startup.
	client.SetTodoList(agent.Queue())
	if err := client.Start(); err != nil {
		return fmt.Errorf("start napcat client: %w", err)
	}
	defer client.Stop()

	log.Println("[main] main_agent started, polling its todo queue...")
	agent.Run(ctx)
	return nil
}
