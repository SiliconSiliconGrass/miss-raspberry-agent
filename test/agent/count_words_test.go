package agent_test

import (
	"strings"
	"testing"

	"miss-raspberry-agent/internal/agent"
)

func TestCountWords(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"english words", "one two three four", 4},
		{"english with punctuation", "Hello, world! Isn't this great?", 5},
		{"chinese characters", "你好世界", 4},
		{"chinese sentence with punctuation", "这是一个测试评论，包含标点。", 12},
		{"mixed english chinese", "I think 这里很好 ok", 7},
		{"punctuation only", "!!! ... ???", 0},
		{"empty", "", 0},
		{"whitespace only", " \n\t ", 0},
		{"numbers count as words", "we scored 42 points and won 3 matches", 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := agent.CountWords(tt.in); got != tt.want {
				t.Errorf("CountWords(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidateCommentLengthBoundary(t *testing.T) {
	hundred := strings.TrimSpace(strings.Repeat("word ", agent.MinCommentWords))
	if err := agent.ValidateComment(hundred); err == nil {
		t.Error("expected error for exactly 100 words")
	}

	if err := agent.ValidateComment(hundred + " plus"); err != nil {
		t.Errorf("expected %d words to pass, got err=%v", agent.MinCommentWords+1, err)
	}

	chineseHundred := strings.Repeat("好", agent.MinCommentWords)
	if err := agent.ValidateComment(chineseHundred); err == nil {
		t.Error("expected error for exactly 100 Chinese characters")
	}

	if err := agent.ValidateComment(chineseHundred + "好"); err != nil {
		t.Errorf("expected %d Chinese characters to pass, got err=%v", agent.MinCommentWords+1, err)
	}
}
