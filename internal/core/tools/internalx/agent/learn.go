// Package agenttools hosts agent-adjacent tools: reasoning scratchpads, todo
// bookkeeping, session exports and skill learning.
package agenttools

import (
	"context"
	_ "embed"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/skills"
)

//go:embed learn.md
var learnDescription []byte

// LearnToolName is the name of the learn tool.
const LearnToolName = "learn"

// Learn actions.
const (
	learnActionCreate = "create"
	learnActionRefine = "refine"
	learnActionList   = "list"
)

// skillDescriptionPrefix is the house style for skill descriptions: the first
// words tell the model *when* to load the skill. See the "新增 skill" section of
// docs/skills-and-roles/README.md and the writing-skills builtin.
const skillDescriptionPrefix = "Use when"

// PermissionGate is the narrow slice of permission.Service that learn needs.
// Keeping it this small lets callers pass the full service (which satisfies it
// structurally) while tests pass a five-line fake. A nil gate means writes are
// pre-approved.
type PermissionGate interface {
	Request(ctx context.Context, opts permission.CreatePermissionRequest) (bool, error)
}

// LearnDeps carries everything the learn tool needs to write a skill.
type LearnDeps struct {
	// Permissions gates every write. Nil skips the prompt (headless/tests).
	Permissions PermissionGate
	// Root is the writable skills directory: the highest-priority user skills
	// path, which skill discovery already reads.
	Root string
	// Known is every discovered skill (builtin + user). Used for name-collision
	// checks and to route refine at the file that is actually in effect.
	Known []*skills.Skill
	// Now overrides the clock in tests.
	Now func() time.Time
}

// LearnParams holds the parameters for a learn tool call.
//
// Required fields differ per action, so requiredness is validated in the
// handler rather than expressed in the schema (which can only mark a field
// unconditionally required).
type LearnParams struct {
	Action string `json:"action" enum:"create,refine,list" description:"create writes a new skill, refine rewrites an existing one, list surveys what already exists"`
	//nolint:lll // schema descriptions are read by the model, not by humans
	Name         string `json:"name,omitempty" description:"Skill name in kebab-case (lowercase words separated by single hyphens), e.g. deploy-runbook. Must match the skill directory name. Required for create and refine."`
	Description  string `json:"description,omitempty" description:"One-line trigger description. MUST start with 'Use when' and say when the skill applies, not what it contains. Required for create, optional for refine."`
	Instructions string `json:"instructions,omitempty" description:"The full markdown body of SKILL.md: the actual procedure, commands and pitfalls to follow. Required for create and refine."`
	Reason       string `json:"reason,omitempty" description:"One sentence on why this is worth remembering, recorded in the skill metadata for later curation."`
}

// LearnPermissionParams is the payload shown to the user when a skill write is
// awaiting approval. It carries both revisions so the UI can render a diff.
type LearnPermissionParams struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Action     string `json:"action"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content"`
}

// LearnResponseMetadata describes what a learn call did.
type LearnResponseMetadata struct {
	Action    string `json:"action"`
	Name      string `json:"name,omitempty"`
	Path      string `json:"path,omitempty"`
	Revision  int    `json:"revision,omitempty"`
	Backup    string `json:"backup,omitempty"`
	Overwrote bool   `json:"overwrote,omitempty"`
	Total     int    `json:"total,omitempty"`
}

// NewLearnTool creates the learn tool, which turns a finished task into a
// durable skill: it writes <root>/<name>/SKILL.md, stamping provenance
// metadata so agent-authored skills stay distinguishable from hand-written and
// bundled ones.
func NewLearnTool(deps LearnDeps) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		LearnToolName,
		toolutil.FirstLineDescription(learnDescription),
		func(ctx context.Context, params LearnParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return runLearn(ctx, deps, params, call)
		},
	)
}

// runLearn dispatches on the requested action. Every failure the model can
// correct is reported as a soft error response so the turn keeps going.
func runLearn(ctx context.Context, deps LearnDeps, p LearnParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	switch action := strings.ToLower(strings.TrimSpace(p.Action)); action {
	case learnActionList:
		return learnList(deps)
	case learnActionCreate:
		return learnCreate(ctx, deps, p, call)
	case learnActionRefine:
		return learnRefine(ctx, deps, p, call)
	case "":
		return softError(fmt.Sprintf("action is required: one of %s, %s, %s",
			learnActionCreate, learnActionRefine, learnActionList)), nil
	default:
		return softError(fmt.Sprintf("unknown action %q: expected one of %s, %s, %s",
			p.Action, learnActionCreate, learnActionRefine, learnActionList)), nil
	}
}

// learnList surveys the skills the agent could create alongside or improve, so
// it can pick create vs refine without guessing. Non-builtin skills are listed
// in full; bundled ones are collapsed to a bare name list purely so the model
// can avoid colliding with them.
func learnList(deps LearnDeps) (fantasy.ToolResponse, error) {
	var user, builtin []*skills.Skill
	for _, s := range sortedKnown(deps.Known) {
		if s.Builtin {
			builtin = append(builtin, s)
			continue
		}
		user = append(user, s)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Discovered skills: %d (%d editable, %d bundled)\n",
		len(user)+len(builtin), len(user), len(builtin))

	if len(user) > 0 {
		sb.WriteString("\nEditable skills (create/refine apply here):\n")
		for _, s := range user {
			origin := s.Metadata[skills.MetaOrigin]
			if origin == "" {
				origin = "hand-written"
			}
			rev := s.Metadata[skills.MetaRevision]
			if rev == "" {
				rev = "1"
			}
			fmt.Fprintf(&sb, "  - %s [%s, rev %s]\n      %s\n      %s\n",
				s.Name, origin, rev, skills.SkillGist(s.Description), s.SkillFilePath)
		}
	}

	if len(builtin) > 0 {
		names := make([]string, 0, len(builtin))
		for _, s := range builtin {
			names = append(names, s.Name)
		}
		sb.WriteString("\nBundled skills (cannot be modified — choose another name):\n  ")
		sb.WriteString(strings.Join(names, ", "))
		sb.WriteString("\n")
	}

	if deps.Root != "" {
		fmt.Fprintf(&sb, "\nNew skills are written to: %s\n", deps.Root)
	} else {
		sb.WriteString("\nNo writable skills directory is configured; create/refine will fail.\n")
	}

	meta := LearnResponseMetadata{Action: learnActionList, Total: len(user) + len(builtin)}
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(sb.String()), meta), nil
}

// learnCreate writes a brand-new skill. It refuses names that would shadow an
// existing one so that a "create" never silently changes what another skill
// does.
func learnCreate(ctx context.Context, deps LearnDeps, p LearnParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	name, errResp := resolveWriteRequest(deps, p.Name, p.Description, p.Instructions, learnActionCreate)
	if errResp != nil {
		return *errResp, nil
	}

	if existing := findKnown(deps.Known, name); existing != nil {
		if existing.Builtin {
			return softError(fmt.Sprintf(
				"%q is a bundled skill (%s). Pick a different name: a learned skill with the same name would shadow the bundled one.",
				name, existing.SkillFilePath)), nil
		}
		return softError(fmt.Sprintf(
			"a skill named %q already exists at %s. Use action=%s to improve it, or pick a different name.",
			name, existing.SkillFilePath, learnActionRefine)), nil
	}

	// A skill written earlier in this session is on disk but not in Known,
	// which is only populated at session start.
	if path, ok := existingSkillFile(deps.Root, name); ok {
		return softError(fmt.Sprintf(
			"a skill named %q already exists at %s. Use action=%s to improve it.",
			name, path, learnActionRefine)), nil
	}

	now := deps.now()
	dir := filepath.Join(deps.Root, name)
	skill := &skills.Skill{
		Name:         name,
		Description:  strings.TrimSpace(p.Description),
		Instructions: strings.TrimSpace(p.Instructions),
		Metadata: map[string]string{
			skills.MetaOrigin:    skills.OriginLearn,
			skills.MetaRevision:  "1",
			skills.MetaCreatedAt: now.Format(time.RFC3339),
			skills.MetaUpdatedAt: now.Format(time.RFC3339),
		},
	}
	if reason := strings.TrimSpace(p.Reason); reason != "" {
		skill.Metadata[skills.MetaReason] = reason
	}

	data, err := skills.Render(skill)
	if err != nil {
		return softError(fmt.Sprintf("failed to render skill: %v", err)), nil
	}

	granted, resp, err := requestWrite(ctx, deps, call, name, dir, learnActionCreate, "", string(data))
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if !granted {
		return *resp, nil
	}

	res, err := skills.WriteFileTo(dir, skill, now)
	if err != nil {
		return softError(fmt.Sprintf("failed to write skill: %v", err)), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Created skill %q (revision 1).\nPath: %s\n", name, res.Path)
	sb.WriteString("\nIt joins <available_skills> from the next session onwards; the current session's skill list is already loaded.")
	if reason := strings.TrimSpace(p.Reason); reason != "" {
		fmt.Fprintf(&sb, "\nRecorded reason: %s", reason)
	}

	metaOut := LearnResponseMetadata{Action: learnActionCreate, Name: name, Path: res.Path, Revision: 1}
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(sb.String()), metaOut), nil
}

// learnRefine rewrites the body of an existing skill in place, preserving its
// provenance.
func learnRefine(ctx context.Context, deps LearnDeps, p LearnParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	name, errResp := resolveWriteRequest(deps, p.Name, "", p.Instructions, learnActionRefine)
	if errResp != nil {
		return *errResp, nil
	}

	dir, existing := resolveRefineTarget(deps, name)
	if dir == "" {
		return softError(fmt.Sprintf(
			"no editable skill named %q was found. Bundled skills cannot be modified; use action=%s to write a new one.",
			name, learnActionCreate)), nil
	}

	now := deps.now()
	// Provenance is never laundered: a hand-written skill stays hand-written,
	// so future automatic curation still skips it.
	meta := make(map[string]string, len(existing.Metadata)+1)
	maps.Copy(meta, existing.Metadata)

	// A missing revision on an agent-written skill means the counter was lost;
	// restart it rather than silently jumping to a misleading high number.
	revision := 1
	if prev, err := strconv.Atoi(meta[skills.MetaRevision]); err == nil && prev > 0 {
		revision = prev + 1
	}
	meta[skills.MetaRevision] = strconv.Itoa(revision)
	meta[skills.MetaUpdatedAt] = now.Format(time.RFC3339)
	if meta[skills.MetaCreatedAt] == "" {
		meta[skills.MetaCreatedAt] = now.Format(time.RFC3339)
	}
	if reason := strings.TrimSpace(p.Reason); reason != "" {
		meta[skills.MetaReason] = reason
	}

	skill := &skills.Skill{
		Name:          existing.Name,
		Description:   existing.Description,
		Instructions:  strings.TrimSpace(p.Instructions),
		License:       existing.License,
		Compatibility: existing.Compatibility,
		Metadata:      meta,
	}
	if desc := strings.TrimSpace(p.Description); desc != "" {
		if descErr := validateDescription(desc); descErr != nil {
			return softError(descErr.Error()), nil
		}
		skill.Description = desc
	}
	if skill.Description == "" {
		return softError(fmt.Sprintf(
			"%q has no description yet; pass description (starting with %q) so it can be discovered.",
			name, skillDescriptionPrefix)), nil
	}

	data, err := skills.Render(skill)
	if err != nil {
		return softError(fmt.Sprintf("failed to render skill: %v", err)), nil
	}

	previous, _ := os.ReadFile(filepath.Join(dir, skills.SkillFileName))
	granted, resp, err := requestWrite(ctx, deps, call, name, dir, learnActionRefine, string(previous), string(data))
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if !granted {
		return *resp, nil
	}

	res, err := skills.WriteFileTo(dir, skill, now)
	if err != nil {
		return softError(fmt.Sprintf("failed to write skill: %v", err)), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Updated skill %q (revision %d).\nPath: %s\n", name, revision, res.Path)
	if res.Backup != "" {
		fmt.Fprintf(&sb, "Previous version backed up to: %s\n", res.Backup)
	}
	sb.WriteString("\nChanges take effect from the next session onwards.")

	metaOut := LearnResponseMetadata{
		Action:    learnActionRefine,
		Name:      name,
		Path:      res.Path,
		Revision:  revision,
		Backup:    res.Backup,
		Overwrote: res.Overwrote,
	}
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(sb.String()), metaOut), nil
}

// requestWrite asks for approval before a skill is written. It reports whether
// the write may proceed; when it may not, resp carries the response to return.
// A nil gate means writes are pre-approved.
func requestWrite(
	ctx context.Context,
	deps LearnDeps,
	call fantasy.ToolCall,
	name, dir, action, oldContent, newContent string,
) (granted bool, resp *fantasy.ToolResponse, err error) {
	if deps.Permissions == nil {
		return true, nil, nil
	}

	path := filepath.Join(dir, skills.SkillFileName)
	verb := "Create"
	if action == learnActionRefine {
		verb = "Update"
	}

	ok, err := deps.Permissions.Request(ctx, permission.CreatePermissionRequest{
		SessionID:   toolutil.GetSessionFromContext(ctx),
		Path:        path,
		ToolCallID:  call.ID,
		ToolName:    LearnToolName,
		Action:      action,
		Description: fmt.Sprintf("%s skill %q at %s", verb, name, path),
		Params: LearnPermissionParams{
			Name:       name,
			Path:       path,
			Action:     action,
			OldContent: oldContent,
			NewContent: newContent,
		},
	})
	if err != nil {
		return false, nil, err
	}
	if !ok {
		denied := toolutil.NewPermissionDeniedResponse()
		return false, &denied, nil
	}
	return true, nil, nil
}

// resolveWriteRequest validates the fields shared by create and refine,
// returning the normalised name or a ready-to-return soft error response.
func resolveWriteRequest(deps LearnDeps, rawName, description, instructions, action string) (string, *fantasy.ToolResponse) {
	name := strings.TrimSpace(rawName)
	if name == "" {
		resp := softError(fmt.Sprintf("name is required for action=%s", action))
		return "", &resp
	}
	if strings.TrimSpace(deps.Root) == "" {
		resp := softError("no writable skills directory is configured for this workspace")
		return "", &resp
	}
	// Skill.Validate enforces every required field; we only want its name rules
	// here, so hand it a throwaway description and let the field-specific checks
	// below produce the real messages.
	if err := (&skills.Skill{Name: name, Description: skillDescriptionPrefix}).Validate(); err != nil {
		resp := softError(fmt.Sprintf("invalid skill name %q: %v", name, err))
		return "", &resp
	}
	if strings.TrimSpace(instructions) == "" {
		resp := softError(fmt.Sprintf("instructions is required for action=%s: provide the full SKILL.md body", action))
		return "", &resp
	}
	if action == learnActionCreate {
		if err := validateDescription(strings.TrimSpace(description)); err != nil {
			resp := softError(err.Error())
			return "", &resp
		}
	}
	return name, nil
}

// validateDescription enforces the house style that makes skills discoverable.
func validateDescription(desc string) error {
	if desc == "" {
		return fmt.Errorf("description is required: one line starting with %q that says when the skill applies", skillDescriptionPrefix)
	}
	if !strings.HasPrefix(desc, skillDescriptionPrefix) {
		return fmt.Errorf("description must start with %q (got %q) so the skill is discoverable at the right moment",
			skillDescriptionPrefix, truncateForMessage(desc))
	}
	if len(desc) > skills.MaxDescriptionLength {
		return fmt.Errorf("description exceeds %d characters", skills.MaxDescriptionLength)
	}
	return nil
}

// resolveRefineTarget picks the directory refine should rewrite. A known skill
// is rewritten where it actually lives, so a skill discovered in any configured
// directory is improved in place rather than shadowed by a copy; otherwise we
// fall back to a skill written earlier in this session under Root.
//
// It returns an empty directory when there is nothing editable to rewrite.
func resolveRefineTarget(deps LearnDeps, name string) (string, *skills.Skill) {
	if known := findKnown(deps.Known, name); known != nil {
		if known.Builtin || known.SkillFilePath == "" {
			return "", nil
		}
		// Read from disk so we merge against what is actually in effect and
		// preserve fields (license, compatibility) the prompt never mentions.
		if s, err := skills.Parse(known.SkillFilePath); err == nil {
			return filepath.Dir(known.SkillFilePath), s
		}
		return filepath.Dir(known.SkillFilePath), known
	}

	path, ok := existingSkillFile(deps.Root, name)
	if !ok {
		return "", nil
	}
	if s, err := skills.Parse(path); err == nil {
		return filepath.Dir(path), s
	}
	return "", nil
}

// findKnown returns the discovered skill with the given name, or nil.
func findKnown(known []*skills.Skill, name string) *skills.Skill {
	for _, s := range known {
		if s != nil && strings.EqualFold(s.Name, name) {
			return s
		}
	}
	return nil
}

// existingSkillFile reports whether <root>/<name>/SKILL.md is already present.
func existingSkillFile(root, name string) (string, bool) {
	if strings.TrimSpace(root) == "" || name == "" {
		return "", false
	}
	path := filepath.Join(root, name, skills.SkillFileName)
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
}

// sortedKnown returns the known skills ordered by name for stable output.
func sortedKnown(known []*skills.Skill) []*skills.Skill {
	out := make([]*skills.Skill, 0, len(known))
	for _, s := range known {
		if s != nil {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func softError(msg string) fantasy.ToolResponse {
	return fantasy.NewTextErrorResponse(msg)
}

func truncateForMessage(s string) string {
	const max = 60
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// now returns the clock the tool should stamp timestamps with.
func (d LearnDeps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}
