package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"github.com/BitTrawler/bittrawler/internal/config"
	"github.com/BitTrawler/bittrawler/internal/settings"
	"github.com/BitTrawler/bittrawler/internal/version"
)

// inspectConfig loads the effective settings without starting the application.
// It reads the settings database read only, and falls back to the defaults when
// the database does not exist yet, so a diagnostic command never changes
// anything.
func inspectConfig(ctx context.Context, overrides config.Overrides) (config.Loaded, bool, error) {
	dataDir := config.DataDirectory(overrides)

	store, err := settings.OpenReadOnly(dataDir)
	if errors.Is(err, os.ErrNotExist) {
		loaded, loadErr := config.Load(nil, overrides)

		return loaded, false, loadErr
	}

	if err != nil {
		return config.Loaded{}, false, err
	}

	defer store.Close()

	values, err := store.All(ctx)
	if err != nil {
		return config.Loaded{}, false, err
	}

	loaded, err := config.Load(values, overrides)

	return loaded, true, err
}

// runDoctor checks an installation and reports what would stop it working. It
// creates nothing, starts nothing and closes everything it opens.
func runDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags := addCommonFlags(fs)

	if ok, code := parseFlags(fs, args); !ok {
		return code
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Printf("BitTrawler %s\n\n", version.Current().Version)

	loaded, hasDatabase, err := inspectConfig(ctx, flags.overrides())
	if err != nil {
		fmt.Printf("fail  settings: %v\n", err)

		return 1
	}

	cfg := loaded.Config
	dataDir, err := filepath.Abs(cfg.Data.Directory)
	if err != nil {
		fmt.Printf("fail  data directory: %v\n", err)

		return 1
	}

	var failures, warnings int

	report := func(state, name, detail string) {
		switch state {
		case "fail":
			failures++
		case "warn":
			warnings++
		}

		fmt.Printf("%-4s  %-22s %s\n", state, name, detail)
	}

	if hasDatabase {
		report("ok", "settings", "read from "+filepath.Join(dataDir, "settings.db"))
	} else {
		report("warn", "settings", "no settings database yet; a new installation starts in "+dataDir)
	}

	if cfg.Web.AdminUsername != "" {
		report("ok", "administrator", "account configured")
	} else {
		report("warn", "administrator", "no account yet; the setup page runs on first visit")
	}

	if info, statErr := os.Stat(dataDir); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			report("warn", "data directory", dataDir+" does not exist yet; it is created at startup")

			if err := probeWritable(filepath.Dir(dataDir)); err != nil {
				report("fail", "data directory", "cannot write to "+filepath.Dir(dataDir)+": "+err.Error())
			}
		} else {
			report("fail", "data directory", statErr.Error())
		}
	} else if !info.IsDir() {
		report("fail", "data directory", dataDir+" is not a directory")
	} else {
		if err := probeWritable(dataDir); err != nil {
			report("fail", "data directory", "not writable: "+err.Error())
		} else {
			report("ok", "data directory", dataDir)
		}

		if free, err := freeBytes(dataDir); err == nil {
			if free < 1<<30 {
				report("warn", "free space", humanBytes(free)+" available; the store grows as it crawls")
			} else {
				report("ok", "free space", humanBytes(free)+" available")
			}
		}
	}

	checkListen := func(network, address, name string) {
		var err error

		if network == "tcp" {
			var listener net.Listener

			listener, err = net.Listen("tcp", address)
			if err == nil {
				_ = listener.Close()
			}
		} else {
			var conn net.PacketConn

			conn, err = net.ListenPacket("udp", address)
			if err == nil {
				_ = conn.Close()
			}
		}

		if err != nil {
			report("fail", name, fmt.Sprintf("%s: %v", address, err))

			return
		}

		report("ok", name, address)
	}

	checkListen("tcp", cfg.Web.Address, "web address")
	checkListen("udp", cfg.DHT.ListenAddress, "dht address")

	if _, statErr := os.Stat(dataDir); statErr == nil {
		lockPath := filepath.Join(dataDir, ".lock")

		// Probing the lock creates the file when it is missing, and doctor must
		// not change the installation it inspects. A directory without a lock
		// file has never run, so there is no lock to check.
		if _, err := os.Stat(lockPath); err != nil {
			report("ok", "data directory lock", "not created yet")
		} else {
			lock := flock.New(lockPath)

			locked, lockErr := lock.TryLock()
			switch {
			case lockErr != nil:
				report("fail", "data directory lock", lockErr.Error())
			case !locked:
				report("fail", "data directory lock", "in use by another process")
			default:
				report("ok", "data directory lock", "free")
				_ = lock.Unlock()
			}
		}
	}

	if err := resolveBootstrap(ctx, cfg.Crawler.BootstrapNodes); err != nil {
		report("fail", "bootstrap nodes", err.Error())
	} else {
		report("ok", "bootstrap nodes", fmt.Sprintf("%d resolved", len(cfg.Crawler.BootstrapNodes)))
	}

	if strings.TrimSpace(cfg.TMDB.Token) != "" {
		report("ok", "tmdb", "token configured; confirmation enabled")
	} else {
		report("ok", "tmdb", "not configured; confirmation disabled")
	}

	fmt.Println()

	switch {
	case failures > 0:
		fmt.Printf("%d problem(s) found\n", failures)

		return 1
	case warnings > 0:
		fmt.Printf("ready, with %d warning(s)\n", warnings)

		return 0
	default:
		fmt.Println("ready")

		return 0
	}
}

// probeWritable creates a file in dir and removes it, to prove the process can
// write there. Only the probe file is touched.
func probeWritable(dir string) error {
	file, err := os.CreateTemp(dir, ".bittrawler-doctor-*")
	if err != nil {
		return err
	}

	name := file.Name()

	if err := file.Close(); err != nil {
		_ = os.Remove(name)

		return err
	}

	return os.Remove(name)
}

// resolveBootstrap reports an error only when no bootstrap host resolves, so
// one retired router does not make an installation look broken.
func resolveBootstrap(ctx context.Context, nodes []string) error {
	var resolved int

	for _, node := range nodes {
		host, _, err := net.SplitHostPort(node)
		if err != nil {
			continue
		}

		lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err = net.DefaultResolver.LookupHost(lookupCtx, host)
		cancel()

		if err == nil {
			resolved++
		}
	}

	if resolved == 0 {
		return errors.New("no bootstrap host resolved")
	}

	return nil
}

// runHealthcheck exits successfully when a running instance answers.
func runHealthcheck(args []string) int {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	flags := addCommonFlags(fs)
	timeout := fs.Duration("timeout", 5*time.Second, "how long to wait for a response")

	if ok, code := parseFlags(fs, args); !ok {
		return code
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout+time.Second)
	defer cancel()

	loaded, _, err := inspectConfig(ctx, flags.overrides())
	if err != nil {
		fmt.Fprintln(os.Stderr, "bittrawler: healthcheck:", err)

		return 1
	}

	address, err := healthAddress(loaded.Config.Web.Address)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bittrawler: healthcheck:", err)

		return 1
	}

	client := &http.Client{Timeout: *timeout}

	response, err := client.Get("http://" + address + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "bittrawler: not healthy:", err)

		return 1
	}

	defer response.Body.Close()

	_, _ = io.Copy(io.Discard, response.Body)

	if response.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "bittrawler: not healthy: HTTP %d\n", response.StatusCode)

		return 1
	}

	return 0
}

// healthAddress turns a listen address into an address a client can dial.
func healthAddress(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}

	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}

	return net.JoinHostPort(host, port), nil
}

// humanBytes formats a byte count with binary units.
func humanBytes(value uint64) string {
	const unit = 1024

	if value < unit {
		return fmt.Sprintf("%d B", value)
	}

	div, exponent := uint64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exponent++
	}

	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exponent])
}
