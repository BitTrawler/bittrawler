package banning

import (
	"strings"
	"testing"

	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo"
)

func TestDefaultChecks(t *testing.T) {
	valid := metainfo.Info{Name: "a-valid-name", Length: 10_000}
	if err := New().Check(valid); err != nil {
		t.Fatalf("valid info rejected: %v", err)
	}

	if err := New().Check(metainfo.Info{Name: "short", Length: 10_000}); err == nil || !strings.Contains(err.Error(), "name too short") {
		t.Fatalf("short name error = %v", err)
	}

	if err := New().Check(metainfo.Info{Name: "a-valid-name", Length: 100}); err == nil || !strings.Contains(err.Error(), "size too small") {
		t.Fatalf("small size error = %v", err)
	}

	if err := New().Check(metainfo.Info{Name: "invalid-\xff-name", Length: 10_000}); err == nil || !strings.Contains(err.Error(), "invalid utf8") {
		t.Fatalf("invalid utf8 error = %v", err)
	}
}

func TestCombinedCheckReturnsAllFailures(t *testing.T) {
	err := New().Check(metainfo.Info{Name: "x", Length: 1})
	if err == nil {
		t.Fatal("invalid info accepted")
	}

	msg := err.Error()
	if !strings.Contains(msg, "name too short") || !strings.Contains(msg, "size too small") {
		t.Fatalf("error = %q, want both failures", msg)
	}
}

func TestExtraCheckers(t *testing.T) {
	rejectAll := checkerFunc(func(metainfo.Info) error {
		return errRejected
	})

	err := New(rejectAll).Check(metainfo.Info{Name: "a-valid-name", Length: 10_000})
	if err == nil {
		t.Fatal("extra checker was not applied")
	}
}

type checkerFunc func(metainfo.Info) error

func (f checkerFunc) Check(info metainfo.Info) error { return f(info) }

var errRejected = &checkerError{"rejected by test"}

type checkerError struct{ msg string }

func (e *checkerError) Error() string { return e.msg }
