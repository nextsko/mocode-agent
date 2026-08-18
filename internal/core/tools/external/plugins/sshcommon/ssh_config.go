package sshcommon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// HostInfo mirrors the fields the LLM is likely to need from ssh config.
type HostInfo struct {
	Alias        string `json:"alias"`
	HostName     string `json:"hostname"`
	User         string `json:"user,omitempty"`
	Port         int    `json:"port,omitempty"`
	IdentityFile string `json:"identity_file,omitempty"`
	ProxyJump    string `json:"proxy_jump,omitempty"`
}

// ParseSshConfig performs a minimal ssh_config(5) parser.
func ParseSshConfig(content string) []HostInfo {
	var hosts []HostInfo
	var current *HostInfo

	flush := func() {
		if current != nil && current.Alias != "" && !isWildcardAlias(current.Alias) {
			hosts = append(hosts, *current)
		}
		current = nil
	}

	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := fields[0]
		val := strings.TrimSpace(strings.TrimPrefix(line, key))

		switch key {
		case "Host":
			flush()
			current = &HostInfo{Alias: fields[1]}
		case "HostName":
			if current != nil {
				current.HostName = val
			}
		case "User":
			if current != nil {
				current.User = val
			}
		case "Port":
			if current != nil {
				if p, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
					current.Port = p
				}
			}
		case "IdentityFile":
			if current != nil {
				current.IdentityFile = val
			}
		case "ProxyJump":
			if current != nil {
				current.ProxyJump = val
			}
		}
	}
	flush()
	return hosts
}

func isWildcardAlias(s string) bool {
	return strings.ContainsAny(s, "*?")
}

// StubHome swaps home-directory helpers for tests.
func StubHome(t testingT, dir string) {
	t.Helper()
	prevDir, prevKH, prevCfg, prevExists, prevRead := SSHDirPath, KnownHostsPath, ConfigPath, FileExists, ReadFile
	SSHDirPath = func() string { return dir }
	KnownHostsPath = func() string { return filepath.Join(dir, "known_hosts") }
	ConfigPath = func() string { return filepath.Join(dir, "config") }
	FileExists = func(p string) bool { _, err := os.Stat(p); return err == nil }
	ReadFile = func(p string) ([]byte, error) { return os.ReadFile(p) }
	t.Cleanup(func() {
		SSHDirPath = prevDir
		KnownHostsPath = prevKH
		ConfigPath = prevCfg
		FileExists = prevExists
		ReadFile = prevRead
	})
}

type testingT interface {
	Helper()
	Cleanup(func())
}
