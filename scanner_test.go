package elknitter

import (
	"errors"
	"reflect"
	"testing"
)

func scanAll(src string) ([]token, error) {
	s := newScanner(src)
	var toks []token
	for {
		t, err := s.next()
		if err != nil {
			return toks, err
		}
		if t.kind == tEOF {
			return toks, nil
		}
		toks = append(toks, t)
	}
}

func TestScannerTokens(t *testing.T) {
	toks, err := scanAll("<<A, B:16, _:4/binary>>")
	if err != nil {
		t.Fatal(err)
	}
	want := []token{
		{kind: tOpen},
		{kind: tIdent, text: "A"},
		{kind: tComma},
		{kind: tIdent, text: "B"},
		{kind: tColon},
		{kind: tInt, text: "16"},
		{kind: tComma},
		{kind: tUnderscore},
		{kind: tColon},
		{kind: tInt, text: "4"},
		{kind: tSlash},
		{kind: tIdent, text: "binary"},
		{kind: tClose},
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %+v", len(toks), len(want), toks)
	}
	for i, w := range want {
		if toks[i].kind != w.kind || toks[i].text != w.text {
			t.Errorf("token %d = %v, want %v", i, toks[i], w)
		}
	}
}

func TestScannerTokenKinds(t *testing.T) {
	toks, err := scanAll("<< , : / >>")
	if err != nil {
		t.Fatal(err)
	}
	kinds := []tokenKind{tOpen, tComma, tColon, tSlash, tClose}
	if len(toks) != len(kinds) {
		t.Fatalf("got %d tokens: %+v", len(toks), toks)
	}
	for i, k := range kinds {
		if toks[i].kind != k {
			t.Errorf("token %d kind = %v, want %v", i, toks[i].kind, k)
		}
	}
}

func TestScannerIdentifiers(t *testing.T) {
	toks, err := scanAll("<<Var, Var_1, CamelCase, v>>")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, tk := range toks {
		if tk.kind == tIdent {
			names = append(names, tk.text)
		}
	}
	want := []string{"Var", "Var_1", "CamelCase", "v"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("idents = %v, want %v", names, want)
	}
}

func TestScannerNumbers(t *testing.T) {
	toks, err := scanAll("<<0, 255, 0xCA, 0Xca, 0x0>>")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tk := range toks {
		if tk.kind == tInt {
			got = append(got, tk.text)
		}
	}
	if !reflect.DeepEqual(got, []string{"0", "255", "0xCA", "0Xca", "0x0"}) {
		t.Errorf("ints = %v", got)
	}
}

func TestScannerFloats(t *testing.T) {
	toks, err := scanAll("<<1.5, 12.256464, 3.14, 1e5, 1.5e-3, 2E+4>>")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tk := range toks {
		if tk.kind == tFloat {
			got = append(got, tk.text)
		}
	}
	if !reflect.DeepEqual(got, []string{"1.5", "12.256464", "3.14", "1e5", "1.5e-3", "2E+4"}) {
		t.Errorf("floats = %v", got)
	}
}

func TestScannerStrings(t *testing.T) {
	toks, err := scanAll(`<<"GET ", "a\"b\\c\nd\x41">>`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tk := range toks {
		if tk.kind == tString {
			got = append(got, tk.text)
		}
	}
	want := []string{"GET ", "a\"b\\c\ndA"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("strings = %q, want %q", got, want)
	}
}

func TestScannerWhitespaceNewlines(t *testing.T) {
	// a multiline literal: newlines and spaces are ignored
	toks, err := scanAll("<<A,\n\tB  :  16\n, C>>")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tk := range toks {
		switch tk.kind {
		case tIdent:
			got = append(got, tk.text)
		case tInt:
			got = append(got, tk.text)
		}
	}
	if !reflect.DeepEqual(got, []string{"A", "B", "16", "C"}) {
		t.Errorf("tokens = %v", got)
	}
}

func TestScannerEmptyPattern(t *testing.T) {
	toks, err := scanAll("<<>>")
	if err != nil {
		t.Fatal(err)
	}
	if len(toks) != 2 || toks[0].kind != tOpen || toks[1].kind != tClose {
		t.Errorf("tokens = %+v", toks)
	}
}

// Every tokenization error is of class ErrCompile, with a non-zero offset.
func TestScannerErrors(t *testing.T) {
	cases := []string{
		"<<A@8>>",         // an unexpected character
		"<<$X>>",          // an unexpected character
		"<A:8>>",          // a single <
		"<<A:8>",          // a single >
		"<<_foo>>",        // an identifier with a leading _
		"<<0x>>",          // hex without digits
		"<<1.>>",          // a float without fractional digits
		"<<1e>>",          // a float without exponent digits
		"<<12abc>>",       // a number running into a letter
		`<<"abc>>`,        // an unterminated string
		`<<"a\q">>`,       // an unknown escape
		`<<"a\x4">>`,      // \x with a single hex digit
		`<<"unterminated`, // a string without a closing quote and brackets
	}
	for _, src := range cases {
		if _, err := scanAll(src); err == nil {
			t.Errorf("%q: a tokenization error was expected", src)
			continue
		} else if !errors.Is(err, ErrCompile) {
			t.Errorf("%q: the error is not of class ErrCompile: %v", src, err)
		}
	}
}

func TestScannerErrorPosition(t *testing.T) {
	src := "<<A @B>>"
	s := newScanner(src)
	for {
		_, err := s.next()
		if err != nil {
			if !errors.Is(err, ErrCompile) {
				t.Fatalf("the error is not ErrCompile: %v", err)
			}
			if got := s.off; got != 4 {
				t.Errorf("scanner offset = %d, want 4", got)
			}
			return
		}
	}
}
