// Package main is the entry point for the mocode CLI.
package main

import (
	"log"
	"log/slog"
	"net/http"
	_ "net/http/pprof" //nolint:gosec // pprof only registered; endpoint only exposed if MOCODE_PROFILE is set
	"os"

	_ "github.com/joho/godotenv/autoload"

	"github.com/nextsko/mocode-agent/internal/transport/cmd"
	"github.com/nextsko/mocode-agent/internal/util/netx"
)

func main() {
	// Install an explicit resolver before anything performs a network call.
	// Platforms without /etc/resolv.conf (Android/Termux) otherwise resolve
	// against 127.0.0.1:53 and [::1]:53.
	if netx.ConfigureDNS() {
		log.Printf("DNS resolver configured: %s", netx.Servers()[0])
	}

	if os.Getenv("MOCODE_PROFILE") != "" {
		go func() {
			slog.Info("Serving pprof at localhost:6060")
			//nolint:gosec // G108: pprof endpoint only exposed when MOCODE_PROFILE is set — intentional debug capability
			if httpErr := http.ListenAndServe("localhost:6060", nil); httpErr != nil {
				slog.Error("Failed to pprof listen", "error", httpErr)
			}
		}()
	}

	cmd.Execute()
}
