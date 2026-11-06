package config

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// dockerMCPAvailabilityTTL bounds how long a probe result stays fresh.
const dockerMCPAvailabilityTTL = 10 * time.Second

// DockerMCPName is the name of the Docker MCP configuration.
const DockerMCPName = "docker"

// dockerMCPProbe caches the result of probing the local Docker MCP CLI. It is a
// small state holder rather than package-level mutable state, so tests can
// construct isolated probes and the cache lifetime is explicit.
type dockerMCPProbe struct {
	mu         sync.Mutex
	known      bool
	available  bool
	checkedAt  time.Time
	runVersion func(context.Context) error
	now        func() time.Time
}

// newDockerMCPProbe returns a probe backed by the local docker CLI.
func newDockerMCPProbe() *dockerMCPProbe {
	return &dockerMCPProbe{
		runVersion: func(ctx context.Context) error {
			return exec.CommandContext(ctx, "docker", "mcp", "version").Run()
		},
		now: time.Now,
	}
}

// defaultDockerMCPProbe is the process-wide probe used by the package helpers.
var defaultDockerMCPProbe = newDockerMCPProbe()

// IsAvailable runs 'docker mcp version' and reports whether it succeeded.
func (p *dockerMCPProbe) IsAvailable(ctx context.Context) bool {
	return p.runVersion(ctx) == nil
}

// Cached returns the cached availability and whether it is still fresh.
func (p *dockerMCPProbe) Cached() (available bool, fresh bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.known {
		return false, false
	}
	if p.now().Sub(p.checkedAt) > dockerMCPAvailabilityTTL {
		return p.available, false
	}
	return p.available, true
}

// Refresh probes Docker MCP and caches the result.
func (p *dockerMCPProbe) Refresh(ctx context.Context) bool {
	available := p.IsAvailable(ctx)

	p.mu.Lock()
	p.available = available
	p.checkedAt = p.now()
	p.known = true
	p.mu.Unlock()

	return available
}

// IsDockerMCPAvailable checks if Docker MCP is available by running
// 'docker mcp version'.
func IsDockerMCPAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return defaultDockerMCPProbe.IsAvailable(ctx)
}

// DockerMCPAvailabilityCached returns the cached Docker MCP availability and
// whether the cached value is still fresh.
func DockerMCPAvailabilityCached() (available bool, known bool) {
	return defaultDockerMCPProbe.Cached()
}

// RefreshDockerMCPAvailability refreshes and caches Docker MCP availability.
func RefreshDockerMCPAvailability() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return defaultDockerMCPProbe.Refresh(ctx)
}

// IsDockerMCPEnabled checks if Docker MCP is already configured.
func (c *Config) IsDockerMCPEnabled() bool {
	if c.MCP == nil {
		return false
	}
	_, exists := c.MCP[DockerMCPName]
	return exists
}

// DockerMCPConfig returns the default Docker MCP stdio configuration.
func DockerMCPConfig() MCPConfig {
	return MCPConfig{
		Type:     MCPStdio,
		Command:  "docker",
		Args:     []string{"mcp", "gateway", "run"},
		Disabled: false,
	}
}

// PrepareDockerMCPConfig validates Docker MCP availability and stages the
// Docker MCP configuration in memory.
func (s *ConfigStore) PrepareDockerMCPConfig() (MCPConfig, error) {
	if !IsDockerMCPAvailable() {
		return MCPConfig{}, fmt.Errorf("docker mcp is not available, please ensure docker is installed and 'docker mcp version' succeeds")
	}

	mcpConfig := DockerMCPConfig()
	if s.config.MCP == nil {
		s.config.MCP = make(map[string]MCPConfig)
	}
	s.config.MCP[DockerMCPName] = mcpConfig
	return mcpConfig, nil
}

// PersistDockerMCPConfig persists a previously prepared Docker MCP
// configuration to the global config file.
func (s *ConfigStore) PersistDockerMCPConfig(mcpConfig MCPConfig) error {
	if err := s.SetConfigField(ScopeGlobal, "mcp."+DockerMCPName, mcpConfig); err != nil {
		return fmt.Errorf("failed to persist docker mcp configuration: %w", err)
	}
	return nil
}

// EnableDockerMCP adds Docker MCP configuration and persists it.
func (s *ConfigStore) EnableDockerMCP() error {
	mcpConfig, err := s.PrepareDockerMCPConfig()
	if err != nil {
		return err
	}
	if err := s.PersistDockerMCPConfig(mcpConfig); err != nil {
		return err
	}
	return nil
}

// DisableDockerMCP removes Docker MCP configuration and persists the change.
func (s *ConfigStore) DisableDockerMCP() error {
	if s.config.MCP == nil {
		return nil
	}

	// Remove from in-memory config.
	delete(s.config.MCP, DockerMCPName)

	// Persist the updated MCP map to the config file.
	if err := s.SetConfigField(ScopeGlobal, "mcp", s.config.MCP); err != nil {
		return fmt.Errorf("failed to persist docker mcp removal: %w", err)
	}

	return nil
}
