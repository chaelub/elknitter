package elknitter

import (
	"errors"
	"fmt"
	"io"
	"testing"
)

// spec matching-engine, "Error classes": the three classes are pairwise
// distinct via errors.Is; NoMatchError details are reached via errors.As.
func TestErrorClassesPairwiseDistinct(t *testing.T) {
	compileErr := fmt.Errorf("elknitter: %w: bad token at offset 3", ErrCompile)
	noMatch := &NoMatchError{Segment: 2, Reason: "literal 0xFE expected, got 0xFF"}
	inputErr := fmt.Errorf("elknitter: %w: %s", ErrInput, "read failed")

	for _, tc := range []struct {
		name string
		err  error
		want []bool // [compile, nomatch, input]
	}{
		{"compile", compileErr, []bool{true, false, false}},
		{"nomatch", noMatch, []bool{false, true, false}},
		{"input", inputErr, []bool{false, false, true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := errors.Is(tc.err, ErrCompile); got != tc.want[0] {
				t.Errorf("ErrCompile: Is=%v, want %v", got, tc.want[0])
			}
			if got := errors.Is(tc.err, ErrNoMatch); got != tc.want[1] {
				t.Errorf("ErrNoMatch: Is=%v, want %v", got, tc.want[1])
			}
			if got := errors.Is(tc.err, ErrInput); got != tc.want[2] {
				t.Errorf("ErrInput: Is=%v, want %v", got, tc.want[2])
			}
		})
	}
}

func TestNoMatchErrorDetailsViaAs(t *testing.T) {
	err := error(&NoMatchError{Segment: 3, Reason: "8 bits missing"})
	var nm *NoMatchError
	if !errors.As(err, &nm) {
		t.Fatal("errors.As(*NoMatchError) failed")
	}
	if nm.Segment != 3 || nm.Reason != "8 bits missing" {
		t.Errorf("got Segment=%d Reason=%q", nm.Segment, nm.Reason)
	}
	if !errors.Is(err, ErrNoMatch) {
		t.Error("NoMatchError must be ErrNoMatch via Is")
	}
}

func TestNoMatchErrorMessage(t *testing.T) {
	err := &NoMatchError{Segment: 1, Reason: "boom"}
	if got := err.Error(); got != "elknitter: no match at segment 1: boom" {
		t.Errorf("message = %q", got)
	}
	err.Segment = 0
	if got := err.Error(); got != "elknitter: no match: boom" {
		t.Errorf("message = %q", got)
	}
}

// Make sure a read error from an io.Reader wrapped into ErrInput keeps the
// original error (see spec R1 and design D8). The real API path is used so the
// wrap produced by the library itself is what gets checked.
func TestInputErrorPreservesCause(t *testing.T) {
	sentinel := io.ErrUnexpectedEOF
	m, err := Compile("<<A:8>>")
	if err != nil {
		t.Fatal(err)
	}
	_, wrapped := m.MatchReader(&errReader{err: sentinel})
	if wrapped == nil {
		t.Fatal("an input error was expected")
	}
	if !errors.Is(wrapped, ErrInput) {
		t.Error("Is(ErrInput) = false")
	}
	if !errors.Is(wrapped, sentinel) {
		t.Error("the original read error is not reachable via Is")
	}
	if errors.Is(wrapped, ErrNoMatch) || errors.Is(wrapped, ErrCompile) {
		t.Error("the input class overlaps with the other classes")
	}
}
