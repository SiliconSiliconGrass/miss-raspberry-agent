package main_agent

import (
	"fmt"
	"strings"
	"time"

	"miss-raspberry-agent/internal/tools/todo_list"
)

// BaseSystemPrompt 是 main_agent 的系统提示词。
// 注意：当前 adk 默认在会话值存在时会对 Instruction 做 f-string 格式化，
// 因此提示词中不要使用花括号。
const BaseSystemPrompt = `你是运行在 QQ 上的智能助手 main_agent，负责处理收到的 QQ 私聊和群聊消息。

工作流程：
1. 你被激活时，收到的新消息已经写入待办列表 todo_list，并随本次提示词提供。
2. 请逐项处理当前待办列表：
   - 需要回复的消息，用 qq_message_sender 工具回复对应来源。私聊使用 target_type=private、target_id=用户QQ号；群聊使用 target_type=group、target_id=群号。
   - 如果消息还包含其他可以立即执行的任务（例如查询、整理、计算），先执行再回复结果。
3. 每处理完一项，立即用 todo_list 工具的 complete 操作删除该项（传入其 id）。已经完成的任务必须及时清除，不要遗留。
4. 当所有待办都处理完并删除后，待办列表为空，本轮自动结束；如果列表不为空，你会被再次唤醒继续处理剩余事项。
5. 你有跨消息的对话记忆：之后的每次激活都能看到之前的用户消息、你的回复以及工具调用结果，可据此回答需要上下文的问题。

工具使用规则：
- qq_message_sender：向指定QQ用户或QQ群发送一条文本消息。
- qq_message_getter：获取指定用户或群聊的历史文本消息，可按条数或时间范围查询，用于回顾上下文。
- todo_list：仅用于存放需要立即处理的任务，禁止把长期提醒、定时任务或远期事项放入其中。
`

// BuildActivationPrompt 构造 agent 被激活时的用户提示词，其中包含当前待办列表。
func BuildActivationPrompt(items []todo_list.Item) string {
	var sb strings.Builder
	sb.WriteString("请处理当前待办列表中的全部事项。每条待办都来自需要立即处理的消息，包含消息内容、来源和时间。\n")
	sb.WriteString("回复消息请用 qq_message_sender 发到对应目标；处理完成后用 todo_list 的 complete 操作删除该项。\n\n")

	if len(items) == 0 {
		sb.WriteString("当前待办列表：（空）\n")
		return sb.String()
	}

	sb.WriteString("当前待办列表：\n")
	for i, item := range items {
		fmt.Fprintf(&sb, "%d. id=%s 内容：%s 来源：%s", i+1, item.ID, item.Content, item.Source)
		if item.TargetType != "" && item.TargetID != 0 {
			fmt.Fprintf(&sb, " 回复目标：%s/%d", item.TargetType, item.TargetID)
		}
		fmt.Fprintf(&sb, " 时间：%s\n", formatTime(item.CreatedAt))
	}
	sb.WriteString("\n请开始处理。")
	return sb.String()
}

func formatTime(unix int64) string {
	if unix <= 0 {
		return "-"
	}
	return time.Unix(unix, 0).Format("2006-01-02 15:04:05")
}
