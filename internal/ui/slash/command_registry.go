package slash

type CommandCategory string

const (
	CommandCategorySystem CommandCategory = "system"
	CommandCategoryUser   CommandCategory = "user"
	CommandCategoryMCP    CommandCategory = "mcp"
	CommandCategoryAdmin  CommandCategory = "admin"
)

type RiskLevel string

const (
	RiskLevelRead        RiskLevel = "read"
	RiskLevelWrite       RiskLevel = "write"
	RiskLevelNetwork     RiskLevel = "network"
	RiskLevelDestructive RiskLevel = "destructive"
	RiskLevelDangerous   RiskLevel = RiskLevelDestructive
)

type ProviderKind string

const (
	ProviderKindBuiltin       ProviderKind = "builtin"
	ProviderKindCustomCommand ProviderKind = "custom-command"
	ProviderKindMCP           ProviderKind = "mcp"
	ProviderKindSession       ProviderKind = "session"
)

type ProviderInfo struct {
	ID      string
	Name    string
	Kind    ProviderKind
	Source  string
	Version string
}

type Diagnostic struct {
	ProviderID string
	Message    string
	Err        error
}

type CommandContext struct{}

type CommandDescriptor struct {
	ID          string
	Title       string
	Shortcut    string
	Description string
	Category    CommandCategory
	Arguments   []Argument
	Risk        RiskLevel
	Provider    ProviderInfo
	ParentID    string // Non-empty means this command has a parent; used by palette for submenu grouping
	Action      any
}

type CommandProvider interface {
	ProviderInfo() ProviderInfo
	Commands(CommandContext) ([]CommandDescriptor, error)
}

type StaticCommandProvider struct {
	Info  ProviderInfo
	Items []CommandDescriptor
}

func (p StaticCommandProvider) ProviderInfo() ProviderInfo {
	return p.Info
}

func (p StaticCommandProvider) Commands(CommandContext) ([]CommandDescriptor, error) {
	items := make([]CommandDescriptor, len(p.Items))
	copy(items, p.Items)
	for i := range items {
		if items[i].Provider.ID == "" {
			items[i].Provider = p.Info
		}
	}
	return items, nil
}

type CommandRegistry struct {
	providers []CommandProvider
}

func NewCommandRegistry(providers ...CommandProvider) *CommandRegistry {
	registry := &CommandRegistry{}
	registry.providers = append(registry.providers, providers...)
	return registry
}

func (r *CommandRegistry) Commands(ctx CommandContext) ([]CommandDescriptor, []Diagnostic) {
	if r == nil {
		return nil, nil
	}
	var out []CommandDescriptor
	var diagnostics []Diagnostic
	for _, provider := range r.providers {
		if provider == nil {
			continue
		}
		items, err := provider.Commands(ctx)
		if err != nil {
			info := provider.ProviderInfo()
			diagnostics = append(diagnostics, Diagnostic{ProviderID: info.ID, Message: err.Error(), Err: err})
			continue
		}
		out = append(out, items...)
	}
	return out, diagnostics
}

// GetCompletions returns a flat list of all commands, grouped by category.
// Pass an empty filter string to get all commands.
func (r *CommandRegistry) GetCompletions(ctx CommandContext) []CommandDescriptor {
	all, _ := r.Commands(ctx)
	return all
}

// GroupByCategory groups commands by category for UI rendering.
func (r *CommandRegistry) GroupByCategory(ctx CommandContext) map[CommandCategory][]CommandDescriptor {
	all, _ := r.Commands(ctx)
	groups := make(map[CommandCategory][]CommandDescriptor)
	for _, cmd := range all {
		groups[cmd.Category] = append(groups[cmd.Category], cmd)
	}
	return groups
}
