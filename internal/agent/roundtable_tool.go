package agent

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/agent/notify"
	rtpkg "github.com/package-register/mocode/internal/agent/roundtable"
	"github.com/package-register/mocode/internal/agent/tools"
	"github.com/package-register/mocode/internal/config"
	"github.com/package-register/mocode/internal/pubsub"
)

//go:embed templates/roundtable_tool.md
var roundtableToolDescription []byte

const (
	// RoundtableToolName is the coordinator-owned tool that starts or resumes
	// a multi-agent roundtable meeting.
	RoundtableToolName = "roundtable"
)

// RoundtableParticipantInput describes a single seat for a roundtable meeting.
type RoundtableParticipantInput struct {
	// Name is the display name for the seat (e.g. "Moderator").
	Name string `json:"name" description:"Display name for this seat"`
	// AgentID is the Mocode agent/mode ID that will power this seat.
	AgentID string `json:"agent_id" description:"Agent/mode ID from the Mocode config"`
	// Role is a short description of the seat's specialty.
	Role string `json:"role,omitempty" description:"Short role description"`
	// CanExecute allows the seat to use write tools during the execution phase.
	CanExecute bool `json:"can_execute,omitempty" description:"Allow this seat to execute write tools after a plan is approved"`
}

// RoundtableParams is the input schema for the roundtable tool.
type RoundtableParams struct {
	// Topic is the subject of the meeting.
	Topic string `json:"topic" description:"The subject or task for the roundtable"`
	// Participants is an optional list of seats. Defaults are used when empty.
	Participants []RoundtableParticipantInput `json:"participants,omitempty" description:"Optional list of roundtable seats"`
	// MaxTurns caps the discussion length.
	MaxTurns int `json:"max_turns,omitempty" description:"Maximum discussion turns before halting (default 20)"`
	// ResumeID resumes a previously saved roundtable snapshot.
	ResumeID string `json:"resume_id,omitempty" description:"Resume a roundtable by its snapshot ID"`
}

// seatResult is the output of one seat's turn.
type seatResult struct {
	Content string
	Usage   rtpkg.Usage
}

// seatRunner abstracts creating and running per-seat sub-sessions so the core
// roundtable loop can be tested without a full coordinator.
type seatRunner interface {
	CreateSeatSession(ctx context.Context, participantName string) (string, error)
	RunSeat(ctx context.Context, seatSessionID string, participant rtpkg.Participant, phase rtpkg.Phase, prompt rtpkg.TurnPrompt) (seatResult, error)
}

// roundtableRunner executes the turn loop for a single roundtable meeting.
type roundtableRunner struct {
	rt                  *rtpkg.Roundtable
	parentSessionID     string
	roundtableSessionID string
	store               *rtpkg.Store
	notifier            pubsub.Publisher[notify.Notification]
	seatRunner          seatRunner

	ctxBuilder   *rtpkg.ContextBuilder
	seatSessions map[string]string
}

func roundtableResultAsText(rt *rtpkg.Roundtable) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Roundtable %s\n\n", rt.Config.ID)
	fmt.Fprintf(&b, "Topic: %s\n", rt.Config.Topic)
	fmt.Fprintf(&b, "Final phase: %s\n", rt.Phase)
	fmt.Fprintf(&b, "Turns: %d\n", rt.CurrentTurn)
	if rt.LoopDetected {
		fmt.Fprintln(&b, "Loop detected: meeting halted.")
	}
	if rt.MaxTurnsHit {
		fmt.Fprintf(&b, "Max turns (%d) reached.\n", rt.Config.Budget.MaxTurns)
	}
	fmt.Fprintln(&b)

	if rt.Consensus != nil {
		fmt.Fprintln(&b, "## Consensus")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, rt.Consensus.Summary)
		fmt.Fprintln(&b)
	}

	if len(rt.Transcript) > 0 {
		fmt.Fprintln(&b, "## Transcript")
		fmt.Fprintln(&b)
		for _, s := range rt.Transcript {
			fmt.Fprintf(&b, "**[%s] %s**: %s\n\n", s.Kind, s.Speaker, strings.ReplaceAll(s.Content, "\n", " "))
		}
	}

	return b.String()
}

func (c *coordinator) roundtableTool(ctx context.Context) (fantasy.AgentTool, error) {
	_ = ctx
	return fantasy.NewAgentTool(
		RoundtableToolName,
		tools.FirstLineDescription(roundtableToolDescription),
		func(ctx context.Context, params RoundtableParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Topic == "" && params.ResumeID == "" {
				return fantasy.NewTextErrorResponse("topic or resume_id is required"), nil
			}

			sessionID := tools.GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, errors.New("session id missing from context")
			}
			agentMessageID := tools.GetMessageFromContext(ctx)
			if agentMessageID == "" {
				return fantasy.ToolResponse{}, errors.New("agent message id missing from context")
			}

			baseDir := filepath.Dir(c.sessionLogDir)
			store := rtpkg.NewStore(baseDir)

			var rt *rtpkg.Roundtable
			var err error
			if params.ResumeID != "" {
				rt, err = store.Load(ctx, params.ResumeID)
				if err != nil {
					return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to resume roundtable: %v", err)), nil
				}
			} else {
				cfg, cfgErr := configFromRoundtableParams(params)
				if cfgErr != nil {
					return fantasy.NewTextErrorResponse(cfgErr.Error()), nil
				}
				rt, err = rtpkg.New(cfg)
				if err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
			}

			roundtableBaseID := c.sessions.CreateAgentToolSessionID(agentMessageID, call.ID)
			roundtableSession, err := c.sessions.CreateTaskSession(ctx, roundtableBaseID, sessionID, "Roundtable: "+rt.Config.Topic)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("create roundtable session: %w", err)
			}

			runner := &roundtableRunner{
				rt:                  rt,
				parentSessionID:     sessionID,
				roundtableSessionID: roundtableSession.ID,
				store:               store,
				notifier:            c.notify,
				seatRunner: &coordinatorSeatRunner{
					c:                   c,
					parentSessionID:     sessionID,
					roundtableSessionID: roundtableSession.ID,
					messageID:           agentMessageID,
					toolCallID:          call.ID,
				},
				ctxBuilder:   rtpkg.NewContextBuilder(),
				seatSessions: make(map[string]string),
			}

			if runErr := runner.Run(ctx); runErr != nil {
				slog.Warn("Roundtable run failed", "roundtable_id", rt.Config.ID, "error", runErr)
			}

			if costErr := c.updateParentSessionCost(ctx, roundtableSession.ID, sessionID); costErr != nil {
				slog.Warn("Failed to propagate roundtable cost to parent", "error", costErr)
			}

			if c.notify != nil {
				c.notify.Publish(pubsub.CreatedEvent, notify.Notification{
					SessionID:    roundtableSession.ID,
					SessionTitle: rt.Config.Topic,
					Type:         notify.TypeRoundtableFinished,
					ToolName:     RoundtableToolName,
				})
			}

			return fantasy.NewTextResponse(roundtableResultAsText(rt)), nil
		},
	), nil
}

func configFromRoundtableParams(params RoundtableParams) (rtpkg.Config, error) {
	participants := make([]rtpkg.Participant, 0, len(params.Participants))
	for _, p := range params.Participants {
		participants = append(participants, rtpkg.Participant{
			Name:        p.Name,
			AgentName:   p.AgentID,
			IsModerator: strings.EqualFold(p.Role, "moderator"),
			CanExecute:  p.CanExecute,
		})
	}

	if len(participants) == 0 {
		participants = defaultRoundtableParticipants()
	}

	var hasModerator bool
	for _, p := range participants {
		if p.IsModerator {
			hasModerator = true
			break
		}
	}
	if !hasModerator {
		return rtpkg.Config{}, errors.New("roundtable requires at least one moderator")
	}

	maxTurns := params.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 20
	}

	return rtpkg.Config{
		ID:           newRoundtableID(),
		Topic:        params.Topic,
		Participants: participants,
		Budget: rtpkg.Budget{
			MaxTurns: maxTurns,
		},
	}, nil
}

func defaultRoundtableParticipants() []rtpkg.Participant {
	return []rtpkg.Participant{
		{Name: "Moderator", AgentName: config.AgentPlan, IsModerator: true},
		{Name: "Researcher", AgentName: config.AgentTask},
		{Name: "Reviewer", AgentName: config.AgentCoder},
		{Name: "Executor", AgentName: config.AgentCoder, CanExecute: true},
	}
}

func newRoundtableID() string {
	return fmt.Sprintf("rt-%d", time.Now().UnixNano())
}

// Run executes the roundtable turn loop until the meeting ends or the context
// is cancelled.
func (r *roundtableRunner) Run(ctx context.Context) error {
	selector := rtpkg.RoundRobinStrategy{}
	executors := r.executors()
	executionIdx := 0

	for {
		if err := ctx.Err(); err != nil {
			r.addSystemStatement(ctx, "Roundtable interrupted by cancellation.")
			r.rt.Phase = rtpkg.PhaseInterrupted
			break
		}

		if r.rt.Phase == rtpkg.PhaseDone || r.rt.Phase == rtpkg.PhaseInterrupted {
			break
		}

		if over, reason := r.rt.IsOverBudget(); over {
			r.addSystemStatement(ctx, "Budget limit reached: "+reason)
			r.rt.Phase = rtpkg.PhaseInterrupted
			break
		}

		if loop, reason := r.rt.DetectLoop(4); loop {
			r.rt.LoopDetected = true
			r.addSystemStatement(ctx, "Loop detected: "+reason)
			r.rt.Phase = rtpkg.PhaseInterrupted
			break
		}

		var speaker string
		var err error
		if r.rt.Phase == rtpkg.PhaseExecution {
			if executionIdx >= len(executors) {
				r.rt.Phase = rtpkg.PhaseDone
				continue
			}
			speaker = executors[executionIdx]
			executionIdx++
		} else {
			speaker, err = r.rt.NextSpeaker(selector)
			if err != nil {
				r.addSystemStatement(ctx, "Speaker selection failed: "+err.Error())
				r.rt.Phase = rtpkg.PhaseInterrupted
				break
			}
		}

		participant, perr := r.findParticipant(speaker)
		if perr != nil {
			r.addSystemStatement(ctx, "Unknown participant: "+speaker)
			r.rt.Phase = rtpkg.PhaseInterrupted
			break
		}

		prompt, perr := r.ctxBuilder.BuildTurnPrompt(r.rt, speaker)
		if perr != nil {
			r.addSystemStatement(ctx, "Prompt build failed: "+perr.Error())
			r.rt.Phase = rtpkg.PhaseInterrupted
			break
		}

		seatSessionID, ok := r.seatSessions[speaker]
		if !ok {
			seatSessionID, perr = r.seatRunner.CreateSeatSession(ctx, speaker)
			if perr != nil {
				r.addSystemStatement(ctx, fmt.Sprintf("Failed to create seat session for %s: %v", speaker, perr))
				r.rt.Phase = rtpkg.PhaseInterrupted
				break
			}
			r.seatSessions[speaker] = seatSessionID
		}

		result, rerr := r.seatRunner.RunSeat(ctx, seatSessionID, participant, r.rt.Phase, prompt)
		if rerr != nil {
			r.addSystemStatement(ctx, fmt.Sprintf("Seat %s failed at turn %d: %v", speaker, r.rt.CurrentTurn, rerr))
			// Advance the turn counter so a failing seat does not trap the
			// round-robin selector on the same speaker forever.
			r.rt.CurrentTurn++
			continue
		}

		kind, content, motion, vote, parseErr := parseSeatStatement(r.rt, result.Content)
		if parseErr != nil {
			r.addSystemStatement(ctx, fmt.Sprintf("Failed to parse statement from %s: %v", speaker, parseErr))
			continue
		}

		_, aerr := r.rt.Advance(speaker, kind, content, motion, vote, result.Usage)
		if aerr != nil {
			if r.rt.MaxTurnsHit {
				r.rt.Phase = rtpkg.PhaseDone
			} else {
				r.addSystemStatement(ctx, "Advance failed: "+aerr.Error())
				r.rt.Phase = rtpkg.PhaseInterrupted
			}
			break
		}

		if r.rt.Phase == rtpkg.PhaseApproved {
			if len(executors) > 0 {
				r.rt.Phase = rtpkg.PhaseExecution
				r.addSystemStatement(ctx, "Consensus reached. Moving to execution phase.")
			} else {
				r.rt.Phase = rtpkg.PhaseDone
			}
		}

		if err := r.store.Save(ctx, r.rt); err != nil {
			slog.Warn("Failed to save roundtable snapshot", "roundtable_id", r.rt.Config.ID, "error", err)
		}

		r.publishTurn(ctx)
	}

	if r.rt.Phase != rtpkg.PhaseInterrupted {
		if r.rt.Phase != rtpkg.PhaseDone {
			r.rt.Phase = rtpkg.PhaseDone
		}
	}

	if err := r.store.Save(ctx, r.rt); err != nil {
		slog.Warn("Failed to save final roundtable snapshot", "roundtable_id", r.rt.Config.ID, "error", err)
	}

	return nil
}

func (r *roundtableRunner) executors() []string {
	var out []string
	for _, p := range r.rt.Config.Participants {
		if p.CanExecute {
			out = append(out, p.Name)
		}
	}
	return out
}

func (r *roundtableRunner) findParticipant(name string) (rtpkg.Participant, error) {
	for _, p := range r.rt.Config.Participants {
		if p.Name == name {
			return p, nil
		}
	}
	return rtpkg.Participant{}, fmt.Errorf("participant %q not found", name)
}

func (r *roundtableRunner) addSystemStatement(ctx context.Context, content string) {
	r.rt.AddStatement(rtpkg.Statement{
		Speaker: "system",
		Kind:    rtpkg.StatementSystem,
		Content: content,
	})
	if err := r.store.Save(ctx, r.rt); err != nil {
		slog.Warn("Failed to save roundtable snapshot after system statement", "roundtable_id", r.rt.Config.ID, "error", err)
	}
}

func (r *roundtableRunner) publishTurn(ctx context.Context) {
	if r.notifier == nil {
		return
	}
	r.notifier.Publish(pubsub.CreatedEvent, notify.Notification{
		SessionID:    r.roundtableSessionID,
		SessionTitle: r.rt.Config.Topic,
		Type:         notify.TypeRoundtableTurn,
		ToolName:     RoundtableToolName,
	})
}

// coordinatorSeatRunner creates per-seat sub-sessions and runs sub-agents for
// each roundtable turn.
type coordinatorSeatRunner struct {
	c                   *coordinator
	parentSessionID     string
	roundtableSessionID string
	messageID           string
	toolCallID          string
}

func (r *coordinatorSeatRunner) CreateSeatSession(ctx context.Context, participantName string) (string, error) {
	baseID := r.c.sessions.CreateAgentToolSessionID(r.messageID, r.toolCallID+"-"+participantName)
	sess, err := r.c.sessions.CreateTaskSession(ctx, baseID, r.roundtableSessionID, "Roundtable Seat: "+participantName)
	if err != nil {
		return "", fmt.Errorf("create seat session: %w", err)
	}
	return sess.ID, nil
}

func (r *coordinatorSeatRunner) RunSeat(ctx context.Context, seatSessionID string, participant rtpkg.Participant, phase rtpkg.Phase, prompt rtpkg.TurnPrompt) (seatResult, error) {
	allowEdit := phase == rtpkg.PhaseExecution && participant.CanExecute
	agent, err := r.c.buildSubAgent(ctx, participant.AgentName, allowEdit)
	if err != nil {
		return seatResult{}, fmt.Errorf("build seat agent: %w", err)
	}

	agent.SetSystemPrompt(prompt.SystemPrompt)

	model := agent.Model()
	maxTokens := model.CatwalkCfg.DefaultMaxTokens
	if model.ModelCfg.MaxTokens != 0 {
		maxTokens = model.ModelCfg.MaxTokens
	}

	providerCfg, ok := r.c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		return seatResult{}, errModelProviderNotConfigured
	}

	fullPrompt := prompt.Context + "\n\n" + prompt.Instructions
	if phase == rtpkg.PhaseExecution {
		fullPrompt += "\n\nYou are now in the execution phase. You may use file write and bash tools to carry out the approved plan. Be concise and report what you did."
	}

	result, err := agent.Run(ctx, SessionAgentCall{
		SessionID:        seatSessionID,
		Prompt:           fullPrompt,
		MaxOutputTokens:  maxTokens,
		ProviderOptions:  getProviderOptions(model, providerCfg),
		Temperature:      model.ModelCfg.Temperature,
		TopP:             model.ModelCfg.TopP,
		TopK:             model.ModelCfg.TopK,
		FrequencyPenalty: model.ModelCfg.FrequencyPenalty,
		PresencePenalty:  model.ModelCfg.PresencePenalty,
		NonInteractive:   true,
	})
	if err != nil {
		return seatResult{}, fmt.Errorf("seat agent run: %w", err)
	}
	if result == nil {
		return seatResult{}, errors.New("seat agent returned no result")
	}

	if costErr := r.c.updateParentSessionCost(ctx, seatSessionID, r.roundtableSessionID); costErr != nil {
		return seatResult{}, fmt.Errorf("propagate seat cost: %w", costErr)
	}

	usage := rtpkg.Usage{Turns: 1}
	if result.TotalUsage.TotalTokens > 0 {
		usage.InputTokens = int(result.TotalUsage.InputTokens)
		usage.OutputTokens = int(result.TotalUsage.OutputTokens)
		usage.TotalTokens = int(result.TotalUsage.TotalTokens)
	} else {
		for _, step := range result.Steps {
			usage.InputTokens += int(step.Usage.InputTokens)
			usage.OutputTokens += int(step.Usage.OutputTokens)
			usage.TotalTokens += int(step.Usage.TotalTokens)
		}
	}

	return seatResult{
		Content: result.Response.Content.Text(),
		Usage:   usage,
	}, nil
}

// parseSeatStatement inspects a seat's raw output and converts formal motion or
// vote markers into structured transcript entries.  It scans every line of the
// output, stripping leading markdown formatting (**, *, #, >, -), so that
// markers embedded after explanatory text are still recognized.
func parseSeatStatement(rt *rtpkg.Roundtable, raw string) (rtpkg.StatementKind, string, *rtpkg.Motion, *rtpkg.Vote, error) {
	content := strings.TrimSpace(raw)
	if content == "" {
		return rtpkg.StatementChat, content, nil, nil, nil
	}

	for _, line := range strings.Split(content, "\n") {
		cleaned := stripMarkdownLinePrefix(line)
		upper := strings.ToUpper(cleaned)

		if strings.HasPrefix(upper, "MOTION:") {
			rest := strings.TrimSpace(cleaned[len("MOTION:"):])
			parts := strings.SplitN(rest, " ", 2)
			if len(parts) == 0 || parts[0] == "" {
				return rtpkg.StatementChat, raw, nil, nil, errors.New("empty motion type")
			}
			motionType := parseMotionType(parts[0])
			body := ""
			if len(parts) > 1 {
				body = strings.TrimSpace(parts[1])
			}
			body = strings.TrimSuffix(body, "**")
			var payload map[string]any
			if idx := strings.Index(body, "|"); idx >= 0 {
				jsonPart := strings.TrimSpace(body[idx+1:])
				body = strings.TrimSpace(body[:idx])
				_ = json.Unmarshal([]byte(jsonPart), &payload)
			}
			return rtpkg.StatementMotion, body, &rtpkg.Motion{Type: motionType, Payload: payload}, nil, nil
		}

		if strings.HasPrefix(upper, "VOTE:") {
			rest := strings.TrimSpace(cleaned[len("VOTE:"):])
			parts := strings.SplitN(rest, " ", 2)
			value := rtpkg.VoteYes
			if len(parts) > 0 && parts[0] != "" {
				value = parseVoteValue(parts[0])
			}
			reason := ""
			if len(parts) > 1 {
				reason = strings.TrimSpace(parts[1])
			}
			reason = strings.TrimSuffix(reason, "**")
			motionSeq := lastMotionSeq(rt)
			if motionSeq == 0 {
				return rtpkg.StatementChat, raw, nil, nil, errors.New("vote with no active motion — emit MOTION: conclude or MOTION: propose_plan before voting")
			}
			return rtpkg.StatementVote, rest, nil, &rtpkg.Vote{MotionSeq: motionSeq, Value: value, Reason: reason}, nil
		}
	}

	return rtpkg.StatementChat, content, nil, nil, nil
}

// stripMarkdownLinePrefix removes leading whitespace and common markdown
// formatting characters so that markers like MOTION: or VOTE: can be detected
// even when wrapped in bold, headings, or blockquotes.
func stripMarkdownLinePrefix(line string) string {
	s := strings.TrimSpace(line)
	for {
		switch {
		case strings.HasPrefix(s, "**"):
			s = strings.TrimPrefix(s, "**")
		case strings.HasPrefix(s, "*"):
			s = strings.TrimPrefix(s, "*")
		case strings.HasPrefix(s, "#"):
			s = strings.TrimPrefix(s, "#")
		case strings.HasPrefix(s, ">"):
			s = strings.TrimPrefix(s, ">")
		case strings.HasPrefix(s, "-"):
			s = strings.TrimPrefix(s, "-")
		default:
			return strings.TrimSpace(s)
		}
	}
}

func parseMotionType(s string) rtpkg.MotionType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "propose_plan", "plan":
		return rtpkg.MotionProposePlan
	case "request_clarification", "clarify":
		return rtpkg.MotionRequestClarification
	case "call_vote", "vote":
		return rtpkg.MotionCallVote
	case "conclude", "done":
		return rtpkg.MotionConclude
	case "deadlock":
		return rtpkg.MotionDeadlock
	default:
		return rtpkg.MotionProposePlan
	}
}

func parseVoteValue(s string) rtpkg.VoteValue {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "yes", "y", "aye":
		return rtpkg.VoteYes
	case "no", "n", "nay":
		return rtpkg.VoteNo
	case "abstain", "abstention":
		return rtpkg.VoteAbstain
	default:
		return rtpkg.VoteYes
	}
}

func lastMotionSeq(rt *rtpkg.Roundtable) int {
	for i := len(rt.Transcript) - 1; i >= 0; i-- {
		s := rt.Transcript[i]
		if s.Kind == rtpkg.StatementMotion {
			return s.Seq
		}
	}
	return 0
}

// MarshalJSON implements custom JSON for RoundtableParams so the tool schema
// generator can expose the nested participant type.
func (p RoundtableParams) MarshalJSON() ([]byte, error) {
	type alias RoundtableParams
	return json.Marshal(alias(p))
}
