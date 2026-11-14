package config_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BitTrawler/bittrawler/internal/config"
)

func TestDefaultIsValid(t *testing.T) {
	cfg := config.Default()

	require.NoError(t, cfg.Validate())
	assert.Equal(t, "./data", cfg.Data.Directory)
	assert.Equal(t, "0.0.0.0:6684", cfg.Web.Address)
	assert.Equal(t, "0.0.0.0:6684", cfg.DHT.ListenAddress)
	assert.Equal(t, 12, cfg.Store.RetentionMonths)
	assert.Equal(t, 200, cfg.Crawler.Workers.Metadata)
	assert.Equal(t, 30, cfg.Web.SessionDays)
	assert.True(t, cfg.Crawler.Enabled)
	assert.Empty(t, cfg.Web.AdminUsername)
	assert.Empty(t, cfg.Torznab.APIKey)
}

func TestLoadWithoutValuesUsesDefaults(t *testing.T) {
	loaded, err := config.Load(nil, config.Overrides{})
	require.NoError(t, err)

	assert.Equal(t, config.Default(), loaded.Config)
	assert.Equal(t, config.SourceDefault, loaded.Source("crawler.workers.metadata"))
}

func TestDatabaseValuesOverrideDefaults(t *testing.T) {
	loaded, err := config.Load(map[string]string{
		"crawler.workers.metadata": "48",
		"store.retention_months":   "3",
	}, config.Overrides{})
	require.NoError(t, err)

	assert.Equal(t, 48, loaded.Config.Crawler.Workers.Metadata)
	assert.Equal(t, 3, loaded.Config.Store.RetentionMonths)
	assert.Equal(t, config.SourceDatabase, loaded.Source("crawler.workers.metadata"))
	assert.Equal(t, config.SourceDefault, loaded.Source("crawler.workers.ping"))
}

func TestEnvironmentOverridesDatabase(t *testing.T) {
	t.Setenv("BITTRAWLER_CRAWLER_WORKERS_METADATA", "64")
	t.Setenv("BITTRAWLER_CRAWLER_BOOTSTRAP_NODES", "one.example:1, two.example:2")
	t.Setenv("BITTRAWLER_TMDB_TIMEOUT", "9s")
	t.Setenv("BITTRAWLER_CRAWLER_ENABLED", "false")

	loaded, err := config.Load(map[string]string{
		"crawler.workers.metadata": "48",
	}, config.Overrides{})
	require.NoError(t, err)

	assert.Equal(t, 64, loaded.Config.Crawler.Workers.Metadata)
	assert.Equal(t, []string{"one.example:1", "two.example:2"}, loaded.Config.Crawler.BootstrapNodes)
	assert.Equal(t, 9*time.Second, loaded.Config.TMDB.Timeout)
	assert.False(t, loaded.Config.Crawler.Enabled)
	assert.Equal(t, config.SourceEnvironment, loaded.Source("crawler.workers.metadata"))
}

func TestFlagsOverrideEverything(t *testing.T) {
	t.Setenv("BITTRAWLER_WEB_ADDRESS", "127.0.0.1:1002")
	t.Setenv("BITTRAWLER_LOG_LEVEL", "warn")

	loaded, err := config.Load(map[string]string{
		"web.address": "127.0.0.1:1001",
	}, config.Overrides{
		WebAddress: "127.0.0.1:1003",
		LogLevel:   "debug",
	})
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:1003", loaded.Config.Web.Address)
	assert.Equal(t, "debug", loaded.Config.Log.Level)
	assert.Equal(t, config.SourceFlag, loaded.Source("web.address"))
	assert.Equal(t, config.SourceFlag, loaded.Source("log.level"))
}

func TestDataDirectoryIsNeverReadFromTheDatabase(t *testing.T) {
	loaded, err := config.Load(map[string]string{
		"data.directory": "/somewhere/else",
	}, config.Overrides{})
	require.NoError(t, err)

	assert.Equal(t, "./data", loaded.Config.Data.Directory)
	assert.Equal(t, config.SourceDefault, loaded.Source("data.directory"))
}

func TestUnknownDatabaseKeysAreIgnored(t *testing.T) {
	loaded, err := config.Load(map[string]string{
		"removed.setting": "1",
	}, config.Overrides{})
	require.NoError(t, err)

	assert.Equal(t, config.Default(), loaded.Config)
}

func TestInvalidDatabaseValueNamesTheKey(t *testing.T) {
	_, err := config.Load(map[string]string{
		"crawler.workers.metadata": "many",
	}, config.Overrides{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "crawler.workers.metadata")
	assert.Contains(t, err.Error(), "bittrawler settings reset")
}

func TestInvalidEnvironmentValueNamesTheVariable(t *testing.T) {
	t.Setenv("BITTRAWLER_CRAWLER_WORKERS_METADATA", "many")

	_, err := config.Load(nil, config.Overrides{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "BITTRAWLER_CRAWLER_WORKERS_METADATA")
}

func TestSettingsRegistry(t *testing.T) {
	settings := config.Settings()
	require.NotEmpty(t, settings)

	keys := make(map[string]struct{}, len(settings))
	envNames := make(map[string]struct{}, len(settings))

	for _, setting := range settings {
		assert.NotContains(t, setting.Usage, "label:", "%s has a mangled tag", setting.Key)
		assert.NotEmpty(t, setting.Group, "%s has no group", setting.Key)

		_, duplicate := keys[setting.Key]
		assert.False(t, duplicate, "%s is declared twice", setting.Key)

		keys[setting.Key] = struct{}{}

		envName := config.EnvName(setting.Key)
		_, duplicate = envNames[envName]
		assert.False(t, duplicate, "%s is declared twice", envName)

		envNames[envName] = struct{}{}

		checkDefault(t, setting)
	}

	directory, ok := config.Lookup("data.directory")
	require.True(t, ok)
	assert.False(t, directory.Persist, "the data directory cannot live inside the database it locates")

	apiKey, ok := config.Lookup("torznab.api_key")
	require.True(t, ok)
	assert.True(t, apiKey.Secret)
	assert.True(t, apiKey.Persist)
}

// checkDefault asserts that a setting's default has the type its declaration
// promises and satisfies its own bounds.
func checkDefault(t *testing.T, setting config.Setting) {
	t.Helper()

	switch setting.Type {
	case config.TypeBool:
		assert.IsType(t, false, setting.Default, setting.Key)
	case config.TypeInt:
		assert.IsType(t, 0, setting.Default, setting.Key)
	case config.TypeFloat:
		assert.IsType(t, 0.0, setting.Default, setting.Key)
	case config.TypeString:
		assert.IsType(t, "", setting.Default, setting.Key)
	case config.TypeDuration:
		assert.IsType(t, time.Duration(0), setting.Default, setting.Key)
	case config.TypeStrings:
		assert.IsType(t, []string{}, setting.Default, setting.Key)
	default:
		t.Fatalf("%s has unknown type %q", setting.Key, setting.Type)
	}

	if setting.Type == config.TypeString && len(setting.Enum) > 0 {
		assert.Contains(t, setting.Enum, setting.Default, setting.Key)
	}

	if setting.Min != nil {
		value := reflect.ValueOf(setting.Default)
		if value.Kind() == reflect.Int {
			assert.GreaterOrEqual(t, float64(value.Int()), *setting.Min, setting.Key)
		}
	}

	if setting.Max != nil {
		value := reflect.ValueOf(setting.Default)
		if value.Kind() == reflect.Int {
			assert.LessOrEqual(t, float64(value.Int()), *setting.Max, setting.Key)
		}
	}
}

func TestValidationErrors(t *testing.T) {
	tests := []struct {
		name   string
		change func(*config.Config)
		expect string
	}{
		{"bad web address", func(c *config.Config) { c.Web.Address = "not an address" }, "web.address"},
		{"bad dht address", func(c *config.Config) { c.DHT.ListenAddress = "not an address" }, "dht.listen_address"},
		{"bad log level", func(c *config.Config) { c.Log.Level = "verbose" }, "log.level"},
		{"negative retention", func(c *config.Config) { c.Store.RetentionMonths = -1 }, "retention_months"},
		{"zero worker", func(c *config.Config) { c.Crawler.Workers.Metadata = 0 }, "workers.metadata"},
		{"zero session days", func(c *config.Config) { c.Web.SessionDays = 0 }, "session_days"},
		{"session days above the maximum", func(c *config.Config) { c.Web.SessionDays = 366 }, "session_days"},
		{"zero metadata timeout", func(c *config.Config) { c.Crawler.MetadataTimeout = 0 }, "metadata_timeout"},
		{"bad bootstrap node", func(c *config.Config) { c.Crawler.BootstrapNodes = []string{"nohost"} }, "bootstrap_nodes"},
		{"bad drop script", func(c *config.Config) { c.Classify.DropScripts = []string{"klingon"} }, "drop_scripts"},
		{"bad seen rate", func(c *config.Config) { c.Crawler.SeenHashesFalsePositiveRate = 1 }, "seen_hashes_false_positive_rate"},
		{"zero query rate", func(c *config.Config) { c.DHT.OverallQueryRate = 0 }, "overall_query_rate"},
		{"default limit above max", func(c *config.Config) { c.Torznab.DefaultLimit = 200 }, "default_limit"},
		{"empty data directory", func(c *config.Config) { c.Data.Directory = "  " }, "data.directory"},
		{"username without hash", func(c *config.Config) { c.Web.AdminUsername = "admin" }, "admin_password_hash"},
		{"hash without username", func(c *config.Config) { c.Web.AdminPasswordHash = "hash" }, "admin_username"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := config.Default()
			test.change(&cfg)

			err := cfg.Validate()

			require.Error(t, err)
			assert.Contains(t, err.Error(), test.expect)
		})
	}
}

func TestFormat(t *testing.T) {
	assert.Equal(t, "48", config.Format(48))
	assert.Equal(t, "0.5", config.Format(0.5))
	assert.Equal(t, "true", config.Format(true))
	assert.Equal(t, "45s", config.Format(45*time.Second))
	assert.Equal(t, "15m", config.Format(15*time.Minute))
	assert.Equal(t, "1h30m", config.Format(90*time.Minute))
	assert.Equal(t, "a, b", config.Format([]string{"a", "b"}))
	assert.Equal(t, "text", config.Format("text"))
}
