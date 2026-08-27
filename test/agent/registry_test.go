package agent_test

import (
	"context"
	"strings"
	"testing"

	"miss-raspberry-agent/internal/agent"
)

func TestNewRegistryBuildsAllRoles(t *testing.T) {
	reg, err := agent.NewRegistry(&fakeChatModel{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a, err := reg.Get(agent.RoleCommentModerator)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
}

func TestGetUnknownRole(t *testing.T) {
	reg, _ := agent.NewRegistry(&fakeChatModel{})

	_, err := reg.Get("no_such_role")
	if err == nil || !strings.Contains(err.Error(), "unknown agent role") {
		t.Fatalf("expected unknown role error, got %v", err)
	}
}

func TestRegistryAgentRuns(t *testing.T) {
	fake := &fakeChatModel{content: judgmentJSON(true, false)}
	reg, err := agent.NewRegistry(fake)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a, err := reg.Get(agent.RoleCommentModerator)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	comment := strings.Repeat("word ", 101)
	verdict, err := a.Run(context.Background(), comment)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Allowed {
		t.Errorf("expected allowed verdict, got %+v", verdict)
	}
	if fake.calls != 1 {
		t.Errorf("expected 1 model call, got %d", fake.calls)
	}
}
