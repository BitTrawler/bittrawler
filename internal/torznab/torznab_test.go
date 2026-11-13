package torznab

import (
	"errors"
	"strings"
	"testing"
)

func TestErrorText(t *testing.T) {
	err := Error{Code: CodeIncorrectParameter, Description: "incorrect parameter (season)"}

	if !strings.Contains(err.Error(), "201") || !strings.Contains(err.Error(), "incorrect parameter (season)") {
		t.Fatalf("Error() = %q", err.Error())
	}

	var target Error
	if !errors.As(err, &target) {
		t.Fatal("Error does not unwrap to itself")
	}
}
