package main_agent_test

import (
	"strings"
	"testing"

	"miss-raspberry-agent/internal/agent/main_agent"
	"miss-raspberry-agent/internal/tools/todo_list"
)

func TestBuildActivationPromptIncludesTodoList(t *testing.T) {
	items := []todo_list.Item{
		{ID: "item-1", Content: "你好", Source: "私聊(用户QQ=123)", TargetType: "private", TargetID: 123, CreatedAt: 1700000000},
		{ID: "item-2", Content: "在吗", Source: "群聊(群号=456,发送者QQ=789)", TargetType: "group", TargetID: 456, CreatedAt: 1700000060},
	}

	prompt := main_agent.BuildActivationPrompt(items)
	for _, want := range []string{"item-1", "item-2", "你好", "在吗", "私聊(用户QQ=123)", "群聊(群号=456,发送者QQ=789)", "private/123", "group/456"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt should contain %q, got:\n%s", want, prompt)
		}
	}
}

func TestBuildActivationPromptEmpty(t *testing.T) {
	prompt := main_agent.BuildActivationPrompt(nil)
	if !strings.Contains(prompt, "空") {
		t.Errorf("empty todo list prompt should mention 空, got:\n%s", prompt)
	}
}
