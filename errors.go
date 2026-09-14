package elknitter

import (
	"errors"
	"fmt"
)

// Error classes of the library. Check with errors.Is(err, elknitter.ErrCompile) etc.
// The classes are pairwise distinct (see spec matching-engine, "Matching error classes").
var (
	// ErrCompile — the pattern is invalid: a syntactic or static error
	// detected at compile time, before any input is read.
	ErrCompile = errors.New("elknitter: pattern compile error")
	// ErrNoMatch — the data did not match the pattern (mismatch).
	ErrNoMatch = errors.New("elknitter: pattern does not match")
	// ErrInput — reading the input data failed (io.Reader).
	ErrInput = errors.New("elknitter: input error")
)

// NoMatchError — the detailed mismatch error: the number of the segment
// (1-based) where matching stopped, and the reason.
type NoMatchError struct {
	Segment int // segment index in the pattern, 1-based; 0 — outside the segments
	Reason  string
}

func (e *NoMatchError) Error() string {
	if e.Segment > 0 {
		return fmt.Sprintf("elknitter: no match at segment %d: %s", e.Segment, e.Reason)
	}
	return "elknitter: no match: " + e.Reason
}

func (e *NoMatchError) Unwrap() error { return ErrNoMatch }
