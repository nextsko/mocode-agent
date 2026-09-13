package agent

import "charm.land/fantasy"

func (a *sessionAgent) SetModels(large Model, small Model) {
	a.largeModel.Set(large)
	a.smallModel.Set(small)
}

func (a *sessionAgent) SetTools(tools []fantasy.AgentTool) {
	a.tools.SetSlice(tools)
}

func (a *sessionAgent) SetSystemPrompt(systemPrompt string) {
	a.systemPrompt.Set(systemPrompt)
}

func (a *sessionAgent) SystemPrompt() string {
	return a.systemPrompt.Get()
}

func (a *sessionAgent) Model() Model {
	return a.largeModel.Get()
}

// SmallModel returns the configured small model (used for cheap auxiliary
// calls like the /evo lesson distiller). It is a zero-value Model when the
// small model was never built, whose Model field is nil.
func (a *sessionAgent) SmallModel() Model {
	if a.smallModel == nil {
		return Model{}
	}
	return a.smallModel.Get()
}
