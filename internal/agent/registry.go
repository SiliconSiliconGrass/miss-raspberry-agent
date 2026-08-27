package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/model"
)

// Role names an externally callable agent behavior.
type Role string

// RoleCommentModerator judges whether a comment may be posted.
const RoleCommentModerator Role = "comment_moderator"

// Agent is the minimal contract every registered role implements.
// The result type is shared by all current roles; evolve this interface
// when a role needs a different result shape.
type Agent interface {
	Run(ctx context.Context, input string) (Verdict, error)
}

var _ Agent = (*Moderator)(nil)

// Registry builds every supported agent once at startup and hands them out
// by role. The set of agents is fixed after construction, so concurrent Get
// calls are safe. Construction failures should abort startup.
type Registry struct {
	agents map[Role]Agent
}

// NewRegistry constructs all known agents with the given shared chat model.
// It must be called once at startup, before serving requests.
func NewRegistry(chat model.BaseChatModel) (*Registry, error) {
	return &Registry{
		agents: map[Role]Agent{
			RoleCommentModerator: NewModerator(chat),
		},
	}, nil
}

// Get returns the agent registered under the given role.
func (r *Registry) Get(role Role) (Agent, error) {
	a, ok := r.agents[role]
	if !ok {
		return nil, fmt.Errorf("unknown agent role %q", role)
	}
	return a, nil
}
