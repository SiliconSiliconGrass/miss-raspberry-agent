package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"miss-raspberry-agent/internal/agent"
)

type fakeChatModel struct {
	content string
	err     error

	calls    int
	messages []*schema.Message
	options  []model.Option
}

func (f *fakeChatModel) Generate(_ context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	f.calls++
	f.messages = input
	f.options = opts
	if f.err != nil {
		return nil, f.err
	}
	return schema.AssistantMessage(f.content, nil), nil
}

func (f *fakeChatModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("stream not implemented")
}

func judgmentJSON(substantive bool, attack bool) string {
	return `{
		"has_substantive_argument": ` + boolLiteral(substantive) + `,
		"argument_summary": " argues X for reasons Y ",
		"personal_attack": ` + boolLiteral(attack) + `,
		"attack_evidence": "you fool"
	}`
}

func boolLiteral(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func newModeratorForTest(content string, err error) (*agent.Moderator, *fakeChatModel) {
	fake := &fakeChatModel{content: content, err: err}
	return agent.NewModerator(fake), fake
}

func TestRunRejectsShortCommentWithoutCallingModel(t *testing.T) {
	mod, fake := newModeratorForTest("", nil)

	_, got := mod.Run(context.Background(), "far too short a comment")
	if got == nil || !strings.Contains(got.Error(), "more than 100 words") {
		t.Fatalf("expected word-count error, got %v", got)
	}
	if fake.calls != 0 {
		t.Errorf("model should not be called, was called %d times", fake.calls)
	}
}

func TestRunAllowed(t *testing.T) {
	mod, fake := newModeratorForTest(judgmentJSON(true, false), nil)

	comment := strings.Repeat("word ", 101)
	verdict, err := mod.Run(context.Background(), comment)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !verdict.Allowed {
		t.Error("expected allowed verdict")
	}
	if len(verdict.DenialReasons) != 0 {
		t.Errorf("expected no denial reasons, got %v", verdict.DenialReasons)
	}
	if !verdict.Judgment.HasSubstantiveArgument || verdict.Judgment.PersonalAttack {
		t.Errorf("unexpected judgment: %+v", verdict.Judgment)
	}

	if fake.calls != 1 {
		t.Fatalf("expected 1 model call, got %d", fake.calls)
	}

	if len(fake.messages) != 2 ||
		fake.messages[0].Role != schema.System ||
		fake.messages[1].Role != schema.User ||
		fake.messages[1].Content != comment {
		t.Fatalf("unexpected prompt messages: %+v", fake.messages)
	}

	opts := model.GetCommonOptions(nil, fake.options...)
	if opts.Temperature == nil || *opts.Temperature != 0 {
		t.Errorf("expected temperature 0, got %+v", opts.Temperature)
	}
}

func TestRunDenyNoSubstance(t *testing.T) {
	mod, _ := newModeratorForTest(judgmentJSON(false, false), nil)

	verdict, err := mod.Run(context.Background(), strings.Repeat("word ", 110))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Allowed {
		t.Error("expected denied verdict")
	}
	want := []string{agent.DenialReasonNoSubstance}
	if !equalStrings(verdict.DenialReasons, want) {
		t.Errorf("denial reasons = %v, want %v", verdict.DenialReasons, want)
	}
}

func TestRunDenyPersonalAttack(t *testing.T) {
	mod, _ := newModeratorForTest(judgmentJSON(true, true), nil)

	verdict, err := mod.Run(context.Background(), strings.Repeat("word ", 120))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Allowed {
		t.Error("expected denied verdict")
	}
	want := []string{agent.DenialReasonPersonalAttack}
	if !equalStrings(verdict.DenialReasons, want) {
		t.Errorf("denial reasons = %v, want %v", verdict.DenialReasons, want)
	}
}

func TestRunDenyBothReasons(t *testing.T) {
	mod, _ := newModeratorForTest(judgmentJSON(false, true), nil)

	verdict, err := mod.Run(context.Background(), strings.Repeat("word ", 150))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Allowed {
		t.Error("expected denied verdict")
	}
	want := []string{agent.DenialReasonNoSubstance, agent.DenialReasonPersonalAttack}
	if !equalStrings(verdict.DenialReasons, want) {
		t.Errorf("denial reasons = %v, want %v", verdict.DenialReasons, want)
	}
}

func TestRunParsesFencedOutput(t *testing.T) {
	fenced := "```json\n" + judgmentJSON(true, false) + "\n```"
	mod, _ := newModeratorForTest(fenced, nil)

	verdict, err := mod.Run(context.Background(), strings.Repeat("word ", 105))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Allowed {
		t.Errorf("expected allowed verdict from fenced JSON, got %+v", verdict)
	}
}

func TestRunInvalidModelOutput(t *testing.T) {
	tests := []struct {
		name    string
		content string
		genErr  error
	}{
		{"not json", "I think this comment is fine.", nil},
		{"malformed json", `{"has_substantive_argument": true,`, nil},
		{"missing fields", "{}", nil},
		{"attack without evidence", `{"has_substantive_argument": true, "argument_summary": "s", "personal_attack": true}`, nil},
		{"model error", "", errors.New("boom")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod, fake := newModeratorForTest(tt.content, tt.genErr)

			_, err := mod.Run(context.Background(), strings.Repeat("word ", 102))
			if err == nil {
				t.Fatal("expected error")
			}
			if fake.calls != 1 {
				t.Errorf("expected exactly 1 model call, got %d", fake.calls)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
