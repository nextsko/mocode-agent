package config

import "os"

// Env is an interface for reading environment variables.
type Env interface {
	Get(key string) string
	Env() []string
}

type osEnv struct{}

// Get implements Env.
func (o *osEnv) Get(key string) string {
	return os.Getenv(key)
}

func (o *osEnv) Env() []string {
	return os.Environ()
}

// NewEnv returns a new Env backed by the OS environment.
func NewEnv() Env {
	return &osEnv{}
}

type mapEnv struct {
	m map[string]string
}

// Get implements Env.
func (m *mapEnv) Get(key string) string {
	if value, ok := m.m[key]; ok {
		return value
	}
	return ""
}

// Env implements Env.
func (m *mapEnv) Env() []string {
	envSlice := make([]string, 0, len(m.m))
	for k, v := range m.m {
		envSlice = append(envSlice, k+"="+v)
	}
	return envSlice
}

// NewEnvFromMap returns a new Env backed by the given map.
func NewEnvFromMap(m map[string]string) Env {
	if m == nil {
		m = make(map[string]string)
	}
	return &mapEnv{m: m}
}
