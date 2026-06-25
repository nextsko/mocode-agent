package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/package-register/mocode/internal/web"
	"github.com/spf13/cobra"
)

var (
	webPort int
	webHost string
)

func init() {
	webCmd.Flags().IntVarP(&webPort, "port", "p", 5494, "Web server port")
	webCmd.Flags().StringVarP(&webHost, "host", "H", "127.0.0.1", "Web server host")
	rootCmd.AddCommand(webCmd)
}

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Start the web UI server",
	RunE: func(cmd *cobra.Command, _ []string) error {
		ws, cleanup, err := setupWorkspace(cmd)
		if err != nil {
			return fmt.Errorf("failed to setup workspace: %v", err)
		}
		defer cleanup()

		server := web.New(ws)
		url, err := server.Start(cmd.Context(), webHost, webPort)
		if err != nil {
			return fmt.Errorf("failed to start web server: %v", err)
		}

		printWebBanner(url, webHost)
		slog.Info("Web UI server started", "url", url)

		sigch := make(chan os.Signal, 1)
		signal.Notify(sigch, os.Interrupt)
		select {
		case <-sigch:
			slog.Info("Shutting down...")
		case <-cmd.Context().Done():
		}

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		if err := server.Stop(shutdownCtx); err != nil {
			slog.Error("Failed to shutdown web server", "error", err)
		}

		return nil
	},
}

func printWebBanner(boundURL, host string) {
	stderr := os.Stderr
	_, _ = fmt.Fprintln(stderr)
	_, _ = fmt.Fprintln(stderr, "  ██╗    ██╗███████╗██████╗")
	_, _ = fmt.Fprintln(stderr, "  ██║    ██║██╔════╝██╔══██╗")
	_, _ = fmt.Fprintln(stderr, "  ██║ █╗ ██║█████╗  ██████╔╝")
	_, _ = fmt.Fprintln(stderr, "  ██║███╗██║██╔══╝  ██╔══██╗")
	_, _ = fmt.Fprintln(stderr, "  ╚███╔███╔╝███████╗██████╔╝")
	_, _ = fmt.Fprintln(stderr, "   ╚══╝╚══╝ ╚══════╝╚═════╝ ")
	_, _ = fmt.Fprintln(stderr)
	_, _ = fmt.Fprintln(stderr, "  MOCODE Web UI")
	_, _ = fmt.Fprintln(stderr)
	_, _ = fmt.Fprintf(stderr, "  ➜  Bound:   %s\n", boundURL)
	for _, addr := range discoverWebAddresses(boundURL, host) {
		_, _ = fmt.Fprintf(stderr, "  ➜  Access:  %s\n", addr)
	}
	_, _ = fmt.Fprintln(stderr)
}

func discoverWebAddresses(boundURL, host string) []string {
	_, port, err := net.SplitHostPort(strings.TrimPrefix(boundURL, "http://"))
	if err != nil {
		return nil
	}

	results := []string{fmt.Sprintf("http://127.0.0.1:%s", port)}
	if host != "0.0.0.0" {
		results = append(results, fmt.Sprintf("http://%s:%s", host, port))
		return dedupeStrings(results)
	}

	ifaces, err := net.InterfaceAddrs()
	if err != nil {
		return dedupeStrings(results)
	}
	for _, addr := range ifaces {
		ipnet, ok := addr.(*net.IPNet)
		if !ok || ipnet.IP == nil || ipnet.IP.IsLoopback() {
			continue
		}
		ip := ipnet.IP.To4()
		if ip == nil {
			continue
		}
		results = append(results, fmt.Sprintf("http://%s:%s", ip.String(), port))
	}
	return dedupeStrings(results)
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
