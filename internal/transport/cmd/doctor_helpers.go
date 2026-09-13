package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/config"
)

func collectDoctorWorkspaceFacts(ctx context.Context, cwd string) doctorWorkspaceFacts {
	facts := doctorWorkspaceFacts{
		HasGoMod:      fileExists(filepath.Join(cwd, "go.mod")),
		HasTaskfile:   fileExists(filepath.Join(cwd, "Taskfile.yaml")),
		HasAgents:     fileExists(filepath.Join(cwd, "AGENTS.md")),
		HasProjectCfg: fileExists(filepath.Join(cwd, "mocode.json")),
	}

	if facts.HasGoMod {
		if data, err := os.ReadFile(filepath.Join(cwd, "go.mod")); err == nil {
			facts.ModulePath = parseDoctorModulePath(data)
		}
	}

	gitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	gitCmd := exec.CommandContext(gitCtx, "git", "rev-parse", "--is-inside-work-tree")
	gitCmd.Dir = cwd
	out, err := gitCmd.Output()
	facts.GitWorktree = err == nil && strings.TrimSpace(string(out)) == "true"

	return facts
}

func inspectDoctorPath(path string) doctorPathState {
	state := doctorPathState{Path: path}
	info, err := os.Stat(path)
	if err == nil {
		state.Exists = true
		state.NearestExisting = path
		state.Readable = modeReadable(info.Mode())
		state.Writable = modeWritable(info.Mode())
		return state
	}
	if !os.IsNotExist(err) {
		state.Err = err
		return state
	}

	parent := nearestExistingDir(filepath.Dir(path))
	state.NearestExisting = parent
	if parent == "" {
		return state
	}
	parentInfo, parentErr := os.Stat(parent)
	if parentErr != nil {
		state.Err = parentErr
		return state
	}
	state.Readable = modeReadable(parentInfo.Mode())
	state.Writable = modeWritable(parentInfo.Mode())
	return state
}

func nearestExistingDir(path string) string {
	path = filepath.Clean(path)
	for path != "" && path != string(filepath.Separator) && path != "." {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		next := filepath.Dir(path)
		if next == path {
			break
		}
		path = next
	}
	if _, err := os.Stat(string(filepath.Separator)); err == nil {
		return string(filepath.Separator)
	}
	return ""
}

func parseDoctorModulePath(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "module ") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(line, "module "))
	}
	return ""
}

func countJSONMapField(data []byte, key string) int {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return 0
	}
	raw, ok := root[key]
	if !ok {
		return 0
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return 0
	}
	return len(values)
}

func modeReadable(mode os.FileMode) bool {
	return mode.Perm()&0o444 != 0
}

func modeWritable(mode os.FileMode) bool {
	return mode.Perm()&0o222 != 0
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func ensureDoctorAgentsConfigured(cfg *config.Config) {
	if cfg == nil {
		return
	}
	if len(cfg.Agents) > 0 {
		return
	}
	cfg.SetupAgents()
}

func classifyDoctorProviderError(err error) doctorStatus {
	return classifyDoctorProviderIssue(err).Status
}

func classifyDoctorProviderIssue(err error) doctorProviderIssue {
	if err == nil {
		return doctorProviderIssue{
			Status: doctorStatusOK,
			Code:   "reachable",
			Label:  "reachable",
		}
	}

	text := strings.ToLower(err.Error())
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		dnsText := strings.ToLower(dnsErr.Err)
		if strings.Contains(dnsText, "operation not permitted") || strings.Contains(text, "operation not permitted") {
			return doctorProviderIssue{
				Status: doctorStatusWarn,
				Code:   "network_blocked",
				Label:  "network access blocked",
			}
		}
		if dnsErr.IsTimeout {
			return doctorProviderIssue{
				Status: doctorStatusWarn,
				Code:   "timeout",
				Label:  "timed out",
			}
		}
		return doctorProviderIssue{
			Status: doctorStatusWarn,
			Code:   "dns_unreachable",
			Label:  "DNS lookup failed",
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return doctorProviderIssue{
			Status: doctorStatusWarn,
			Code:   "timeout",
			Label:  "timed out",
		}
	}
	switch {
	case strings.Contains(text, "unauthorized"),
		strings.Contains(text, "status 401"),
		strings.Contains(text, ": 401"):
		return doctorProviderIssue{
			Status: doctorStatusFail,
			Code:   "auth_failed",
			Label:  "authentication failed",
		}
	case strings.Contains(text, "forbidden"),
		strings.Contains(text, "status 403"),
		strings.Contains(text, ": 403"):
		return doctorProviderIssue{
			Status: doctorStatusFail,
			Code:   "forbidden",
			Label:  "access forbidden",
		}
	case strings.Contains(text, "not a valid"),
		strings.Contains(text, "invalid api key"),
		strings.Contains(text, "invalid key"):
		return doctorProviderIssue{
			Status: doctorStatusFail,
			Code:   "invalid_credentials",
			Label:  "credentials look invalid",
		}
	case strings.Contains(text, "deadline exceeded"),
		strings.Contains(text, "timeout"),
		strings.Contains(text, "tls handshake timeout"):
		return doctorProviderIssue{
			Status: doctorStatusWarn,
			Code:   "timeout",
			Label:  "timed out",
		}
	case strings.Contains(text, "network is unreachable"),
		strings.Contains(text, "operation not permitted"),
		strings.Contains(text, "proxyconnect"):
		return doctorProviderIssue{
			Status: doctorStatusWarn,
			Code:   "network_blocked",
			Label:  "network access blocked",
		}
	case strings.Contains(text, "no such host"),
		strings.Contains(text, "lookup "):
		return doctorProviderIssue{
			Status: doctorStatusWarn,
			Code:   "dns_unreachable",
			Label:  "DNS lookup failed",
		}
	case strings.Contains(text, "connection refused"),
		strings.Contains(text, "connection reset"),
		strings.Contains(text, "tls handshake"),
		strings.Contains(text, "x509"),
		strings.Contains(text, "eof"):
		return doctorProviderIssue{
			Status: doctorStatusWarn,
			Code:   "transport_error",
			Label:  "transport failed",
		}
	case strings.Contains(text, "failed to create request for provider"):
		return doctorProviderIssue{
			Status: doctorStatusFail,
			Code:   "request_error",
			Label:  "request setup failed",
		}
	default:
		return doctorProviderIssue{
			Status: doctorStatusFail,
			Code:   "unknown_error",
			Label:  "failed",
		}
	}
}

func trimDoctorError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	text = strings.ReplaceAll(text, "\n", " ")
	if len(text) > 180 {
		text = text[:180] + "…"
	}
	return text
}

func doctorProviderConnectivitySummary(okCount, warnCount, failCount int, categoryCounts map[string]int) string {
	parts := make([]string, 0, 4)
	if okCount > 0 {
		parts = append(parts, fmt.Sprintf("%d reachable", okCount))
	}

	type categorySummary struct {
		Code  string
		Label string
	}
	for _, category := range []categorySummary{
		{Code: "auth_failed", Label: "auth failed"},
		{Code: "forbidden", Label: "forbidden"},
		{Code: "invalid_credentials", Label: "invalid credentials"},
		{Code: "dns_unreachable", Label: "DNS issues"},
		{Code: "network_blocked", Label: "network blocked"},
		{Code: "timeout", Label: "timed out"},
		{Code: "transport_error", Label: "transport issues"},
		{Code: "request_error", Label: "request setup failures"},
		{Code: "unknown_error", Label: "other failures"},
	} {
		if count := categoryCounts[category.Code]; count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", count, category.Label))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "no provider checks ran")
	}
	switch {
	case failCount > 0:
		return "provider checks completed with failures: " + strings.Join(parts, ", ")
	case warnCount > 0:
		return "provider checks completed with warnings: " + strings.Join(parts, ", ")
	default:
		return "provider connectivity healthy: " + strings.Join(parts, ", ")
	}
}

func doctorSourceHasMarkers(content string, markers ...string) bool {
	for _, marker := range markers {
		if !strings.Contains(content, marker) {
			return false
		}
	}
	return true
}

func doctorStatusCounts(checks []doctorCheck) (ok, warn, fail, skip int) {
	for _, check := range checks {
		switch check.Status {
		case doctorStatusOK:
			ok++
		case doctorStatusWarn:
			warn++
		case doctorStatusFail:
			fail++
		case doctorStatusSkip:
			skip++
		}
	}
	return ok, warn, fail, skip
}
