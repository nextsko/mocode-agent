// Package main is the entry point for the mocode CLI.
package main

import (
	"log/slog"
	"net/http"
	_ "net/http/pprof" //nolint:gosec // pprof only registered; endpoint only exposed if MOCODE_PROFILE is set
	"os"

	_ "github.com/joho/godotenv/autoload"

	"github.com/nextsko/mocode-agent/internal/transport/cmd"
)

func main() {
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
