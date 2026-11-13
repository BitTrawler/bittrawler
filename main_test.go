package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BitTrawler/bittrawler/internal/config"
	"github.com/BitTrawler/bittrawler/internal/settings"
	"github.com/BitTrawler/bittrawler/internal/store"
	"github.com/BitTrawler/bittrawler/internal/version"
)

// captureOutput runs fn with the process stdout and stderr redirected and
// returns what it wrote. The command functions print with fmt directly, so a
// test has to intercept the files they write to. The output of these commands
// is small, well under the pipe buffer.
func captureOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()

	oldStdout, oldStderr := os.Stdout, os.Stderr

	stdoutReader, stdoutWriter, err := os.Pipe()
	require.NoError(t, err)

	stderrReader, stderrWriter, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout, os.Stderr = stdoutWriter, stderrWriter

	defer func() {
		os.Stdout, os.Stderr = oldStdout, oldStderr
	}()

	fn()

	require.NoError(t, stdoutWriter.Close())
	require.NoError(t, stderrWriter.Close())

	stdout, err := io.ReadAll(stdoutReader)
	require.NoError(t, err)

	stderr, err := io.ReadAll(stderrReader)
	require.NoError(t, err)

	require.NoError(t, stdoutReader.Close())
	require.NoError(t, stderrReader.Close())

	return string(stdout), string(stderr)
}

// fileState is one entry in a directory snapshot.
type fileState struct {
	mode  fs.FileMode
	size  int64
	hash  [sha256.Size]byte
	isDir bool
}

// snapshotDirectory records the paths, modes, sizes and contents under root, so
// a test can prove a command changed nothing. Directory sizes are filesystem
// bookkeeping and are not recorded; their contents are covered by the file
// entries below.
func snapshotDirectory(t *testing.T, root string) map[string]fileState {
	t.Helper()

	snapshot := make(map[string]fileState)

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		state := fileState{mode: info.Mode(), isDir: entry.IsDir()}
		if entry.IsDir() {
			snapshot[relative] = state

			return nil
		}

		state.size = info.Size()

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		state.hash = sha256.Sum256(data)
		snapshot[relative] = state

		return nil
	})
	require.NoError(t, err)

	return snapshot
}

// newDataDirectory returns a data directory laid out the way an instance leaves
// it after a start and a stop: a settings database and the lock file the
// process opens at startup and does not remove at shutdown.
func newDataDirectory(t *testing.T, values map[string]string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "data")

	settingsStore, err := settings.Open(dir)
	require.NoError(t, err)

	if len(values) > 0 {
		require.NoError(t, settingsStore.SetMany(context.Background(), values))
	}

	require.NoError(t, settingsStore.Close())
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".lock"), nil, 0o600))

	return dir
}

// readSettings returns the stored settings, opening the database read only the
// way the recovery commands do.
func readSettings(t *testing.T, dir string) map[string]string {
	t.Helper()

	settingsStore, err := settings.OpenReadOnly(dir)
	require.NoError(t, err)

	defer func() { require.NoError(t, settingsStore.Close()) }()

	values, err := settingsStore.All(context.Background())
	require.NoError(t, err)

	return values
}

// loadEffectiveSettings loads the effective configuration from a data
// directory the way the commands do.
func loadEffectiveSettings(t *testing.T, dir string) config.Loaded {
	t.Helper()

	loaded, err := config.Load(readSettings(t, dir), config.Overrides{DataDirectory: dir})
	require.NoError(t, err)

	return loaded
}

// clearSettingEnvironment removes any BITTRAWLER_* override for the duration of
// a test, so a developer's shell cannot change what a command reads. t.Setenv
// first records the original value, which the test framework restores at the
// end; the extra Unsetenv then removes it while the test runs.
func clearSettingEnvironment(t *testing.T) {
	t.Helper()

	for _, setting := range config.Settings() {
		name := config.EnvName(setting.Key)

		if _, ok := os.LookupEnv(name); !ok {
			continue
		}

		t.Setenv(name, "")
		require.NoError(t, os.Unsetenv(name))
	}
}

func TestVersionCommand(t *testing.T) {
	build := version.Info{Version: "1.2.3", Commit: "0123abc", Date: "2026-11-13T11:00:00Z"}
	previous := version.Current()

	version.Version, version.Commit, version.Date = build.Version, build.Commit, build.Date

	t.Cleanup(func() {
		version.Version, version.Commit, version.Date = previous.Version, previous.Commit, previous.Date
	})

	schema, err := store.SchemaVersion()
	require.NoError(t, err)

	// The version command must not need a data directory, so run it from an
	// empty directory: anything it created would be visible there.
	working := t.TempDir()
	t.Chdir(working)

	var code int

	stdout, stderr := captureOutput(t, func() {
		code = run([]string{"version"})
	})

	require.Equal(t, 0, code)
	assert.Empty(t, stderr)

	want := fmt.Sprintf(
		"bittrawler %s\ncommit: %s\nbuilt:  %s\nstore schema: %d\n",
		build.Version, build.Commit, build.Date, schema,
	)

	assert.Equal(t, want, stdout)

	entries, err := os.ReadDir(working)
	require.NoError(t, err)
	assert.Empty(t, entries, "the version command touched the working directory")
}

func TestDoctorCreatesNothing(t *testing.T) {
	clearSettingEnvironment(t)

	dir := newDataDirectory(t, map[string]string{
		// A literal address resolves without a DNS query, so the test does not
		// depend on the network or on a bootstrap host that might retire.
		"crawler.bootstrap_nodes": "127.0.0.1:6881",
	})

	before := snapshotDirectory(t, dir)
	require.NotEmpty(t, before, "the snapshot does not describe a data directory")

	var code int

	stdout, stderr := captureOutput(t, func() {
		// Port zero asks the kernel for a free port, so the listen probes cannot
		// collide with a service running on this machine.
		code = run([]string{"doctor", "-data", dir, "-http", "127.0.0.1:0", "-dht", "127.0.0.1:0"})
	})

	after := snapshotDirectory(t, dir)

	require.Equalf(t, 0, code, "doctor reported a problem: stdout=%q stderr=%q", stdout, stderr)
	assert.Contains(t, stdout, "ok    settings")
	assert.Contains(t, stdout, "ok    data directory lock")
	assert.Contains(t, stdout, "ready")
	assert.Empty(t, stderr)

	// Doctor must leave the installation byte-for-byte as it found it. One
	// exception is tolerated: opening a WAL-mode settings database read-only
	// makes SQLite create the -wal and -shm companions beside it. That is a side
	// effect of the SQLite open rather than a write by doctor, but it does mean
	// the strict "creates nothing" promise does not hold while the settings
	// database is in WAL mode. These are the only two additions allowed:
	// everything present before must be untouched and nothing else may appear.
	for path, beforeState := range before {
		afterState, ok := after[path]
		if !assert.Truef(t, ok, "doctor removed %s", path) {
			continue
		}

		assert.Equalf(t, beforeState, afterState, "doctor changed %s", path)
	}

	for path := range after {
		if _, ok := before[path]; ok {
			continue
		}

		assert.Truef(t, isSQLiteWALCompanion(path), "doctor created %s", path)
	}
}

// isSQLiteWALCompanion reports whether path, relative to the data directory, is
// one of the journal files SQLite creates beside a WAL-mode database.
func isSQLiteWALCompanion(path string) bool {
	return path == "settings.db-wal" || path == "settings.db-shm"
}

func TestSettingsResetRestoresTheDefault(t *testing.T) {
	clearSettingEnvironment(t)

	// log.level is stored in the settings database and has a default of info.
	dir := newDataDirectory(t, map[string]string{"log.level": "debug"})

	before := loadEffectiveSettings(t, dir)
	assert.Equal(t, "debug", before.Config.Log.Level)
	assert.Equal(t, config.SourceDatabase, before.Source("log.level"))

	var code int

	stdout, stderr := captureOutput(t, func() {
		code = run([]string{"settings", "reset", "-data", dir, "log.level"})
	})

	require.Equal(t, 0, code)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "Reset log.level to its default.")

	// The stored value is gone.
	values := readSettings(t, dir)
	assert.NotContains(t, values, "log.level")

	// The effective settings fall back to the default.
	after := loadEffectiveSettings(t, dir)
	assert.Equal(t, config.Default().Log.Level, after.Config.Log.Level)
	assert.Equal(t, config.SourceDefault, after.Source("log.level"))
}

func TestAuthResetClearsTheAdministrator(t *testing.T) {
	clearSettingEnvironment(t)

	dir := newDataDirectory(t, map[string]string{
		"web.admin_username":      "admin",
		"web.admin_password_hash": "$2a$12$exampleexampleexampleexample",
		"web.session_secret":      "session-secret",
		"torznab.api_key":         "keep-me",
	})

	before := loadEffectiveSettings(t, dir)
	assert.Equal(t, "admin", before.Config.Web.AdminUsername)
	assert.Equal(t, config.SourceDatabase, before.Source("web.admin_username"))

	var code int

	stdout, stderr := captureOutput(t, func() {
		code = run([]string{"auth", "reset", "-data", dir})
	})

	require.Equal(t, 0, code)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "Administrator account cleared.")

	// The account and the session key are gone.
	values := readSettings(t, dir)

	for _, key := range []string{"web.admin_username", "web.admin_password_hash", "web.session_secret"} {
		assert.NotContains(t, values, key)
	}

	// A reset clears the account, not the whole database.
	assert.Equal(t, "keep-me", values["torznab.api_key"])

	// The next start therefore serves setup: no account is configured.
	after := loadEffectiveSettings(t, dir)
	assert.Empty(t, after.Config.Web.AdminUsername)
	assert.Empty(t, after.Config.Web.AdminPasswordHash)
	assert.Empty(t, after.Config.Web.SessionSecret)
	assert.Equal(t, config.SourceDefault, after.Source("web.admin_username"))
	assert.Equal(t, config.SourceDatabase, after.Source("torznab.api_key"))
}

func TestHealthcheckReportsAnAbsentInstance(t *testing.T) {
	clearSettingEnvironment(t)

	// Reserve a port, then release it, so the address is well formed but
	// nothing answers on it.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	address := listener.Addr().String()
	require.NoError(t, listener.Close())

	// An empty data directory is a valid installation that has not started:
	// the healthcheck falls back to the defaults for everything except the
	// address it was pointed at.
	dataDir := t.TempDir()

	var code int

	stdout, stderr := captureOutput(t, func() {
		code = run([]string{"healthcheck", "-data", dataDir, "-http", address, "-timeout", "2s"})
	})

	assert.NotEqual(t, 0, code, "healthcheck passed with nothing listening")
	assert.Contains(t, stderr, "not healthy")
	assert.Empty(t, stdout)
}

// TestDoctorDoesNotCreateALockFile checks the "creates nothing" promise against
// a directory that has never been started, where the lock probe used to create
// the lock file it only meant to test.
func TestDoctorDoesNotCreateALockFile(t *testing.T) {
	clearSettingEnvironment(t)

	// A literal address resolves without a DNS query, so the test does not
	// depend on the network.
	t.Setenv("BITTRAWLER_CRAWLER_BOOTSTRAP_NODES", "127.0.0.1:6881")

	dir := t.TempDir()

	before := snapshotDirectory(t, dir)

	var code int

	stdout, stderr := captureOutput(t, func() {
		code = run([]string{"doctor", "-data", dir, "-http", "127.0.0.1:0", "-dht", "127.0.0.1:0"})
	})

	require.Equalf(t, 0, code, "doctor reported a problem: stdout=%q stderr=%q", stdout, stderr)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "not created yet")
	assert.Equal(t, before, snapshotDirectory(t, dir))
}

// TestDoctorReportsAnUnwritableDataDirectory checks that doctor notices it
// cannot write to the data directory and exits nonzero, rather than reporting a
// ready installation that cannot start.
func TestDoctorReportsAnUnwritableDataDirectory(t *testing.T) {
	clearSettingEnvironment(t)

	// A literal address resolves without a DNS query, so the test does not
	// depend on the network.
	t.Setenv("BITTRAWLER_CRAWLER_BOOTSTRAP_NODES", "127.0.0.1:6881")

	t.Run("never started", func(t *testing.T) {
		// A directory without a settings database is a valid installation that
		// has not started, so doctor reaches its writability probe.
		dir := t.TempDir()

		require.NoError(t, os.Chmod(dir, 0o500))

		// Restore a traversable mode first: t.TempDir's removal runs after this
		// cleanup, and a failure part way through must not leave a directory the
		// test framework cannot delete.
		t.Cleanup(func() { require.NoError(t, os.Chmod(dir, 0o700)) })

		var code int

		stdout, stderr := captureOutput(t, func() {
			code = run([]string{"doctor", "-data", dir, "-http", "127.0.0.1:0", "-dht", "127.0.0.1:0"})
		})

		assert.NotEqual(t, 0, code, "doctor passed an unwritable data directory")
		assert.Contains(t, stdout, "warn  settings")
		assert.Contains(t, stdout, "fail  data directory")
		assert.Contains(t, stdout, "not writable")
		assert.Contains(t, stdout, "problem(s) found")
		assert.Empty(t, stderr)
	})

	// An installation that has run has a WAL-mode settings database. SQLite
	// cannot read one of those without a writable directory, so the settings
	// probe fails before the data directory probe: doctor still reports a
	// failure and exits nonzero, but names the wrong problem
	// (defect, reported rather than fixed). The assertions stay general so a fix
	// that reaches the data directory check still passes.
	t.Run("existing installation", func(t *testing.T) {
		dir := newDataDirectory(t, nil)

		require.NoError(t, os.Chmod(dir, 0o500))
		t.Cleanup(func() { require.NoError(t, os.Chmod(dir, 0o700)) })

		var code int

		stdout, stderr := captureOutput(t, func() {
			code = run([]string{"doctor", "-data", dir, "-http", "127.0.0.1:0", "-dht", "127.0.0.1:0"})
		})

		assert.NotEqual(t, 0, code, "doctor passed an unwritable data directory")
		assert.Contains(t, stdout, "fail ")
		assert.Empty(t, stderr)
	})
}

// TestDoctorReportsABoundWebPort checks that doctor notices the configured web
// address is already in use, which would stop the instance starting.
func TestDoctorReportsABoundWebPort(t *testing.T) {
	clearSettingEnvironment(t)

	dir := newDataDirectory(t, map[string]string{
		"crawler.bootstrap_nodes": "127.0.0.1:6881",
	})

	// Hold the port open for the duration of the command, so doctor's own
	// listen probe must fail.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	t.Cleanup(func() { _ = listener.Close() })

	address := listener.Addr().String()

	var code int

	stdout, stderr := captureOutput(t, func() {
		code = run([]string{"doctor", "-data", dir, "-http", address, "-dht", "127.0.0.1:0"})
	})

	assert.NotEqual(t, 0, code, "doctor passed with the web port already bound")
	assert.Contains(t, stdout, "fail  web address")
	assert.Contains(t, stdout, address)
	assert.Contains(t, stdout, "problem(s) found")
	assert.Empty(t, stderr)
}

// TestSettingsResetRejectsAnUnknownKey checks the recovery command's argument
// checking: the key is refused before the database is opened, so nothing is
// changed.
func TestSettingsResetRejectsAnUnknownKey(t *testing.T) {
	clearSettingEnvironment(t)

	dir := newDataDirectory(t, nil)

	before := snapshotDirectory(t, dir)

	var code int

	stdout, stderr := captureOutput(t, func() {
		code = run([]string{"settings", "reset", "-data", dir, "no.such.setting"})
	})

	require.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, `unknown setting "no.such.setting"`)
	assert.Equal(t, before, snapshotDirectory(t, dir))
}

// TestAuthResetOnADirectoryWithNoAccount records what the recovery command does
// on two installations without an account: a directory that has never started
// is refused rather than turned into one, and a configured installation with no
// account is cleared harmlessly.
func TestAuthResetOnADirectoryWithNoAccount(t *testing.T) {
	clearSettingEnvironment(t)

	t.Run("never started", func(t *testing.T) {
		dir := t.TempDir()
		before := snapshotDirectory(t, dir)

		var code int

		stdout, stderr := captureOutput(t, func() {
			code = run([]string{"auth", "reset", "-data", dir})
		})

		// openSettings refuses to create the database: a recovery command must
		// not turn a directory that has never started into an installation.
		require.Equal(t, 1, code)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, "no settings database")
		assert.Contains(t, stderr, dir)
		assert.Equal(t, before, snapshotDirectory(t, dir))
	})

	t.Run("no account configured", func(t *testing.T) {
		dir := newDataDirectory(t, nil)

		var code int

		stdout, stderr := captureOutput(t, func() {
			code = run([]string{"auth", "reset", "-data", dir})
		})

		require.Equal(t, 0, code)
		assert.Empty(t, stderr)
		assert.Contains(t, stdout, "Administrator account cleared.")

		// Deleting keys that were never set leaves no settings behind.
		assert.Empty(t, readSettings(t, dir))
	})
}
