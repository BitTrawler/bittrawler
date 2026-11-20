package app

import (
	"context"
	"log/slog"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/config"
	"github.com/BitTrawler/bittrawler/internal/version"
)

// TestSavingTheDropRulesUpdatesTheRunningClassifier is the glue behind the live
// classifier: the settings form must hand the new rules to the classifier the
// crawler is already reading, not just write them to the database. This is the
// half of the bug that a restart used to be needed for.
func TestSavingTheDropRulesUpdatesTheRunningClassifier(t *testing.T) {
	t.Setenv("BITTRAWLER_CRAWLER_ENABLED", "false")
	t.Setenv("BITTRAWLER_CRAWLER_BOOTSTRAP_NODES", "")

	application, err := New(context.Background(), config.Overrides{
		DataDirectory: t.TempDir(),
		WebAddress:    "127.0.0.1:0",
		DHTAddress:    "127.0.0.1:0",
		LogLevel:      "error",
	}, version.Info{Version: "test"}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	// Stop closes the databases and the sockets the instance opened.
	t.Cleanup(func() { _ = application.Stop(context.Background()) })

	const cjkName = "寄生兽 完全版"

	if got := application.classifier.Classify(cjkName, nil).Drop; got != classify.DropNone {
		t.Fatalf("before saving: drop = %v, want %v", got, classify.DropNone)
	}

	spec, ok := settingsGroup("classification")
	require.True(t, ok)

	form := url.Values{
		"group":                 {"classification"},
		"classify.drop_adult":   {"true"},
		"classify.drop_scripts": {"cjk"},
	}

	restart, err := application.saveSettings(context.Background(), spec, form, false)
	require.NoError(t, err)
	assert.False(t, restart, "the drop rules apply without a restart")

	if got := application.classifier.Classify(cjkName, nil).Drop; got != classify.DropScript {
		t.Fatalf("after saving: drop = %v, want %v", got, classify.DropScript)
	}

	assert.True(t, application.classifier.DropAdult())
}
