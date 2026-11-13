package config

import (
	"fmt"
	"os"
	"sort"
)

// Source says where a setting's effective value came from. The settings page
// uses it to show a value that an environment variable or a flag has pinned as
// read only.
type Source string

// Where a value can come from.
const (
	SourceDefault     Source = "default"
	SourceDatabase    Source = "database"
	SourceEnvironment Source = "environment"
	SourceFlag        Source = "flag"
)

// Loaded is the effective configuration and where each value came from.
type Loaded struct {
	Config  Config
	Sources map[string]Source
}

// Source returns where a setting's value came from.
func (l Loaded) Source(key string) Source {
	source, ok := l.Sources[key]
	if !ok {
		return SourceDefault
	}

	return source
}

// Overrides are the bootstrap flags. They exist for the values needed to start
// the process, and for recovering from a bad address without opening the
// database by hand.
type Overrides struct {
	DataDirectory string
	WebAddress    string
	DHTAddress    string
	LogLevel      string
}

// Load computes the effective settings from the defaults, the settings
// database, the environment and the bootstrap flags, in that order, and
// validates the result.
//
// values is the contents of the settings database. Settings the database does
// not know about are ignored, so a database written by a newer version does not
// stop an older binary from starting. The data directory is never read from the
// database: it is how the database is found.
func Load(values map[string]string, overrides Overrides) (Loaded, error) {
	cfg := Default()

	sources := make(map[string]Source, len(allEntries))
	for _, e := range builtEntries() {
		sources[e.Key] = SourceDefault
	}

	for _, key := range sortedKeys(values) {
		e, ok := lookup(key)
		if !ok || !e.Persist {
			continue
		}

		if err := e.set(&cfg, values[key]); err != nil {
			return Loaded{}, fmt.Errorf(
				"config: stored setting %q: %w; reset it with: bittrawler settings reset %s",
				key, err, key,
			)
		}

		sources[key] = SourceDatabase
	}

	for _, e := range builtEntries() {
		raw, ok := os.LookupEnv(EnvName(e.Key))
		if !ok {
			continue
		}

		if err := e.set(&cfg, raw); err != nil {
			return Loaded{}, fmt.Errorf("config: %s: %w", EnvName(e.Key), err)
		}

		sources[e.Key] = SourceEnvironment
	}

	if err := applyOverrides(&cfg, sources, overrides); err != nil {
		return Loaded{}, err
	}

	if err := cfg.Validate(); err != nil {
		return Loaded{}, err
	}

	return Loaded{Config: cfg, Sources: sources}, nil
}

// applyOverrides applies the bootstrap flags. The list is ordered so that more
// than one bad flag is reported consistently.
func applyOverrides(cfg *Config, sources map[string]Source, overrides Overrides) error {
	values := []struct {
		key string
		raw string
	}{
		{"data.directory", overrides.DataDirectory},
		{"web.address", overrides.WebAddress},
		{"dht.listen_address", overrides.DHTAddress},
		{"log.level", overrides.LogLevel},
	}

	for _, override := range values {
		if override.raw == "" {
			continue
		}

		e, ok := lookup(override.key)
		if !ok {
			return fmt.Errorf("config: no setting named %q", override.key)
		}

		if err := e.set(cfg, override.raw); err != nil {
			return fmt.Errorf("config: flag overriding %s: %w", override.key, err)
		}

		sources[override.key] = SourceFlag
	}

	return nil
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

// DataDirectory returns the data directory the process will use: the flag, the
// environment variable or the default. It is resolved without the settings
// database, because the database is found through it.
func DataDirectory(overrides Overrides) string {
	if overrides.DataDirectory != "" {
		return overrides.DataDirectory
	}

	if value := os.Getenv(EnvName("data.directory")); value != "" {
		return value
	}

	return Default().Data.Directory
}
