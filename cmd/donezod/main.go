// Command donezod is the donezo backend: a SQLite-backed HTTP API server.
//
// Phase 1 is API-only — it serves /api/* and nothing static.
// TODO(phase 3): go:embed web/dist here and serve the frontend from the
// same binary (single-file deploy); until then the Vite dev server owns
// the UI.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bgrewell/stencil"

	"github.com/bgrewell/donezo/internal/api"
	"github.com/bgrewell/donezo/internal/config"
	"github.com/bgrewell/donezo/internal/seed"
	"github.com/bgrewell/donezo/internal/store"
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
		Port:     ctx.Flags.Int("port"),
		DataDir:  ctx.Flags.String("data-dir"),
		SeedPath: ctx.Flags.String("seed"),
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
// gracefully.
func serve(cfg config.Config, core *store.CoreStore, spaces *store.SpaceStore) error {
	server := api.NewServer(core, spaces)
	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
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
