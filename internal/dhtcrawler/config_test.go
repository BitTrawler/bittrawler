package dhtcrawler

import (
	"reflect"
	"testing"
	"time"
)

// defaultedField names one Config field with a documented default. The getter
// returns the field as an any so one table covers fields of different types.
type defaultedField struct {
	name string
	get  func(Config) any
	set  func(*Config)
}

// defaultedFields lists every field withDefaults fills, together with an
// explicit value that is not the default. SampleInfoHashesOverride is absent:
// zero is meaningful there and is covered separately.
func defaultedFields() []defaultedField {
	return []defaultedField{
		{
			name: "BootstrapNodes",
			get:  func(c Config) any { return c.BootstrapNodes },
			set:  func(c *Config) { c.BootstrapNodes = []string{"example.invalid:6881"} },
		},
		{
			name: "BootstrapReseedInterval",
			get:  func(c Config) any { return c.BootstrapReseedInterval },
			set:  func(c *Config) { c.BootstrapReseedInterval = 3 * time.Minute },
		},
		{
			name: "OldNodeAge",
			get:  func(c Config) any { return c.OldNodeAge },
			set:  func(c *Config) { c.OldNodeAge = 90 * time.Minute },
		},
		{
			name: "OldNodeCheckInterval",
			get:  func(c Config) any { return c.OldNodeCheckInterval },
			set:  func(c *Config) { c.OldNodeCheckInterval = 30 * time.Second },
		},
		{
			name: "SeenHashesSize",
			get:  func(c Config) any { return c.SeenHashesSize },
			set:  func(c *Config) { c.SeenHashesSize = 1_000 },
		},
		{
			name: "SeenHashesFalsePositiveRate",
			get:  func(c Config) any { return c.SeenHashesFalsePositiveRate },
			set:  func(c *Config) { c.SeenHashesFalsePositiveRate = 0.05 },
		},
		{
			name: "TriageBatchSize",
			get:  func(c Config) any { return c.TriageBatchSize },
			set:  func(c *Config) { c.TriageBatchSize = 42 },
		},
		{
			name: "TriageBatchWait",
			get:  func(c Config) any { return c.TriageBatchWait },
			set:  func(c *Config) { c.TriageBatchWait = 2 * time.Second },
		},
		{
			name: "PersistBatchSize",
			get:  func(c Config) any { return c.PersistBatchSize },
			set:  func(c *Config) { c.PersistBatchSize = 7 },
		},
		{
			name: "PersistBatchWait",
			get:  func(c Config) any { return c.PersistBatchWait },
			set:  func(c *Config) { c.PersistBatchWait = time.Second },
		},
		{
			name: "PeersPerInfoHash",
			get:  func(c Config) any { return c.PeersPerInfoHash },
			set:  func(c *Config) { c.PeersPerInfoHash = 9 },
		},
		{
			name: "MetadataTimeout",
			get:  func(c Config) any { return c.MetadataTimeout },
			set:  func(c *Config) { c.MetadataTimeout = time.Minute },
		},
		{
			name: "TMDBWorkers",
			get:  func(c Config) any { return c.TMDBWorkers },
			set:  func(c *Config) { c.TMDBWorkers = 2 },
		},
		{
			name: "TMDBTimeout",
			get:  func(c Config) any { return c.TMDBTimeout },
			set:  func(c *Config) { c.TMDBTimeout = 9 * time.Second },
		},
		{
			name: "Workers.Ping",
			get:  func(c Config) any { return c.Workers.Ping },
			set:  func(c *Config) { c.Workers.Ping = 1 },
		},
		{
			name: "Workers.FindNode",
			get:  func(c Config) any { return c.Workers.FindNode },
			set:  func(c *Config) { c.Workers.FindNode = 1 },
		},
		{
			name: "Workers.SampleInfoHashes",
			get:  func(c Config) any { return c.Workers.SampleInfoHashes },
			set:  func(c *Config) { c.Workers.SampleInfoHashes = 1 },
		},
		{
			name: "Workers.GetPeers",
			get:  func(c Config) any { return c.Workers.GetPeers },
			set:  func(c *Config) { c.Workers.GetPeers = 1 },
		},
		{
			name: "Workers.Metadata",
			get:  func(c Config) any { return c.Workers.Metadata },
			set:  func(c *Config) { c.Workers.Metadata = 1 },
		},
	}
}

// TestWithDefaultsFillsUnsetFields checks that a zero Config becomes the
// documented defaults, field by field, and that an explicit value is not
// replaced. SampleInfoHashesOverride is the documented exception and is checked
// in TestWithDefaultsLeavesSampleInfoHashesOverrideAtZero.
func TestWithDefaultsFillsUnsetFields(t *testing.T) {
	def := DefaultConfig()

	zeroed := Config{}.withDefaults()

	want := def
	want.SampleInfoHashesOverride = 0

	if !reflect.DeepEqual(zeroed, want) {
		t.Fatalf("withDefaults on the zero config:\n got %+v\nwant %+v", zeroed, want)
	}

	for _, field := range defaultedFields() {
		t.Run(field.name, func(t *testing.T) {
			if got, want := field.get(zeroed), field.get(def); !reflect.DeepEqual(got, want) {
				t.Fatalf("%s = %v, want the default %v", field.name, got, want)
			}

			// An explicit value survives, whatever its zero-ness.
			explicit := Config{}
			field.set(&explicit)

			got := field.get(explicit.withDefaults())
			if want := field.get(explicit); !reflect.DeepEqual(got, want) {
				t.Fatalf("%s = %v, want the explicit value %v", field.name, got, want)
			}
		})
	}
}

// TestWithDefaultsLeavesSampleInfoHashesOverrideAtZero covers the one field that
// is not defaulted: zero means "honour the node's requested interval".
func TestWithDefaultsLeavesSampleInfoHashesOverrideAtZero(t *testing.T) {
	if got := (Config{}).withDefaults().SampleInfoHashesOverride; got != 0 {
		t.Fatalf("SampleInfoHashesOverride = %v, want zero left alone", got)
	}

	explicit := 2 * time.Minute

	if got := (Config{SampleInfoHashesOverride: explicit}).withDefaults().SampleInfoHashesOverride; got != explicit {
		t.Fatalf("SampleInfoHashesOverride = %v, want the explicit value %v", got, explicit)
	}

	// Callers that start from DefaultConfig do get a non-zero starting point.
	if got := DefaultConfig().SampleInfoHashesOverride; got != time.Minute {
		t.Fatalf("DefaultConfig().SampleInfoHashesOverride = %v, want %v", got, time.Minute)
	}
}

// TestWithDefaultsRejectsInvalidFalsePositiveRate checks that a rate outside
// (0, 1) is replaced by the default: there is no valid zero or one.
func TestWithDefaultsRejectsInvalidFalsePositiveRate(t *testing.T) {
	def := DefaultConfig().SeenHashesFalsePositiveRate

	tests := []struct {
		name string
		rate float64
		want float64
	}{
		{"negative", -0.25, def},
		{"one", 1, def},
		{"above one", 1.5, def},
		{"valid", 0.01, 0.01},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := (Config{SeenHashesFalsePositiveRate: test.rate}).withDefaults().SeenHashesFalsePositiveRate
			if got != test.want {
				t.Fatalf("SeenHashesFalsePositiveRate = %v, want %v", got, test.want)
			}
		})
	}
}

// TestWithDefaultsWorkerCounts covers a partly set WorkerCounts: the unset
// stages take their defaults and the set one is kept.
func TestWithDefaultsWorkerCounts(t *testing.T) {
	def := DefaultConfig().Workers

	got := (Config{Workers: WorkerCounts{Ping: 1}}).withDefaults().Workers
	want := WorkerCounts{
		Ping:             1,
		FindNode:         def.FindNode,
		SampleInfoHashes: def.SampleInfoHashes,
		GetPeers:         def.GetPeers,
		Metadata:         def.Metadata,
	}

	if got != want {
		t.Fatalf("Workers = %+v, want %+v", got, want)
	}

	// Every stage set explicitly is left alone.
	explicit := WorkerCounts{Ping: 2, FindNode: 3, SampleInfoHashes: 4, GetPeers: 5, Metadata: 6}

	if got := (Config{Workers: explicit}).withDefaults().Workers; got != explicit {
		t.Fatalf("Workers = %+v, want %+v", got, explicit)
	}
}

// TestWithDefaultsTreatsEmptyBootstrapListAsExplicit documents the nil check:
// an explicitly empty, non-nil list is not replaced by the defaults.
func TestWithDefaultsTreatsEmptyBootstrapListAsExplicit(t *testing.T) {
	got := (Config{BootstrapNodes: []string{}}).withDefaults().BootstrapNodes
	if got == nil {
		t.Fatal("an empty bootstrap list was replaced by the defaults")
	}

	if len(got) != 0 {
		t.Fatalf("BootstrapNodes = %v, want the empty list left alone", got)
	}
}
