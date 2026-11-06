package cmd

import "github.com/nextsko/mocode-agent/internal/integration/wechat"

// processServices holds the process-wide integrations every command shares.
//
// Creating them at the command boundary is deliberate: the wechat package owns
// an account manager's behaviour but not its lifetime, so the composition root
// decides when one exists. Commands that need it read this value instead of
// reaching into the integration package for a package-level singleton.
var processServices = struct {
	// wechat manages logged-in accounts and channels for this process.
	wechat *wechat.AccountManager
}{
	wechat: wechat.NewAccountManager(""),
}
