// Package flywheel turns completed work into durable, improving capability:
// it archives task runs, distils the minimal successful path, and decides
// whether an existing skill should be updated, upgraded or left alone.
//
// The design follows patterns proven in the field:
//   - Voyager (2305.16291): an ever-growing skill library plus iterative
//     prompting with environment feedback and self-verification;
//   - Reflexion (2303.11366): learn from trial and error via linguistic
//     feedback instead of weight updates;
//   - MemGPT (2310.08560): hierarchical memory — hot context plus cold,
//     addressable archives;
//   - A-MEM (2502.12110): dynamically linked memory notes rather than a
//     fixed store.
package flywheel

import "time"

// Step is one action in a task run. Failed steps are kept as evidence: they
// are what makes the distilled "pitfall" section useful.
type Step struct {
	Action   string `json:"action"`
	Target   string `json:"target,omitempty"`
	Command  string `json:"command,omitempty"`
	Result   string `json:"result,omitempty"`
	Failed   bool   `json:"failed,omitempty"`
	Retries  int    `json:"retries,omitempty"`
	Duration int64  `json:"duration_ms,omitempty"`
}

// TaskRun is one completed piece of work (the "长城任务" record).
type TaskRun struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Goal      string    `json:"goal"`
	Outcome   string    `json:"outcome"` // success | partial | failed
	Steps     []Step    `json:"steps"`
	Lessons   []string  `json:"lessons,omitempty"`
	Artifacts []string  `json:"artifacts,omitempty"`
	TokensIn  int       `json:"tokens_in,omitempty"`
	TokensOut int       `json:"tokens_out,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
}

// Path is the distilled minimal successful path.
type Path struct {
	Steps      []Step  `json:"steps"`
	Dropped    int     `json:"dropped"`
	Retries    int     `json:"retries"`
	SavingsPct float64 `json:"savings_pct"`
}

// Skill is a reusable, versioned capability distilled from one or more runs.
type Skill struct {
	Name         string         `json:"name"`
	Type         string         `json:"type"`
	Description  string         `json:"description"`
	Triggers     []string       `json:"triggers,omitempty"`
	Instructions string         `json:"instructions"`
	Path         []Step         `json:"path,omitempty"`
	Version      int            `json:"version"`
	UpdatedAt    time.Time      `json:"updated_at"`
	Uses         int            `json:"uses"`
	Successes    int            `json:"successes"`
	Failures     int            `json:"failures"`
	History      []SkillVersion `json:"history,omitempty"`
}

// SkillVersion is the immutable record of one skill revision.
type SkillVersion struct {
	Version   int       `json:"version"`
	At        time.Time `json:"at"`
	Reason    string    `json:"reason"`
	PathSteps int       `json:"path_steps"`
}

// DecisionKind is what the flywheel decided to do with a skill draft.
type DecisionKind string

const (
	DecisionCreated  DecisionKind = "created"
	DecisionUpdated  DecisionKind = "updated"
	DecisionUpgraded DecisionKind = "upgraded"
	DecisionSkipped  DecisionKind = "skipped"
)

// Decision explains a skill upsert; the reason is meant to be shown to the
// model so it can learn the promotion criteria instead of guessing them.
type Decision struct {
	Kind    DecisionKind `json:"kind"`
	Skill   *Skill       `json:"skill,omitempty"`
	Reason  string       `json:"reason"`
	Version int          `json:"version"`
}

// Draft is a proposed skill, usually distilled from a task archive.
type Draft struct {
	Name         string
	Type         string
	Description  string
	Triggers     []string
	Instructions string
	Path         []Step
	Lessons      []string
}

// Report is the flywheel's current state: what is known, what is improving.
type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	Tasks       int       `json:"tasks"`
	Skills      int       `json:"skills"`
	Reused      int       `json:"reused"`
	AvgSteps    float64   `json:"avg_steps"`
	BestSkills  []Skill   `json:"best_skills"`
	RecentTasks []TaskRun `json:"recent_tasks"`
}
