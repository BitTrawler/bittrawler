package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BitTrawler/bittrawler/internal/config"
	"github.com/BitTrawler/bittrawler/internal/settings"
)

// runAuth handles the administrator account commands.
func runAuth(args []string) int {
	if len(args) == 0 || args[0] != "reset" {
		fmt.Fprintln(os.Stderr, "usage: bittrawler auth reset [flags]")
		fmt.Fprintln(os.Stderr, "It clears the administrator account so the setup page runs again.")

		return 2
	}

	fs := flag.NewFlagSet("auth reset", flag.ContinueOnError)
	flags := addCommonFlags(fs)

	if ok, code := parseFlags(fs, args[1:]); !ok {
		return code
	}

	dataDir := config.DataDirectory(flags.overrides())

	store, err := openSettings(dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bittrawler:", err)

		return 1
	}

	defer store.Close()

	ctx := context.Background()

	for _, key := range []string{"web.admin_username", "web.admin_password_hash", "web.session_secret"} {
		if err := store.Delete(ctx, key); err != nil {
			fmt.Fprintln(os.Stderr, "bittrawler:", err)

			return 1
		}
	}

	fmt.Println("Administrator account cleared.")
	fmt.Println("Restart bittrawler, then open the web interface to run setup again.")

	return 0
}

// runSettings handles the stored settings commands.
func runSettings(args []string) int {
	if len(args) == 0 || args[0] != "reset" {
		fmt.Fprintln(os.Stderr, "usage: bittrawler settings reset [flags] <setting>")
		fmt.Fprintln(os.Stderr, "It deletes a stored value so the setting falls back to its default.")

		return 2
	}

	fs := flag.NewFlagSet("settings reset", flag.ContinueOnError)
	flags := addCommonFlags(fs)

	if ok, code := parseFlags(fs, args[1:]); !ok {
		return code
	}

	key := fs.Arg(0)
	if key == "" {
		fmt.Fprintln(os.Stderr, "bittrawler: name the setting to reset, for example crawler.workers.metadata")

		return 2
	}

	setting, ok := config.Lookup(key)
	if !ok {
		fmt.Fprintf(os.Stderr, "bittrawler: unknown setting %q\n", key)

		return 1
	}

	if !setting.Persist {
		fmt.Fprintf(os.Stderr, "bittrawler: %q is not stored in the settings database\n", key)

		return 1
	}

	dataDir := config.DataDirectory(flags.overrides())

	store, err := openSettings(dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bittrawler:", err)

		return 1
	}

	defer store.Close()

	if err := store.Delete(context.Background(), key); err != nil {
		fmt.Fprintln(os.Stderr, "bittrawler:", err)

		return 1
	}

	fmt.Printf("Reset %s to its default.\n", key)
	fmt.Println("Restart bittrawler for the change to apply.")

	return 0
}

// openSettings opens the settings database of an installation that has run at
// least once.
func openSettings(dataDir string) (*settings.Store, error) {
	path := filepath.Join(dataDir, "settings.db")

	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("no settings database in %s: %w", dataDir, err)
	}

	return settings.Open(dataDir)
}
