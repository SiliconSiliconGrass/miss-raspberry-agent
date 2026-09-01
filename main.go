// Command main 是项目的正式入口：启动 NapCat 客户端、构造 chat model 与
// main_agent，然后监听 QQ 私聊/群聊消息驱动 agent。
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
	"miss-raspberry-agent/internal/tools/todo_list"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// .env 存在时加载，不存在则忽略（由部署环境注入环境变量）。
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := napcat.NewClient(&napcat.NapcatClientConfig{
		WebSocketURL:  cfg.Napcat.WebSocketURL,
		AccessToken:   cfg.Napcat.AccessToken,
		NickName:      []string{"bot"},
		CommandPrefix: "/",
		SuperUsers:    []int64{},
	})
	if err := client.Start(); err != nil {
		return fmt.Errorf("start napcat client: %w", err)
	}
	defer client.Stop()

	// 直接构造 OpenAI 兼容 chat model，不强制 JSON 输出。
	chatModel, err := einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
		APIKey:  cfg.Model.APIKey,
		BaseURL: cfg.Model.BaseURL,
		Model:   cfg.Model.Name,
	})
	if err != nil {
		return fmt.Errorf("construct chat model: %w", err)
	}

	todo := todo_list.NewStore()
	agent, err := main_agent.NewMainAgent(ctx, client, chatModel, todo)
	if err != nil {
		return fmt.Errorf("build main agent: %w", err)
	}

	log.Println("[main] main_agent 已启动，等待 QQ 消息...")
	agent.Run(ctx)
	return nil
}
