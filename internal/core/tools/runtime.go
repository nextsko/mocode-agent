package tools

type RuntimeDependencyKey string

const (
	RuntimeDependencyConfigStore     RuntimeDependencyKey = "config_store"
	RuntimeDependencyPermissions     RuntimeDependencyKey = "permissions"
	RuntimeDependencyLSPManager      RuntimeDependencyKey = "lsp_manager"
	RuntimeDependencyHistory         RuntimeDependencyKey = "history"
	RuntimeDependencyFileTracker     RuntimeDependencyKey = "file_tracker"
	RuntimeDependencySessions        RuntimeDependencyKey = "sessions"
	RuntimeDependencyMessages        RuntimeDependencyKey = "messages"
	RuntimeDependencyMemory          RuntimeDependencyKey = "memory"
	RuntimeDependencyAllSkills       RuntimeDependencyKey = "all_skills"
	RuntimeDependencyActiveSkills    RuntimeDependencyKey = "active_skills"
	RuntimeDependencySkillTracker    RuntimeDependencyKey = "skill_tracker"
	RuntimeDependencyModelName       RuntimeDependencyKey = "model_name"
	RuntimeDependencySummarySchedule RuntimeDependencyKey = "summary_schedule"
	RuntimeDependencySessionSearch   RuntimeDependencyKey = "session_search"
)

type RuntimeDependencies interface {
	Get(key RuntimeDependencyKey) (any, bool)
}

type RuntimeDependencyProvider interface {
	RuntimeDependencies() RuntimeDependencies
}

type RuntimeDependencySet struct {
	values map[RuntimeDependencyKey]any
}

func NewRuntimeDependencySet(values map[RuntimeDependencyKey]any) RuntimeDependencySet {
	out := make(map[RuntimeDependencyKey]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return RuntimeDependencySet{values: out}
}

func (d RuntimeDependencySet) Get(key RuntimeDependencyKey) (any, bool) {
	value, ok := d.values[key]
	return value, ok
}

func RuntimeDependency[T any](tctx ToolContext, key RuntimeDependencyKey) (T, bool) {
	var zero T
	provider, ok := tctx.(RuntimeDependencyProvider)
	if !ok {
		return zero, false
	}
	deps := provider.RuntimeDependencies()
	if deps == nil {
		return zero, false
	}
	value, ok := deps.Get(key)
	if !ok {
		return zero, false
	}
	typed, ok := value.(T)
	if !ok {
		return zero, false
	}
	return typed, true
}
