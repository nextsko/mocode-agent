package config

import (
	"cmp"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
)

func (c *Config) configureProviders(store *ConfigStore, env Env, resolver VariableResolver, knownProviders []catwalk.Provider) error {
	knownProviderNames := make(map[string]bool)
	restore := PushPopMocodeEnv()
	defer restore()

	// When disable_default_providers is enabled, skip all default/embedded
	// providers entirely. Users must fully specify any providers they want.
	// We skip to the custom provider validation loop which handles all
	// user-configured providers uniformly.
	if c.Options.DisableDefaultProviders {
		knownProviders = nil
	}

	for _, p := range knownProviders {
		knownProviderNames[string(p.ID)] = true
		config, configExists := c.Providers.Get(string(p.ID))
		// if the user configured a known provider we need to allow it to override a couple of parameters
		if configExists {
			mergeProviderOverrides(&p, config)
		}

		headers := map[string]string{}
		if len(p.DefaultHeaders) > 0 {
			maps.Copy(headers, p.DefaultHeaders)
		}
		if len(config.ExtraHeaders) > 0 {
			maps.Copy(headers, config.ExtraHeaders)
		}
		for k, v := range headers {
			resolved, err := resolver.ResolveValue(v)
			if err != nil {
				slog.Error("Could not resolve provider header", "err", err.Error())
				continue
			}
			headers[k] = resolved
		}
		prepared := ProviderConfig{
			ID:                 string(p.ID),
			Name:               p.Name,
			BaseURL:            p.APIEndpoint,
			APIKey:             p.APIKey,
			APIKeyTemplate:     p.APIKey, // Store original template for re-resolution
			OAuthToken:         config.OAuthToken,
			Type:               p.Type,
			Disable:            config.Disable,
			SystemPromptPrefix: config.SystemPromptPrefix,
			ExtraHeaders:       headers,
			ExtraBody:          config.ExtraBody,
			ExtraParams:        make(map[string]string),
			Models:             p.Models,
		}

		switch {
		case p.ID == catwalk.InferenceProviderAnthropic && config.OAuthToken != nil:
			// Claude Code subscription is not supported anymore. Remove to show onboarding.
			if !store.reloadInProgress {
				if err := store.RemoveConfigField(ScopeGlobal, "providers.anthropic"); err != nil {
					slog.Warn("Failed to remove unsupported Anthropic OAuth config", "error", err)
				}
			}
			c.Providers.Del(string(p.ID))
			continue
		}

		switch p.ID {
		// Handle specific providers that require additional configuration
		case catwalk.InferenceProviderVertexAI:
			var (
				project  = env.Get("VERTEXAI_PROJECT")
				location = env.Get("VERTEXAI_LOCATION")
			)
			if project == "" || location == "" {
				if configExists {
					slog.Warn("Skipping Vertex AI provider due to missing credentials")
					c.Providers.Del(string(p.ID))
				}
				continue
			}
			prepared.ExtraParams["project"] = project
			prepared.ExtraParams["location"] = location
		case catwalk.InferenceProviderAzure:
			endpoint, err := resolver.ResolveValue(p.APIEndpoint)
			if err != nil || endpoint == "" {
				if configExists {
					slog.Warn("Skipping Azure provider due to missing API endpoint", "provider", p.ID, "error", err)
					c.Providers.Del(string(p.ID))
				}
				continue
			}
			prepared.BaseURL = endpoint
			prepared.ExtraParams["apiVersion"] = env.Get("AZURE_OPENAI_API_VERSION")
		case catwalk.InferenceProviderBedrock:
			if !hasAWSCredentials(env) {
				if configExists {
					slog.Warn("Skipping Bedrock provider due to missing AWS credentials")
					c.Providers.Del(string(p.ID))
				}
				continue
			}
			prepared.ExtraParams["region"] = env.Get("AWS_REGION")
			if prepared.ExtraParams["region"] == "" {
				prepared.ExtraParams["region"] = env.Get("AWS_DEFAULT_REGION")
			}
			for _, model := range p.Models {
				if !strings.HasPrefix(model.ID, "anthropic.") {
					return fmt.Errorf("bedrock provider only supports anthropic models for now, found: %s", model.ID)
				}
			}
		default:
			// if the provider api or endpoint are missing we skip them
			v, err := resolver.ResolveValue(p.APIKey)
			if v == "" || err != nil {
				if configExists {
					slog.Warn("Skipping provider due to missing API key", "provider", p.ID)
					c.Providers.Del(string(p.ID))
				}
				continue
			}
		}
		c.Providers.Set(string(p.ID), prepared)
	}

	// validate the custom providers
	for id, providerConfig := range c.Providers.Seq2() {
		if knownProviderNames[id] {
			continue
		}

		// Make sure the provider ID is set
		providerConfig.ID = id
		providerConfig.Name = cmp.Or(providerConfig.Name, id) // Use ID as name if not set
		// default to OpenAI if not set
		providerConfig.Type = cmp.Or(providerConfig.Type, catwalk.TypeOpenAICompat)
		if !slices.Contains(catwalk.KnownProviderTypes(), providerConfig.Type) {
			slog.Warn("Skipping custom provider due to unsupported provider type", "provider", id)
			c.Providers.Del(id)
			continue
		}

		if providerConfig.Disable {
			slog.Debug("Skipping custom provider due to disable flag", "provider", id)
			c.Providers.Del(id)
			continue
		}
		if providerConfig.APIKey == "" {
			slog.Warn("Provider is missing API key, this might be OK for local providers", "provider", id)
		}
		if providerConfig.BaseURL == "" {
			slog.Warn("Skipping custom provider due to missing API endpoint", "provider", id)
			c.Providers.Del(id)
			continue
		}
		if len(providerConfig.Models) == 0 {
			slog.Warn("Skipping custom provider because the provider has no models", "provider", id)
			c.Providers.Del(id)
			continue
		}
		apiKey, err := resolver.ResolveValue(providerConfig.APIKey)
		if apiKey == "" || err != nil {
			slog.Warn("Provider is missing API key, this might be OK for local providers", "provider", id)
		}
		baseURL, err := resolver.ResolveValue(providerConfig.BaseURL)
		if baseURL == "" || err != nil {
			slog.Warn("Skipping custom provider due to missing API endpoint", "provider", id, "error", err)
			c.Providers.Del(id)
			continue
		}

		for k, v := range providerConfig.ExtraHeaders {
			resolved, err := resolver.ResolveValue(v)
			if err != nil {
				slog.Error("Could not resolve provider header", "err", err.Error())
				continue
			}
			providerConfig.ExtraHeaders[k] = resolved
		}

		c.Providers.Set(id, providerConfig)
	}

	if c.Providers.Len() == 0 && c.Options.DisableDefaultProviders {
		return fmt.Errorf("default providers are disabled and there are no custom providers are configured")
	}

	return nil
}

func (c *Config) defaultModelSelection(knownProviders []catwalk.Provider) (largeModel SelectedModel, smallModel SelectedModel, err error) {
	if len(knownProviders) == 0 && c.Providers.Len() == 0 {
		err = fmt.Errorf("no providers configured, please configure at least one provider")
		return largeModel, smallModel, err
	}

	// Use the first provider enabled based on the known providers order
	// if no provider found that is known use the first provider configured
	for _, p := range knownProviders {
		providerConfig, ok := c.Providers.Get(string(p.ID))
		if !ok || providerConfig.Disable {
			continue
		}
		defaultLargeModel := c.GetModel(string(p.ID), p.DefaultLargeModelID)
		if defaultLargeModel == nil {
			err = fmt.Errorf("default large model %s not found for provider %s", p.DefaultLargeModelID, p.ID)
			return largeModel, smallModel, err
		}
		largeModel = SelectedModel{
			Provider:        string(p.ID),
			Model:           defaultLargeModel.ID,
			MaxTokens:       defaultLargeModel.DefaultMaxTokens,
			ReasoningEffort: defaultLargeModel.DefaultReasoningEffort,
		}

		defaultSmallModel := c.GetModel(string(p.ID), p.DefaultSmallModelID)
		if defaultSmallModel == nil {
			err = fmt.Errorf("default small model %s not found for provider %s", p.DefaultSmallModelID, p.ID)
			return largeModel, smallModel, err
		}
		smallModel = SelectedModel{
			Provider:        string(p.ID),
			Model:           defaultSmallModel.ID,
			MaxTokens:       defaultSmallModel.DefaultMaxTokens,
			ReasoningEffort: defaultSmallModel.DefaultReasoningEffort,
		}
		return largeModel, smallModel, err
	}

	enabledProviders := c.EnabledProviders()
	slices.SortFunc(enabledProviders, func(a, b ProviderConfig) int {
		return strings.Compare(a.ID, b.ID)
	})

	if len(enabledProviders) == 0 {
		err = fmt.Errorf("no providers configured, please configure at least one provider")
		return largeModel, smallModel, err
	}

	providerConfig := enabledProviders[0]
	if len(providerConfig.Models) == 0 {
		err = fmt.Errorf("provider %s has no models configured", providerConfig.ID)
		return largeModel, smallModel, err
	}
	defaultLargeModel := c.GetModel(providerConfig.ID, providerConfig.Models[0].ID)
	largeModel = SelectedModel{
		Provider:  providerConfig.ID,
		Model:     defaultLargeModel.ID,
		MaxTokens: defaultLargeModel.DefaultMaxTokens,
	}
	defaultSmallModel := c.GetModel(providerConfig.ID, providerConfig.Models[0].ID)
	smallModel = SelectedModel{
		Provider:  providerConfig.ID,
		Model:     defaultSmallModel.ID,
		MaxTokens: defaultSmallModel.DefaultMaxTokens,
	}
	return largeModel, smallModel, err
}

// mergeProviderOverrides applies user-configured provider fields on top of the
// catalog entry. Configured models take precedence; catalog models are appended
// afterwards so that model IDs stay unique.
func mergeProviderOverrides(p *catwalk.Provider, config ProviderConfig) {
	if config.BaseURL != "" {
		p.APIEndpoint = config.BaseURL
	}
	if config.APIKey != "" {
		p.APIKey = config.APIKey
	}
	if len(config.Models) == 0 {
		return
	}

	models := make([]catwalk.Model, 0, len(config.Models)+len(p.Models))
	seen := make(map[string]bool)
	for _, model := range config.Models {
		if seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		if model.Name == "" {
			model.Name = model.ID
		}
		models = append(models, model)
	}
	for _, model := range p.Models {
		if seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		if model.Name == "" {
			model.Name = model.ID
		}
		models = append(models, model)
	}
	p.Models = models
}

func configureSelectedModels(store *ConfigStore, knownProviders []catwalk.Provider, persist bool) error {
	c := store.config
	defaultLarge, defaultSmall, err := c.defaultModelSelection(knownProviders)
	if err != nil {
		return fmt.Errorf("failed to select default models: %w", err)
	}
	largeModelSelected, largeModelConfigured := c.Models[SelectedModelTypeLarge]
	large, err := resolveSelectedModel(c, store, SelectedModelTypeLarge, defaultLarge, largeModelSelected, largeModelConfigured, persist)
	if err != nil {
		return err
	}

	smallModelSelected, smallModelConfigured := c.Models[SelectedModelTypeSmall]
	small, err := resolveSelectedModel(c, store, SelectedModelTypeSmall, defaultSmall, smallModelSelected, smallModelConfigured, persist)
	if err != nil {
		return err
	}

	c.Models[SelectedModelTypeLarge] = large
	c.Models[SelectedModelTypeSmall] = small
	return nil
}

// resolveSelectedModel applies a user-selected model on top of the default
// selection, repairing it to the default when it no longer resolves. The
// repaired selection is persisted when persist is set.
func resolveSelectedModel(
	c *Config,
	store *ConfigStore,
	modelType SelectedModelType,
	defaultModel SelectedModel,
	selected SelectedModel,
	configured bool,
	persist bool,
) (SelectedModel, error) {
	if !configured {
		return defaultModel, nil
	}

	model := defaultModel
	if selected.Model != "" {
		model.Model = selected.Model
	}
	if selected.Provider != "" {
		model.Provider = selected.Provider
	}

	catwalkModel := c.GetModel(model.Provider, model.Model)
	if catwalkModel == nil {
		if !persist {
			return defaultModel, nil
		}
		if err := store.UpdatePreferredModel(ScopeGlobal, modelType, defaultModel); err != nil {
			return SelectedModel{}, fmt.Errorf("failed to update preferred %s model: %w", modelType, err)
		}
		return defaultModel, nil
	}

	if selected.MaxTokens > 0 {
		model.MaxTokens = selected.MaxTokens
	} else {
		model.MaxTokens = catwalkModel.DefaultMaxTokens
	}
	if selected.ReasoningEffort != "" {
		model.ReasoningEffort = selected.ReasoningEffort
	}
	model.Think = selected.Think
	if selected.Temperature != nil {
		model.Temperature = selected.Temperature
	}
	if selected.TopP != nil {
		model.TopP = selected.TopP
	}
	if selected.TopK != nil {
		model.TopK = selected.TopK
	}
	if selected.FrequencyPenalty != nil {
		model.FrequencyPenalty = selected.FrequencyPenalty
	}
	if selected.PresencePenalty != nil {
		model.PresencePenalty = selected.PresencePenalty
	}
	return model, nil
}
