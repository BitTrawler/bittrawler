// Command bittrawler crawls the BitTorrent DHT, indexes what it finds in a
// local store and serves the result to Prowlarr and the Arr applications.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/BitTrawler/bittrawler/internal/app"
	"github.com/BitTrawler/bittrawler/internal/config"
	"github.com/BitTrawler/bittrawler/internal/store"
	"github.com/BitTrawler/bittrawler/internal/version"
)

// shutdownTimeout bounds how long an orderly shutdown waits for the crawler to
// write its last batches.
const shutdownTimeout = 30 * time.Second

func main() {
	os.Exit(run(os.Args[1:]))
}

// run dispatches a command and returns the process exit code.
func run(args []string) int {
	if len(args) == 0 {
		return runServe(nil)
	}

	switch args[0] {
	case "run":
		return runServe(args[1:])
	case "doctor":
		return runDoctor(args[1:])
	case "healthcheck":
		return runHealthcheck(args[1:])
	case "version":
		return runVersion(args[1:])
	case "auth":
		return runAuth(args[1:])
	case "settings":
		return runSettings(args[1:])
	case "help", "-h", "--help":
		usage(os.Stdout)

		return 0
	default:
		fmt.Fprintf(os.Stderr, "bittrawler: unknown command %q\n\n", args[0])
		usage(os.Stderr)

		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `Usage: bittrawler <command> [flags]

Commands:
  run          crawl the DHT and serve Torznab and the web interface (default)
  doctor       check an installation without changing it
  healthcheck  exit successfully if a running instance answers
  version      print the build version
  auth reset   clear the administrator account and run setup again
  settings reset <key>
               reset a stored setting to its default

Flags accepted by run, doctor, healthcheck, auth and settings:
  -data string       data directory (default ./data)
  -http string       override web.address, for example 0.0.0.0:6684
  -dht string        override dht.listen_address
  -log-level string  override log.level
`)
}

// commonFlags are the bootstrap flags. They exist to start an instance, to
// recover from an address that cannot be reached, and to point a command at a
// data directory that is not the default.
type commonFlags struct {
	data     string
	http     string
	dht      string
	logLevel string
}

func addCommonFlags(fs *flag.FlagSet) *commonFlags {
	flags := &commonFlags{}

	fs.StringVar(&flags.data, "data", "", "data directory (default ./data)")
	fs.StringVar(&flags.http, "http", "", "override web.address")
	fs.StringVar(&flags.dht, "dht", "", "override dht.listen_address")
	fs.StringVar(&flags.logLevel, "log-level", "", "override log.level")

	return flags
}

func (f *commonFlags) overrides() config.Overrides {
	return config.Overrides{
		DataDirectory: f.data,
		WebAddress:    f.http,
		DHTAddress:    f.dht,
		LogLevel:      f.logLevel,
	}
}

// parseFlags parses a command's flags. It returns false and an exit code when
// the command should stop.
func parseFlags(fs *flag.FlagSet, args []string) (bool, int) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return false, 0
		}

		fmt.Fprintln(os.Stderr, "bittrawler:", err)

		return false, 2
	}

	return true, 0
}

// runServe runs the product.
func runServe(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	flags := addCommonFlags(fs)

	if ok, code := parseFlags(fs, args); !ok {
		return code
	}

	level := &slog.LevelVar{}
	logger := newLogger(level)
	if flags.logLevel != "" {
		level.Set(parseLogLevel(flags.logLevel))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.New(ctx, flags.overrides(), version.Current(), logger)
	if err != nil {
		logger.Error("startup failed", "error", err)

		return 1
	}

	level.Set(parseLogLevel(application.LogLevel()))

	if err := application.Start(ctx); err != nil {
		logger.Error("startup failed", "error", err)

		_ = application.Stop(context.Background())

		return 1
	}

	var runErr error

	select {
	case <-ctx.Done():
		logger.Info("shutting down")
	case err := <-application.Errors():
		logger.Error("stopping after a fatal error", "error", err)

		runErr = err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := application.Stop(shutdownCtx); err != nil {
		logger.Error("shutdown", "error", err)

		return 1
	}

	if runErr != nil {
		return 1
	}

	return 0
}

// newLogger returns the process logger: text on a terminal, JSON otherwise.
func newLogger(level *slog.LevelVar) *slog.Logger {
	options := &slog.HandlerOptions{Level: level}

	if isTerminal(os.Stderr) {
		return slog.New(slog.NewTextHandler(os.Stderr, options))
	}

	return slog.New(slog.NewJSONHandler(os.Stderr, options))
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()

	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// parseLogLevel maps a setting value to a slog level. An invalid value is
// rejected by the settings validation, so the fallback is only a safety net.
func parseLogLevel(name string) slog.Level {
	switch strings.ToLower(name) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// runVersion prints the build information.
func runVersion(args []string) int {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)

	if ok, code := parseFlags(fs, args); !ok {
		return code
	}

	info := version.Current()

	fmt.Printf("bittrawler %s\n", info.Version)
	fmt.Printf("commit: %s\n", info.Commit)
	fmt.Printf("built:  %s\n", info.Date)

	if schema, err := store.SchemaVersion(); err == nil {
		fmt.Printf("store schema: %d\n", schema)
	}

	return 0
}
