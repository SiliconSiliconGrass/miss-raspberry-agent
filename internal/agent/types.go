package agent

// Denial reason labels included in deny verdicts.
const (
	DenialReasonNoSubstance    = "lacks_substantive_argument"
	DenialReasonPersonalAttack = "personal_attack"
)

// Judgment carries the factual findings about a comment produced by the LLM.
// It deliberately contains no allow/deny field: that rule is applied in code.
type Judgment struct {
	HasSubstantiveArgument bool   `json:"has_substantive_argument"`
	ArgumentSummary        string `json:"argument_summary"`
	PersonalAttack         bool   `json:"personal_attack"`
	AttackEvidence         string `json:"attack_evidence,omitempty"`
}

// Verdict is the final, project-owned moderation result for a comment.
type Verdict struct {
	Allowed       bool     `json:"allowed"`
	DenialReasons []string `json:"denial_reasons,omitempty"`
	Judgment      Judgment `json:"judgment"`
}
