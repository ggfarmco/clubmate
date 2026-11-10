package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"clubmate/pkg/apns"
)

const (
	pushRate        = 10
	pushBurst       = 50
	shutdownTimeout = 10 * time.Second
)

var (
	errUsage    = errors.New("usage: clubmate-push-relay [-listen addr -key file -key-id id -team-id id -tokens file -admins file -bans file -reports dir -devices file] | token add|rm <name> -tokens file | token ls -tokens file | admin add|rm <name> -admins file | admin ls -admins file")
	errNoKey    = errors.New("-key is required")
	errNoKeyID  = errors.New("-key-id is required unless the key file is named AuthKey_<KEYID>.p8")
	errNoTeamID = errors.New("-team-id is required")
	errNoTokens = errors.New("-tokens is required")
	errNoAdmins = errors.New("-admins is required")

	macTokens = tokenFile{
		command: "token",
		flag:    "tokens",
		env:     "PUSH_RELAY_TOKENS",
		usage:   "Mac tokens file (PUSH_RELAY_TOKENS)",
		missing: errNoTokens,
	}
	adminTokens = tokenFile{
		command: "admin",
		flag:    "admins",
		env:     "PUSH_RELAY_ADMINS",
		usage:   "admin tokens file (PUSH_RELAY_ADMINS)",
		missing: errNoAdmins,
	}
)

type config struct {
	listen  string
	key     string
	keyID   string
	teamID  string
	tokens  string
	admins  string
	bans    string
	reports string
	devices string
}

type tokenFile struct {
	command string
	flag    string
	env     string
	usage   string
	missing error
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "clubmate-push-relay:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case macTokens.command:
			return tokenCommand(macTokens, args[1:], out)
		case adminTokens.command:
			return tokenCommand(adminTokens, args[1:], out)
		}
	}
	return serve(args)
}

func serve(args []string) error {
	var cfg config
	fs := flag.NewFlagSet("clubmate-push-relay", flag.ContinueOnError)
	fs.StringVar(&cfg.listen, "listen", env("PUSH_RELAY_LISTEN", ":8000"), "address to listen on (PUSH_RELAY_LISTEN)")
	fs.StringVar(&cfg.key, "key", env("PUSH_RELAY_KEY", ""), "APNs auth key AuthKey_<KEYID>.p8 (PUSH_RELAY_KEY)")
	fs.StringVar(&cfg.keyID, "key-id", env("PUSH_RELAY_KEY_ID", ""), "APNs key id, default from the key file name (PUSH_RELAY_KEY_ID)")
	fs.StringVar(&cfg.teamID, "team-id", env("PUSH_RELAY_TEAM_ID", ""), "Apple Developer team id (PUSH_RELAY_TEAM_ID)")
	fs.StringVar(&cfg.tokens, "tokens", env("PUSH_RELAY_TOKENS", ""), "Mac tokens file (PUSH_RELAY_TOKENS)")
	fs.StringVar(&cfg.admins, "admins", env("PUSH_RELAY_ADMINS", ""), "admin tokens file for the reports API (PUSH_RELAY_ADMINS)")
	fs.StringVar(&cfg.bans, "bans", env("PUSH_RELAY_BANS", ""), "banned report keys and IPs (PUSH_RELAY_BANS)")
	fs.StringVar(&cfg.reports, "reports", env("PUSH_RELAY_REPORTS", "reports"), "problem reports directory (PUSH_RELAY_REPORTS)")
	fs.StringVar(&cfg.devices, "devices", env("PUSH_RELAY_DEVICES", ""), "device tokens each Mac registered, unset keeps them in memory (PUSH_RELAY_DEVICES)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	client, err := cfg.apnsClient()
	if err != nil {
		return err
	}
	if cfg.tokens == "" {
		return errNoTokens
	}
	toks, err := loadTokens(cfg.tokens)
	if err != nil {
		return err
	}
	var admins *tokens
	if cfg.admins != "" {
		if admins, err = loadTokens(cfg.admins); err != nil {
			return err
		}
	}
	b, err := loadBans(cfg.bans)
	if err != nil {
		return err
	}
	reports, err := openStore(cfg.reports, reportQuota, anonReportQuota)
	if err != nil {
		return err
	}
	devices, err := loadBindings(cfg.devices)
	if err != nil {
		return err
	}
	logger := log.New(os.Stderr, "", 0)
	if cfg.devices == "" {
		logger.Printf("no -devices file: registered device tokens are lost on restart")
	}
	r := &relay{
		tokens:     toks,
		devices:    devices,
		apns:       client,
		limiter:    newLimiter(pushRate, pushBurst, time.Now),
		log:        logger,
		admins:     admins,
		bans:       b,
		reports:    reports,
		limits:     newReportLimits(time.Now),
		seen:       newReplayCache(),
		uploads:    make(chan struct{}, maxUploads),
		macUploads: make(chan struct{}, maxMacUploads),
		active:     newInflight(),
		grace:      minTransfer,
		now:        time.Now,
	}
	srv := &http.Server{
		Addr:              cfg.listen,
		Handler:           r.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	errc := make(chan error, 1)
	go func() {
		errc <- srv.ListenAndServe()
	}()
	go r.sweepEvery(ctx, sweepInterval)
	logger.Printf("listening on %s", cfg.listen)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func tokenCommand(file tokenFile, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	sub, rest := args[0], args[1:]
	var name string
	if sub != "ls" {
		if len(rest) == 0 || strings.HasPrefix(rest[0], "-") {
			return errUsage
		}
		name, rest = rest[0], rest[1:]
	}
	fs := flag.NewFlagSet(file.command+" "+sub, flag.ContinueOnError)
	path := fs.String(file.flag, env(file.env, ""), file.usage)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errUsage
	}
	if *path == "" {
		return file.missing
	}
	switch sub {
	case "add":
		token, err := addToken(*path, name)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, token)
		return err
	case "rm":
		return removeToken(*path, name)
	case "ls":
		names, err := tokenNames(*path)
		if err != nil {
			return err
		}
		for _, n := range names {
			if _, err := fmt.Fprintln(out, n); err != nil {
				return err
			}
		}
		return nil
	}
	return errUsage
}

func (c config) apnsClient() (*apns.Client, error) {
	if c.key == "" {
		return nil, errNoKey
	}
	raw, err := os.ReadFile(c.key)
	if err != nil {
		return nil, err
	}
	key, err := apns.ParseKey(raw)
	if err != nil {
		return nil, err
	}
	keyID := c.keyID
	if keyID == "" {
		keyID = apns.KeyIDFromPath(c.key)
	}
	if keyID == "" {
		return nil, errNoKeyID
	}
	if c.teamID == "" {
		return nil, errNoTeamID
	}
	return apns.NewClient(key, keyID, c.teamID), nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
