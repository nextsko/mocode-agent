// Package roundtable implements the domain model for Mocode's team mode:
// a persistent, multi-agent roundtable meeting with a moderator, specialists,
// a shared transcript, motions, votes, and consensus extraction.
package roundtable

import "time"

// Phase is the lifecycle stage of a roundtable meeting.
type Phase string

// Possible roundtable phases.
const (
	PhaseOpening     Phase = "opening"
	PhaseDiscussion  Phase = "discussion"
	PhasePlanning    Phase = "planning"
	PhaseVoting      Phase = "voting"
	PhaseApproved    Phase = "approved"
	PhaseExecution   Phase = "execution"
	PhaseDone        Phase = "done"
	PhaseInterrupted Phase = "interrupted"
)

// StatementKind classifies a single entry in the roundtable transcript.
type StatementKind string

// Possible statement kinds.
const (
	StatementChat       StatementKind = "chat"
	StatementToolResult StatementKind = "tool_result"
	StatementMotion     StatementKind = "motion"
	StatementVote       StatementKind = "vote"
	StatementConsensus  StatementKind = "consensus"
	StatementSystem     StatementKind = "system"
)

// MotionType identifies the purpose of a formal motion.
type MotionType string

// Possible motion types.
const (
	MotionProposePlan          MotionType = "propose_plan"
	MotionRequestClarification MotionType = "request_clarification"
	MotionCallVote             MotionType = "call_vote"
	MotionConclude             MotionType = "conclude"
	MotionDeadlock             MotionType = "deadlock"
)

// VoteValue is an explicit vote on a motion.
type VoteValue string

// Possible vote values.
const (
	VoteYes     VoteValue = "yes"
	VoteNo      VoteValue = "no"
	VoteAbstain VoteValue = "abstain"
)

// Participant describes a seat at the roundtable.
type Participant struct {
	Name        string
	AgentName   string
	IsModerator bool
	CanExecute  bool
}

// Motion is a formal proposal made during the meeting.
type Motion struct {
	Type      MotionType
	TargetSeq int
	Payload   map[string]any
}

// Vote records a participant's position on a motion.
type Vote struct {
	MotionSeq int
	Value     VoteValue
	Reason    string
}

// Statement is one turn in the roundtable transcript.
type Statement struct {
	ID        string
	Seq       int
	Speaker   string
	Kind      StatementKind
	Content   string
	Motion    *Motion
	Vote      *Vote
	CreatedAt time.Time
}

// Budget limits resource consumption for a meeting.
type Budget struct {
	MaxTurns         int
	MaxInputTokens   int
	MaxOutputTokens  int
	MaxTotalTokens   int
	PerRoleMaxTokens map[string]int
}

// Usage tracks token consumption for a meeting or turn.
type Usage struct {
	Turns        int
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	ByRole       map[string]RoleUsage
}

// RoleUsage tracks token consumption for a single participant role.
type RoleUsage struct {
	Turns        int
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// Config is the static configuration for a roundtable meeting.
type Config struct {
	ID           string
	Topic        string
	Participants []Participant
	Budget       Budget
}

// Consensus represents an adopted decision or plan.
type Consensus struct {
	Summary string
	Plan    map[string]any
	Votes   map[string]VoteValue
}

// Roundtable is the mutable state of a meeting.
type Roundtable struct {
	Config       Config
	Phase        Phase
	Transcript   []Statement
	CurrentSeq   int
	CurrentTurn  int
	Usage        Usage
	Consensus    *Consensus
	MaxTurnsHit  bool
	LoopDetected bool
}
