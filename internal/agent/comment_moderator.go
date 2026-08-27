package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// moderatorSystemPrompt instructs the LLM to produce a strict JSON judgment.
const moderatorSystemPrompt = `You are a strict comment moderator for an online discussion community.
You judge exactly one user-provided comment and reply with ONLY a JSON object, no extra prose.

Judge the comment against two independent criteria:

1. Substantive argument: does the comment present a clear point of view AND support it
   with substantive reasoning (evidence, examples, logical steps, concrete analysis)?
   The criterion FAILS when the comment instead:
   - relies on credentials or authority alone ("I am an expert", "trust me, I studied this");
   - makes vague or empty statements ("it is just bad", "obviously wrong", "everyone knows");
   - repeats conclusions without giving reasons for them.

2. Personal attack: does the comment contain insults, mockery, name-calling, or ridicule
   directed at demeaning a specific identifiable individual?
   The criterion PASSES (attack present) only when someone's person - their character,
   intelligence, appearance, motives as a private individual, family, etc. - is targeted.
   Harsh criticism aimed at arguments, positions, organizations, or official actions
   is NOT a personal attack.

The comment may be written in English or Chinese. Judge it fairly regardless of language.
Write the values of "argument_summary" and "attack_evidence" in the same language as the
comment. Use "attack_evidence" to quote the shortest passage proving an attack; use an
empty string when there is no attack.

Reply with ONLY this JSON object and nothing else:
{
  "has_substantive_argument": true|false,
  "argument_summary": "<one or two sentences describing the viewpoint and the quality of its reasoning>",
  "personal_attack": true|false,
  "attack_evidence": "<shortest quote proving the attack, empty string if none>"
}`

// Moderator judges whether a comment should be allowed in discussion.
type Moderator struct {
	chat model.BaseChatModel
}

// NewModerator creates a Moderator backed by the given chat model.
func NewModerator(chat model.BaseChatModel) *Moderator {
	return &Moderator{chat: chat}
}

// ValidateComment reports whether the comment satisfies the moderator's
// structural requirements (length threshold) without calling the model.
func ValidateComment(comment string) error {
	return validateCommentLength(comment)
}

// Run evaluates one comment and returns the structured verdict.
// Comments at or below the word threshold are rejected without calling the model.
func (m *Moderator) Run(ctx context.Context, comment string) (Verdict, error) {
	if err := validateCommentLength(comment); err != nil {
		return Verdict{}, err
	}

	resp, err := m.chat.Generate(
		ctx,
		[]*schema.Message{
			schema.SystemMessage(moderatorSystemPrompt),
			schema.UserMessage(comment),
		},
		model.WithTemperature(0),
	)
	if err != nil {
		return Verdict{}, fmt.Errorf("model generate: %w", err)
	}

	judgment, err := parseJudgment(resp.Content)
	if err != nil {
		return Verdict{}, fmt.Errorf("parse model output: %w", err)
	}
	return buildVerdict(judgment), nil
}

func buildVerdict(j Judgment) Verdict {
	var reasons []string
	if !j.HasSubstantiveArgument {
		reasons = append(reasons, DenialReasonNoSubstance)
	}
	if j.PersonalAttack {
		reasons = append(reasons, DenialReasonPersonalAttack)
	}
	return Verdict{
		Allowed:       len(reasons) == 0,
		DenialReasons: reasons,
		Judgment:      j,
	}
}

// parseJudgment decodes the JSON object produced by the model into a Judgment,
// tolerating surrounding whitespace or markdown fences.
func parseJudgment(content string) (Judgment, error) {
	var j Judgment

	s := strings.TrimSpace(content)
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return Judgment{}, fmt.Errorf("no JSON object found in: %.100q", content)
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), &j); err != nil {
		return Judgment{}, fmt.Errorf("decode judgment JSON: %w", err)
	}
	if err := validateJudgment(j); err != nil {
		return Judgment{}, err
	}
	return j, nil
}

// validateJudgment rejects judgments missing the fields the prompt requires,
// so a verdict is never built from degenerate model output.
func validateJudgment(j Judgment) error {
	if j.ArgumentSummary == "" {
		return errors.New("argument_summary is required")
	}
	if j.PersonalAttack && j.AttackEvidence == "" {
		return errors.New("attack_evidence is required when personal_attack is true")
	}
	return nil
}
