package main_agent_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"

	"miss-raspberry-agent/internal/agent/main_agent"
	"miss-raspberry-agent/internal/napcat"
	"miss-raspberry-agent/internal/tools/todo_list"
)

type mockChatRequest struct {
	Messages []map[string]any `json:"messages"`
}

// strictMockServer 模拟 DeepSeek v4 的严格校验：tool 消息必须紧跟带匹配
// tool_call_id 的 assistant(tool_calls) 消息，且 assistant(tool_calls)/tool
// 消息必须带有 content 字段。不合法时返回 400。
type strictMockServer struct {
	mu       sync.Mutex
	calls    int
	requests []mockChatRequest
	rejected []string
}

func (s *strictMockServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req mockChatRequest
		_ = json.Unmarshal(body, &req)

		s.mu.Lock()
		s.calls++
		s.requests = append(s.requests, req)
		if reason := validateStrictToolSequence(req); reason != "" {
			s.rejected = append(s.rejected, reason)
		}
		n := s.calls
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if n%2 == 1 {
			// 奇数请求：返回 tool call，模拟模型决定调用 todo_list
			callID := "call-" + string(rune('0'+n))
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"test",
				"choices":[{"index":0,"message":{"role":"assistant","content":null,
					"tool_calls":[{"id":"` + callID + `","type":"function","function":{"name":"todo_list","arguments":"{\"action\":\"list\"}"}}]},
					"finish_reason":"tool_calls"}],
				"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
			}`))
			return
		}
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-2","object":"chat.completion","created":2,"model":"test",
			"choices":[{"index":0,"message":{"role":"assistant","content":"好的"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`))
	}
}

// TestMainAgentStrictProviderToolSequence 验证 DeepSeek 严格校验场景：
// 带 tool_calls 的 assistant 消息缺失 content 字段时会被 400 拒绝，
// 我们的请求体补丁必须让所有请求通过校验。
func TestMainAgentStrictProviderToolSequence(t *testing.T) {
	mock := &strictMockServer{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	chatModel, err := einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "test-model",
	})
	if err != nil {
		t.Fatalf("NewChatModel: %v", err)
	}

	client := napcat.NewClient(nil)
	todo := todo_list.NewStore()
	agent, err := main_agent.NewMainAgent(ctx, client, chatModel, todo)
	if err != nil {
		t.Fatalf("NewMainAgent: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		agent.Run(ctx)
	}()
	defer func() {
		cancel()
		<-done
	}()

	client.Incoming <- napcat.Message{MessageType: "private", UserID: 111, Content: "你好"}
	waitServerCalls(t, mock, 4)

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.rejected) > 0 {
		t.Fatalf("strict provider rejected requests: %v", mock.rejected)
	}
	if len(mock.requests) < 2 {
		t.Fatalf("expected at least 2 requests, got %d", len(mock.requests))
	}
	for i, req := range mock.requests {
		for _, m := range req.Messages {
			if role, _ := m["role"].(string); role == "assistant" {
				if _, hasToolCalls := m["tool_calls"]; hasToolCalls {
					if _, hasContent := m["content"]; !hasContent {
						t.Errorf("request #%d assistant tool_calls message missing content field", i+1)
					}
				}
			}
		}
	}
}

// validateStrictToolSequence 模拟 DeepSeek v4 的严格消息序列校验。
func validateStrictToolSequence(req mockChatRequest) string {
	for i, m := range req.Messages {
		role, _ := m["role"].(string)
		switch role {
		case "tool":
			if i == 0 {
				return "Messages with role 'tool' must be a response to a preceding message with 'tool_calls'"
			}
			toolCallID, _ := m["tool_call_id"].(string)
			prev, ok := req.Messages[i-1]["tool_calls"].([]any)
			if !ok || !containsCallID(prev, toolCallID) {
				return "Messages with role 'tool' must be a response to a preceding message with 'tool_calls'"
			}
			if _, hasContent := m["content"]; !hasContent {
				return "Messages with role 'tool' must have content"
			}
		case "assistant":
			if _, hasToolCalls := m["tool_calls"]; hasToolCalls {
				if _, hasContent := m["content"]; !hasContent {
					return "An assistant message with 'tool_calls' must be followed by tool messages and carry content"
				}
			}
		}
	}
	return ""
}

func containsCallID(toolCalls []any, id string) bool {
	for _, tc := range toolCalls {
		t, _ := tc.(map[string]any)
		if t["id"] == id {
			return true
		}
	}
	return false
}

func waitServerCalls(t *testing.T, mock *strictMockServer, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		mock.mu.Lock()
		n := mock.calls
		mock.mu.Unlock()
		if n >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("server calls = %d, want >= %d", n, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
