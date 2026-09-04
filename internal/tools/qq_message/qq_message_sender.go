package qq_message

import (
	"context"
	"fmt"
	"log"
	"miss-raspberry-agent/internal/napcat"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

type QQMessageSenderInput struct {
	TargetId   int64  `json:"target_id" jsonschema:"required,description=目标用户QQ号（target_type=private）或目标群聊QQ群号（target_type=group）"`
	TargetType string `json:"target_type" jsonschema:"enum=private,enum=group,default=private,description=目标类型：private=私聊，group=群聊"`
	Content    string `json:"content" jsonschema:"required,description=要发送的文本内容"`
}

type QQMessageSenderOutput struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// SendMessage puts the message into the NapCat client's send queue; the client performs the actual sending.
func SendMessage(ctx context.Context, client *napcat.NapcatClient, in *QQMessageSenderInput) (*QQMessageSenderOutput, error) {
	var ok bool
	switch in.TargetType {
	case "", "private":
		ok = client.SendMessage(in.TargetId, in.Content)
	case "group":
		ok = client.SendGroupMessage(in.TargetId, in.Content)
	default:
		return nil, fmt.Errorf("qq_message_sender: unknown target_type %q (expected private or group)", in.TargetType)
	}
	if !ok {
		return &QQMessageSenderOutput{Success: false, Message: "send queue is full"}, nil
	}
	return &QQMessageSenderOutput{
		Success: true,
		Message: "ok",
	}, nil
}

func NewQQMessageSender(napcatClient *napcat.NapcatClient) tool.BaseTool {
	fn := func(ctx context.Context, in *QQMessageSenderInput) (*QQMessageSenderOutput, error) {
		return SendMessage(ctx, napcatClient, in)
	}
	tool, err := utils.InferTool(
		"qq_message_sender", // tool name, used by the LLM to invoke it
		"向指定QQ用户或QQ群发送一条文本消息", // tool desc, written for the LLM
		fn,
	)
	if err != nil {
		log.Fatal(err)
	}
	return tool
}
