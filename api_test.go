package elknitter

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// Scenario «Повторное использование матчера»: one matcher, two applications.
func TestScenarioMatcherReuse(t *testing.T) {
	m, err := Compile("<<A:8, B:8>>")
	if err != nil {
		t.Fatal(err)
	}
	r1, err := m.Match([]byte{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := m.Match([]byte{3, 4})
	if err != nil {
		t.Fatal(err)
	}
	if r1["A"] != int64(1) || r1["B"] != int64(2) ||
		r2["A"] != int64(3) || r2["B"] != int64(4) {
		t.Errorf("reuse: %v / %v", r1, r2)
	}
}

// Scenario «Ошибка чтения reader-а»: the error is of class ErrInput, the original
// one is reachable via errors.As; everything is read before matching starts.
func TestScenarioReaderInputError(t *testing.T) {
	boom := errors.New("boom: disk on fire")
	m, err := Compile("<<A:8>>")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.MatchReader(&errReader{err: boom})
	if err == nil {
		t.Fatal("expected an input error")
	}
	if !errors.Is(err, ErrInput) {
		t.Errorf("not of class ErrInput: %v", err)
	}
	// the original reader error stays in the chain: errors.Is/errors.As
	// reach it via Unwrap
	if !errors.Is(err, boom) {
		t.Errorf("the original error is not reachable in the chain: %v", err)
	}
	if errors.Is(err, ErrNoMatch) || errors.Is(err, ErrCompile) {
		t.Errorf("class ErrInput overlaps with the other classes: %v", err)
	}
}

// Scenario "Input data and matcher lifecycle": the reader is consumed fully —
// data past the first chunk read also takes part in the match.
func TestScenarioReaderReadsAll(t *testing.T) {
	m, err := Compile("<<A:8, Rest/binary>>")
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.MatchReader(io.MultiReader(
		strings.NewReader("\x01"), strings.NewReader("tail"),
	))
	if err != nil {
		t.Fatal(err)
	}
	if r["A"] != int64(1) || !reflect.DeepEqual(r["Rest"], []byte("tail")) {
		t.Errorf("map = %v", r)
	}
}

// One-shot helpers: Match and MatchReader compile and match.
func TestOneShotHelpers(t *testing.T) {
	r, err := Match("<<X:16>>", []byte{0x01, 0x02})
	if err != nil {
		t.Fatal(err)
	}
	if r["X"] != int64(0x0102) {
		t.Errorf("Match: %v", r)
	}
	r, err = MatchReader("<<X:8, Y/binary>>", bytes.NewReader([]byte{5, 6, 7}))
	if err != nil {
		t.Fatal(err)
	}
	if r["X"] != int64(5) || !reflect.DeepEqual(r["Y"], []byte{6, 7}) {
		t.Errorf("MatchReader: %v", r)
	}
}

// Scenario «Классы различимы» at API level: a compile error, a mismatch
// and an input error are told apart by errors.Is and do not overlap.
func TestScenarioAPIClasses(t *testing.T) {
	_, errCompile := Compile("A:8")
	_, errMismatch := Match("<<A:8>>", nil)
	_, errInput := MatchReader("<<A:8>>", &errReader{err: io.ErrUnexpectedEOF})
	if errCompile == nil || errMismatch == nil || errInput == nil {
		t.Fatal("all three errors are needed")
	}
	cases := []struct {
		err    error
		class  error
		belong bool
	}{
		{errCompile, ErrCompile, true},
		{errCompile, ErrNoMatch, false},
		{errCompile, ErrInput, false},
		{errMismatch, ErrNoMatch, true},
		{errMismatch, ErrCompile, false},
		{errMismatch, ErrInput, false},
		{errInput, ErrInput, true},
		{errInput, ErrCompile, false},
		{errInput, ErrNoMatch, false},
	}
	for _, c := range cases {
		if got := errors.Is(c.err, c.class); got != c.belong {
			t.Errorf("errors.Is(%v, %v) = %v, want %v", c.err, c.class, got, c.belong)
		}
	}
}

// Scenario "Input data and matcher lifecycle": the []byte input is not
// modified by matching.
func TestScenarioInputNotModified(t *testing.T) {
	in := []byte{'a', 'b', 'c', 'd', 'e'}
	cp := append([]byte(nil), in...)
	m, err := Compile("<<A:8, B/binary>>")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Match(in); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, cp) {
		t.Errorf("input was modified: %q", in)
	}
}

// Scenario «Повторное использование матчера» plus thread safety (task 4.5):
// one *Matcher from
// many goroutines; run with -race.
func TestScenarioConcurrentMatcher(t *testing.T) {
	m, err := Compile("<<Len:8, Payload:Len/binary, Rest/binary>>")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("hello world")
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data := append([]byte{byte(len(payload))}, payload...)
			r, err := m.Match(data)
			if err != nil {
				errs <- err
				return
			}
			if !reflect.DeepEqual(r["Payload"], payload) {
				errs <- fmt.Errorf("Payload = %q", r["Payload"])
			}
			// and a negative case mixed in: a mismatch must not corrupt
			// the matcher state
			if _, err := m.Match([]byte{0xFF, 0x01}); err == nil {
				errs <- fmt.Errorf("expected a mismatch")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// The reference example from the Erlang documentation: length-prefix, where
// the size of Payload is set by the previously bound Len; Rest is the tail.
// This example is executed by godoc.
func ExampleMatch() {
	pattern := `<<Len:8, Payload:Len/binary, Rest/binary>>`
	data := append([]byte{3}, []byte("abctail")...)
	res, err := Match(pattern, data)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Len=%v Payload=%q Rest=%q\n", res["Len"], res["Payload"], res["Rest"])
	// Output: Len=3 Payload="abc" Rest="tail"
}

// errReader returns an error after the first read.
type errReader struct {
	err error
}

func (r *errReader) Read(p []byte) (int, error) {
	return 0, r.err
}
