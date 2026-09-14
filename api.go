package elknitter

// Public API: compiling a pattern into a *Matcher and matching data from a
// []byte or an io.Reader. Compilation is separate from matching (design D1);
// a matcher is immutable after compilation and safe for concurrent use.

import "io"

// Matcher — a compiled pattern. Its state is immutable after Compile, so one
// matcher may be used from several goroutines and applied to data repeatedly
// without recompiling.
type Matcher struct {
	cp *compiledPattern
}

// Compile compiles the pattern. Pattern validity errors are of class ErrCompile
// (check with errors.Is); the error position is given in the message text.
func Compile(pattern string) (*Matcher, error) {
	raws, err := parseSegments(pattern)
	if err != nil {
		return nil, err
	}
	cp, err := compileSegments(raws)
	if err != nil {
		return nil, err
	}
	return &Matcher{cp: cp}, nil
}

// Match applies the compiled pattern to the data. The input is not modified;
// binary values in the result are independent copies. Mismatch errors are of
// class ErrNoMatch (*NoMatchError with the segment number and the reason).
func (m *Matcher) Match(data []byte) (map[string]any, error) {
	return matchData(m.cp, data)
}

// MatchReader reads the input to completion before matching starts, then
// applies the pattern. A read error is of class ErrInput, and the original
// reader error is reachable via errors.As; a mismatch error is ErrNoMatch.
func (m *Matcher) MatchReader(r io.Reader) (map[string]any, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, &inputError{err: err}
	}
	return matchData(m.cp, data)
}

// Match compiles the pattern and applies it to the data right away (the
// one-liner form of Compile + Matcher.Match).
func Match(pattern string, data []byte) (map[string]any, error) {
	m, err := Compile(pattern)
	if err != nil {
		return nil, err
	}
	return m.Match(data)
}

// MatchReader compiles the pattern and applies it right away to the data
// from the reader.
func MatchReader(pattern string, r io.Reader) (map[string]any, error) {
	m, err := Compile(pattern)
	if err != nil {
		return nil, err
	}
	return m.MatchReader(r)
}

// inputError — a read error of class ErrInput; the original reader error
// stays in the chain (errors.As) and is not lost.
type inputError struct {
	err error
}

func (e *inputError) Error() string { return "elknitter: input error: " + e.err.Error() }

func (e *inputError) Unwrap() error { return e.err }

func (e *inputError) Is(target error) bool { return target == ErrInput }
