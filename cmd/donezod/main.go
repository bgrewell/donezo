// Command donezod is the donezo backend: a SQLite-backed HTTP API server.
//
// Dev builds are API-only — they serve /api/* and nothing static, with
// the Vite dev server owning the UI. Release builds compiled with
// -tags embedui (the Makefile's release-build target) also embed the
// production web bundle via internal/webui and serve it from the same
// binary: a single-file deploy.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	// Embeds a copy of the IANA zone database, used only when the host has
	// none of its own. donezod resolves calendar days in a named zone, and a
	// slim container image without tzdata would otherwise fall back to UTC
	// and quietly date an evening's work on tomorrow — the failure this
	// whole path exists to prevent.
	_ "time/tzdata"

	"github.com/bgrewell/stencil"

	"github.com/bgrewell/donezo/internal/api"
	"github.com/bgrewell/donezo/internal/auth"
	"github.com/bgrewell/donezo/internal/config"
	"github.com/bgrewell/donezo/internal/llm"
	"github.com/bgrewell/donezo/internal/seed"
	"github.com/bgrewell/donezo/internal/store"
	"github.com/bgrewell/donezo/internal/webui"
)

// Populated at build time via -ldflags (see Makefile).
var (
	appVersion    = "dev"
	appBuildDate  = ""
	appCommitHash = ""
	appBranch     = ""
)

func main() {
	defaultDataDir, err := config.DefaultDataDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "donezod: %v\n", err)
		os.Exit(1)
	}

	root := &stencil.Command{
		Name:    "donezod",
		Summary: "donezo backend API server (SQLite, one database file per space).",
		Flags:   stencil.NewFlagSet(),
		Run:     run,
	}
	root.Flags.Int("port", "p", "HTTP listen port", config.DefaultPort).Env = config.EnvPort
	root.Flags.String("data-dir", "d", "Data directory for core.db and space databases", defaultDataDir).Env = config.EnvDataDir
	root.Flags.String("seed", "s", "Seed JSON file to import before serving (skipped if already seeded)", "").Env = config.EnvSeed
	root.Flags.Bool("trust-proxy", "", "Trust proxy headers: the last X-Forwarded-For hop keys rate limiting and X-Forwarded-Proto marks cookies Secure (only directly behind a reverse proxy)", false).Env = config.EnvTrustProxy
	root.Flags.String("bind", "", "Interface to listen on (default: 127.0.0.1 when --trust-proxy is set, all interfaces otherwise)", "").Env = config.EnvBindAddress
	root.Flags.String("trusted-proxies", "", "Comma-separated CIDRs, beyond loopback, whose X-Forwarded-* headers are trusted (only needed when the proxy is on a different host)", "").Env = config.EnvTrustedProxyCIDRs
	root.Flags.Bool("hide-version", "", "Do not report the running version to the web UI (it is shown in the nav rail otherwise)", false).Env = config.EnvHideVersion
	root.Flags.String("timezone", "", "IANA zone for calendar days when a user has no timezone of their own, e.g. America/Los_Angeles (default: the host's zone)", "").Env = config.EnvTimezone
	root.Flags.Int("trash-retention-days", "", "Days a deleted item stays restorable before it is purged for good (0 disables the sweep)", 30).Env = config.EnvTrashRetentionDays
	root.Flags.String("llm-provider", "", "Optional language-model provider: anthropic or openai-compatible (empty leaves model features off)", "").Env = config.EnvLLMProvider
	root.Flags.String("llm-base-url", "", "Language-model endpoint (required for openai-compatible, e.g. http://localhost:11434/v1)", "").Env = config.EnvLLMBaseURL
	root.Flags.String("llm-model", "", "Language model to call", "").Env = config.EnvLLMModel
	root.Flags.String("smtp-host", "", "SMTP relay for delivering reminders by email (empty leaves email delivery off)", "").Env = config.EnvSMTPHost
	root.Flags.Int("smtp-port", "", "SMTP relay port", config.DefaultSMTPPort).Env = config.EnvSMTPPort
	root.Flags.String("smtp-username", "", "SMTP username (leave empty for an unauthenticated relay; the password is "+config.EnvSMTPPassword+")", "").Env = config.EnvSMTPUsername
	root.Flags.String("smtp-from", "", "Address reminders are sent from, e.g. donezo@example.com", "").Env = config.EnvSMTPFrom
	root.Flags.String("smtp-from-name", "", "Display name shown beside the sending address", config.DefaultSMTPFromName).Env = config.EnvSMTPFromName
	root.Flags.String("smtp-security", "", "How to protect the relay connection: starttls (587), tls (465) or none (a local relay only)", config.DefaultSMTPSecurity).Env = config.EnvSMTPSecurity
	root.Flags.String("twilio-account-sid", "", "Twilio account SID for delivering reminders by SMS (empty leaves SMS delivery off; the token is "+config.EnvTwilioAuthToken+")", "").Env = config.EnvTwilioAccountSID
	root.Flags.String("twilio-from", "", "Twilio sending number in E.164, or a messaging service SID", "").Env = config.EnvTwilioFrom
	root.Flags.String("operator-name", "", "Who runs this instance, named on the published privacy policy and terms, e.g. \"Grewell Tech\" (empty leaves those pages unpublished)", "").Env = config.EnvOperatorName
	root.Flags.String("support-email", "", "Support address shown on the published terms; required by carriers for an SMS program", "").Env = config.EnvSupportEmail
	root.Flags.String("public-url", "", "Where this instance is reachable, for the link in a delivered reminder, e.g. https://donezo.example.com", "").Env = config.EnvPublicURL
	root.Flags.Int("reminder-max-lateness-hours", "", "How overdue a reminder may be and still be delivered after downtime (0 delivers however late)", config.DefaultReminderMaxLatenessHours).Env = config.EnvReminderMaxLatenessHours
	root.Flags.Bool("dev-auto-login", "", "DANGEROUS: disable authentication and act as the seeded dev user (frontend dev only; requires a /tmp data dir or "+config.EnvDevAutoLoginConsent+"=1)", false).Env = config.EnvDevAutoLogin

	app := stencil.NewApp(
		stencil.WithName("donezod"),
		stencil.WithDescription("Personal work memory and attention system — backend."),
		stencil.WithVersionInfo(stencil.VersionInfo{
			Version:    appVersion,
			BuildDate:  appBuildDate,
			CommitHash: appCommitHash,
			Branch:     appBranch,
		}),
		stencil.WithRootCommand(root),
	)
	os.Exit(app.Execute(os.Args[1:]))
}

// run is the root command: optionally seed, then serve until interrupted.
func run(ctx *stencil.Context) error {
	cfg := config.Config{
		Port:               ctx.Flags.Int("port"),
		DataDir:            ctx.Flags.String("data-dir"),
		SeedPath:           ctx.Flags.String("seed"),
		TrustProxy:         ctx.Flags.Bool("trust-proxy"),
		BindAddress:        ctx.Flags.String("bind"),
		TrustedProxyCIDRs:  splitCSV(ctx.Flags.String("trusted-proxies")),
		HideVersion:        ctx.Flags.Bool("hide-version"),
		Timezone:           ctx.Flags.String("timezone"),
		TrashRetentionDays: ctx.Flags.Int("trash-retention-days"),
		DevAutoLogin:       ctx.Flags.Bool("dev-auto-login"),
		LLMProvider:        ctx.Flags.String("llm-provider"),
		LLMBaseURL:         ctx.Flags.String("llm-base-url"),
		LLMModel:           ctx.Flags.String("llm-model"),
		// Environment-only: a key passed as a flag would show up in the
		// process list for every user on the host.
		LLMAPIKey: os.Getenv(config.EnvLLMAPIKey),

		SMTPHost:     ctx.Flags.String("smtp-host"),
		SMTPPort:     ctx.Flags.Int("smtp-port"),
		SMTPUsername: ctx.Flags.String("smtp-username"),
		// Environment-only, for the same reason as the model key.
		SMTPPassword: os.Getenv(config.EnvSMTPPassword),
		SMTPFrom:     ctx.Flags.String("smtp-from"),
		SMTPFromName: ctx.Flags.String("smtp-from-name"),
		SMTPSecurity: ctx.Flags.String("smtp-security"),

		TwilioAccountSID: ctx.Flags.String("twilio-account-sid"),
		// Environment-only.
		TwilioAuthToken: os.Getenv(config.EnvTwilioAuthToken),
		TwilioFrom:      ctx.Flags.String("twilio-from"),

		OperatorName:             ctx.Flags.String("operator-name"),
		SupportEmail:             ctx.Flags.String("support-email"),
		PublicURL:                ctx.Flags.String("public-url"),
		ReminderMaxLatenessHours: ctx.Flags.Int("reminder-max-lateness-hours"),
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	core, err := store.NewCoreStore(store.WithDataDir(cfg.DataDir))
	if err != nil {
		return err
	}
	defer func() {
		if cerr := core.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "donezod: close core store: %v\n", cerr)
		}
	}()
	spaces, err := store.NewSpaceStore(store.WithDataDir(cfg.DataDir))
	if err != nil {
		return err
	}
	defer func() {
		if cerr := spaces.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "donezod: close space store: %v\n", cerr)
		}
	}()

	if cfg.SeedPath != "" {
		if err := runSeed(context.Background(), cfg.SeedPath, core, spaces); err != nil {
			return err
		}
	}

	return serve(cfg, core, spaces)
}

// runSeed imports the dataset at path and prints the summary table. An
// already-seeded data dir is a logged no-op, not an error: --seed has a
// DONEZOD_SEED env fallback that may be set persistently (e.g. in a
// systemd unit), and a restart of an already-seeded deployment must still
// reach serve() instead of crash-looping.
func runSeed(ctx context.Context, path string, core *store.CoreStore, spaces *store.SpaceStore) error {
	seeded, err := seed.IsSeeded(ctx, core)
	if err != nil {
		return err
	}
	if seeded {
		fmt.Fprintf(os.Stderr, "donezod: data dir already seeded; skipping seed import of %s\n", path)
		return nil
	}
	ds, err := seed.Load(path)
	if err != nil {
		return err
	}
	res, err := seed.Import(ctx, core, spaces, ds)
	if errors.Is(err, seed.ErrAlreadySeeded) {
		fmt.Fprintf(os.Stderr, "donezod: %v; skipping seed import\n", err)
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Printf("seeded user %q, space %q\n\n", res.Username, res.SpaceID)
	fmt.Printf("  %-12s %5s\n", "entity", "count")
	fmt.Printf("  %-12s %5s\n", "------", "-----")
	total := 0
	for _, row := range res.Counts.SummaryRows() {
		fmt.Printf("  %-12s %5d\n", row.Name, row.Count)
		total += row.Count
	}
	fmt.Printf("  %-12s %5s\n", "", "-----")
	fmt.Printf("  %-12s %5d\n", "total", total)
	return nil
}

// serve runs the HTTP server until SIGINT/SIGTERM, then shuts down
// gracefully. It also runs the hourly session/rate-limiter sweep for
// the lifetime of the server.
func serve(cfg config.Config, core *store.CoreStore, spaces *store.SpaceStore) error {
	limiter := auth.NewRateLimiter()
	mcpLimiter := auth.NewRateLimiter(auth.WithLimit(120), auth.WithWindow(time.Minute))
	// Validate already proved this resolves, so the error cannot fire here.
	location, err := cfg.Location()
	if err != nil {
		return err
	}
	// Validate has already checked these parse; ignore the error here.
	trustedProxies, _ := cfg.TrustedProxyPrefixes()
	opts := []api.ServerOption{
		api.WithRateLimiter(limiter),
		api.WithMCPRateLimiter(mcpLimiter),
		api.WithTrustProxy(cfg.TrustProxy),
		api.WithTrustedProxies(trustedProxies),
		api.WithServerVersion(appVersion),
		api.WithHideVersion(cfg.HideVersion),
		api.WithLocation(location),
		api.WithTrashRetention(cfg.TrashRetention()),
		api.WithReminderMaxLateness(cfg.ReminderMaxLateness()),
		api.WithPublicURL(cfg.PublicURL),
		api.WithOperator(cfg.OperatorName, cfg.SupportEmail),
	}
	// Reminder delivery is optional in exactly the way the model is: with
	// nothing configured, reminders keep working inside the app and simply
	// go no further.
	notifiers, err := cfg.Senders()
	if err != nil {
		return err
	}
	opts = append(opts, api.WithNotifiers(notifiers))
	// The model connection is optional: an unconfigured donezo serves the
	// same app with the model-backed affordances simply absent.
	llmClient, err := llm.New(llm.Config{
		Provider: cfg.LLMProvider,
		BaseURL:  cfg.LLMBaseURL,
		Model:    cfg.LLMModel,
		APIKey:   cfg.LLMAPIKey,
	})
	if err != nil {
		return err
	}
	if _, off := llmClient.(llm.Disabled); off {
		fmt.Fprintln(os.Stderr, "donezod: no language model configured (model features off)")
	} else {
		fmt.Fprintf(os.Stderr, "donezod: language model: %s / %s\n",
			llmClient.Provider(), llmClient.Model())
	}
	opts = append(opts, api.WithLLM(llmClient))

	// Prompt wording is taste-dependent, so it is tunable without a rebuild:
	// the data directory gets a .default.txt reference for each prompt and
	// reads back "<id>.txt" as an override. A directory that cannot be read
	// is not fatal — the built-in prompts still work.
	promptDir := filepath.Join(cfg.DataDir, llm.PromptDirName)
	prompts, err := llm.LoadPrompts(promptDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "donezod: prompt overrides: %v\n", err)
	}
	if ids := prompts.Overridden(); len(ids) > 0 {
		fmt.Fprintf(os.Stderr, "donezod: prompt overrides in effect from %s: %s\n",
			promptDir, strings.Join(ids, ", "))
	}
	opts = append(opts, api.WithPrompts(prompts))

	if cfg.PublishesPolicies() {
		fmt.Fprintf(os.Stderr, "donezod: publishing /privacy and /terms for %s\n", cfg.OperatorName)
	} else {
		fmt.Fprintf(os.Stderr, "donezod: /privacy and /terms are not published (set %s and %s)\n",
			config.EnvOperatorName, config.EnvSupportEmail)
	}
	if webui.Available() {
		fmt.Fprintln(os.Stderr, "donezod: serving embedded web UI")
		opts = append(opts, api.WithWebUI(webui.FS()))
	} else {
		fmt.Fprintln(os.Stderr, "donezod: API-only build (no embedui tag)")
	}
	if cfg.DevAutoLogin {
		// config.Validate already gated this on a /tmp data dir or the
		// explicit consent env var; still make it impossible to miss.
		fmt.Fprintln(os.Stderr, "donezod: WARNING: --dev-auto-login is set: authentication is DISABLED and every request acts as the seeded dev user. Never expose this instance beyond localhost.")
		// The static identity must match a real users row: user_id foreign
		// keys (e.g. the spaces registry) are enforced, so acting as a user
		// that does not exist would turn every such write into a 500. On an
		// unseeded data dir this creates the dev user (without a password).
		devUser, err := seed.EnsureDevUser(context.Background(), core)
		if err != nil {
			return err
		}
		opts = append(opts, api.WithAuthenticator(api.StaticAuthenticator{User: devUser}))
	}
	server := api.NewServer(core, spaces, opts...)

	sweepCtx, stopSweep := context.WithCancel(context.Background())
	sweeper := auth.NewSweeper(core,
		auth.WithSweepLimiter(limiter),
		auth.WithSweepLimiter(mcpLimiter),
		auth.WithSweepLogger(log.New(os.Stderr, "donezod ", log.LstdFlags)),
	)
	sweepDone := make(chan struct{})
	go func() {
		defer close(sweepDone)
		sweeper.Run(sweepCtx)
	}()
	// The trash retention sweep, on the same lifecycle for the same reason.
	trashSweepDone := make(chan struct{})
	go func() {
		defer close(trashSweepDone)
		server.RunTrashSweep(sweepCtx)
	}()
	// Reminder delivery, likewise: it writes notified_at back into space
	// databases, so it must be stopped and waited for before they close.
	dispatchDone := make(chan struct{})
	go func() {
		defer close(dispatchDone)
		server.RunReminderDispatch(sweepCtx)
	}()
	// Cancel the sweeper AND wait for it to exit before serve returns:
	// run()'s deferred store Close() calls fire right after, and an
	// in-flight sweep must not race them.
	defer func() {
		stopSweep()
		<-sweepDone
		<-trashSweepDone
		<-dispatchDone
	}()

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr(),
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// Bound whole-request reads and writes so slow clients cannot pin
		// connections open indefinitely; generous for an API this small.
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		fmt.Fprintf(os.Stderr, "donezod listening on http://localhost:%d (data dir %s)\n", cfg.Port, cfg.DataDir)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case sig := <-sigCh:
		fmt.Fprintf(os.Stderr, "donezod: received %s, shutting down\n", sig)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return <-errCh
	}
}

// splitCSV splits a comma-separated flag value into trimmed, non-empty
// entries. An empty input yields nil, which the config treats as "unset".
func splitCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
