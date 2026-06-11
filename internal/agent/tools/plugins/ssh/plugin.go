// Package ssh implements the agent-facing SSH tool set.
//
// The package exposes four tools plus shared connection plumbing:
//
//	┌────────────────┬───────────────────────────────────────────┐
//	│ Tool           │ What it does                                │
//	├────────────────┼───────────────────────────────────────────┤
//	│ ssh_exec       │ Run a command on a remote host              │
//	│ ssh_upload     │ scp local → remote                          │
//	│ ssh_download   │ scp remote → local                          │
//	│ ssh_list_hosts │ List ~/.ssh/config entries                  │
//	└────────────────┴───────────────────────────────────────────┘
//
// The package does NOT define the tools.ToolPlugin implementation
// itself: that lives in internal/agent/tools/registry.go next to the
// other plugins, so this package stays free of upward imports into
// the tools tree.
//
// To wire the plugin into the registry, add a small `sshPlugin{}` struct
// to standardPlugins() in registry.go that calls:
//
//	ssh.NewSshExecTool(svc, perms),
//	ssh.NewSshUploadTool(svc, perms),
//	ssh.NewSshDownloadTool(svc, perms),
//	ssh.NewSshListHostsTool(svc),
//
// where `svc` is a single *ssh.Service instance (typically a package-level
// var owned by the plugin struct, closed on Stop()).
package ssh

// Tool names — re-exported by internal/agent/tools/compat.go so callers
// can refer to them as tools.SshExecToolName etc.
const (
	SshExecToolName      = "ssh_exec"
	SshUploadToolName    = "ssh_upload"
	SshDownloadToolName  = "ssh_download"
	SshListHostsToolName = "ssh_list_hosts"
)
