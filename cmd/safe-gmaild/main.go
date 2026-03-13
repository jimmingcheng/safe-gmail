package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"google.golang.org/api/gmail/v1"

	"github.com/jimmingcheng/safe-gmail/internal/auth"
	"github.com/jimmingcheng/safe-gmail/internal/broker"
	"github.com/jimmingcheng/safe-gmail/internal/config"
	"github.com/jimmingcheng/safe-gmail/internal/gmailapi"
	"github.com/jimmingcheng/safe-gmail/internal/policy"
	"github.com/jimmingcheng/safe-gmail/internal/service"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}

	switch args[0] {
	case "run":
		return runDaemon(args[1:])
	case "config":
		return runConfig(args[1:])
	case "auth":
		return runAuth(args[1:])
	case "service":
		return runService(args[1:])
	default:
		usage(os.Stderr)
		return 2
	}
}

func runDaemon(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := fs.String("config", "", "Path to broker config JSON")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "missing --config")
		return 2
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	srv, err := broker.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "safe-gmaild listening on %s for client uid %d\n", cfg.SocketPath, cfg.ClientUID)
	if err := srv.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runConfig(args []string) int {
	if len(args) == 0 || args[0] != "validate" {
		usage(os.Stderr)
		return 2
	}

	fs := flag.NewFlagSet("config validate", flag.ContinueOnError)
	configPath := fs.String("config", "", "Path to broker config JSON")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "missing --config")
		return 2
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if _, err := auth.LoadOAuthClient(cfg.OAuthClientPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if _, err := policy.Load(cfg.PolicyPath, cfg.AccountEmail); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintf(os.Stdout, "config valid: instance=%s socket=%s client_uid=%d\n", cfg.Instance, cfg.SocketPath, cfg.ClientUID)
	return 0
}

func runAuth(args []string) int {
	if len(args) == 0 || args[0] != "login" {
		usage(os.Stderr)
		return 2
	}

	fs := flag.NewFlagSet("auth login", flag.ContinueOnError)
	configPath := fs.String("config", "", "Path to broker config JSON")
	redirectURI := fs.String("redirect-uri", "", "Override OAuth redirect URI")
	authURL := fs.String("auth-url", "", "Paste the final redirect URL non-interactively")
	forceConsent := fs.Bool("force-consent", false, "Force consent to obtain a fresh refresh token")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "missing --config")
		return 2
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	client, err := auth.LoadOAuthClient(cfg.OAuthClientPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	store, err := auth.OpenTokenStore(cfg.AuthStore)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	flow, err := auth.NewManualFlow(client, strings.TrimSpace(*redirectURI), []string{gmail.GmailReadonlyScope}, *forceConsent)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	finalURL := strings.TrimSpace(*authURL)
	if finalURL == "" {
		fmt.Fprintln(os.Stderr, "Visit this URL to authorize the broker:")
		fmt.Fprintln(os.Stderr, flow.AuthURL())
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "After Google redirects you back, copy the full redirect URL from the browser and paste it here.")
		fmt.Fprint(os.Stderr, "Redirect URL: ")

		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		finalURL = strings.TrimSpace(line)
	}
	if finalURL == "" {
		fmt.Fprintln(os.Stderr, "missing redirect URL")
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tok, err := flow.ExchangeRedirect(ctx, finalURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	email, err := gmailapi.ProfileEmailFromToken(ctx, tok)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !strings.EqualFold(strings.TrimSpace(email), strings.TrimSpace(cfg.AccountEmail)) {
		fmt.Fprintf(os.Stderr, "authorized as %s, expected %s\n", email, cfg.AccountEmail)
		return 1
	}

	if err := store.Save(cfg.Instance, cfg.AccountEmail, tok); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Fprintf(os.Stdout, "stored broker token for %s\n", email)
	return 0
}

func runService(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}

	switch args[0] {
	case "print-systemd":
		return printService(args[1:], "systemd")
	case "print-launchd":
		return printService(args[1:], "launchd")
	default:
		usage(os.Stderr)
		return 2
	}
}

func printService(args []string, target string) int {
	fs := flag.NewFlagSet("service", flag.ContinueOnError)
	configPath := fs.String("config", "", "Path to broker config JSON")
	binaryPath := fs.String("binary", "", "Path to the safe-gmaild binary")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "missing --config")
		return 2
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	resolvedBinary := strings.TrimSpace(*binaryPath)
	if resolvedBinary == "" {
		resolvedBinary, err = os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	spec, err := service.BuildSpec(cfg, *configPath, resolvedBinary)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	switch target {
	case "systemd":
		unit, err := service.SystemdUnit(spec)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "suggested filename: %s\n", service.SystemdUnitName(cfg.Instance))
		fmt.Fprintf(os.Stderr, "suggested path: %s\n", filepath.Join("$HOME", ".config", "systemd", "user", service.SystemdUnitName(cfg.Instance)))
		_, _ = fmt.Fprint(os.Stdout, unit)
		return 0
	case "launchd":
		plist, err := service.LaunchdPlist(spec)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "suggested filename: %s\n", service.LaunchdFileName(cfg.Instance))
		fmt.Fprintf(os.Stderr, "suggested path: %s\n", filepath.Join("$HOME", "Library", "LaunchAgents", service.LaunchdFileName(cfg.Instance)))
		_, _ = fmt.Fprint(os.Stdout, plist)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unsupported service target %q\n", target)
		return 2
	}
}

func usage(w *os.File) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  safe-gmaild run --config /path/to/broker.json")
	fmt.Fprintln(w, "  safe-gmaild config validate --config /path/to/broker.json")
	fmt.Fprintln(w, "  safe-gmaild auth login --config /path/to/broker.json [--redirect-uri ...] [--auth-url ...]")
	fmt.Fprintln(w, "  safe-gmaild service print-systemd --config /path/to/broker.json [--binary /path/to/safe-gmaild]")
	fmt.Fprintln(w, "  safe-gmaild service print-launchd --config /path/to/broker.json [--binary /path/to/safe-gmaild]")
}
