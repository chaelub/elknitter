package elknitter

import (
	"errors"
	"reflect"
	"testing"
)

// Matching without the public API: parse + compile + matchData.
func matchDataT(t *testing.T, src string, data []byte) (map[string]any, error) {
	t.Helper()
	cp := mustCompile(t, src)
	return matchData(cp, data)
}

func wantMatchError(t *testing.T, src string, data []byte, wantSeg int, wantText string) *NoMatchError {
	t.Helper()
	_, err := matchDataT(t, src, data)
	if err == nil {
		t.Fatalf("%q with data %#v: a mismatch error was expected", src, data)
	}
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("%q: the error is not of class ErrNoMatch: %v", src, err)
	}
	var nm *NoMatchError
	if !errors.As(err, &nm) {
		t.Fatalf("%q: the error is not a NoMatchError: %v", src, err)
	}
	if wantSeg >= 0 && nm.Segment != wantSeg {
		t.Errorf("%q: Segment = %d, want %d (error: %v)", src, nm.Segment, wantSeg, nm)
	}
	if wantText != "" && !contains(nm.Reason, wantText) {
		t.Errorf("%q: Reason = %q, want it to contain %q", src, nm.Reason, wantText)
	}
	return nm
}

func contains(s, sub string) bool {
	return len(sub) == 0 || len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// Scenario «Эталон из документации» (spec matching-engine); the same pattern as
// scenario «Трёхсегментный паттерн» (pattern-language): three segments.
func TestScenarioReference(t *testing.T) {
	m, err := matchDataT(t, "<<A, B:16, Rest/binary>>", []byte{1, 2, 3, 4, 5})
	if err != nil {
		t.Fatal(err)
	}
	if m["A"] != int64(1) {
		t.Errorf("A = %v (%T)", m["A"], m["A"])
	}
	if m["B"] != int64(515) {
		t.Errorf("B = %v", m["B"])
	}
	if !reflect.DeepEqual(m["Rest"], []byte{4, 5}) {
		t.Errorf("Rest = %#v", m["Rest"])
	}
}

// Scenario «Все переменные в map»: little 0x0302 = 770.
func TestScenarioAllVariablesInMap(t *testing.T) {
	m, err := matchDataT(t, "<<A, B:16/little, Rest/binary>>", []byte{1, 2, 3, 4, 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 3 {
		t.Fatalf("map = %v", m)
	}
	if m["A"] != int64(1) || m["B"] != int64(770) || !reflect.DeepEqual(m["Rest"], []byte{4, 5}) {
		t.Errorf("map = %v", m)
	}
}

// Scenario «Паттерн только из литералов»: an empty non-nil map.
func TestScenarioLiteralOnlyPattern(t *testing.T) {
	m, err := matchDataT(t, `<<"GET ", _/binary>>`, []byte("GET /path"))
	if err != nil {
		t.Fatal(err)
	}
	if m == nil || len(m) != 0 {
		t.Errorf("map = %#v, want an empty non-nil map", m)
	}
}

// Scenario «Атомарность при несовпадении».
func TestScenarioAtomicity(t *testing.T) {
	_, err := matchDataT(t, "<<A:8, B:8>>", []byte{1})
	wantMatchErrorHelper(t, err, 2, "")
}

func wantMatchErrorHelper(t *testing.T, err error, wantSeg int, wantText string) {
	t.Helper()
	if err == nil {
		t.Fatal("a mismatch error was expected")
	}
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("the error is not ErrNoMatch: %v", err)
	}
	var nm *NoMatchError
	if !errors.As(err, &nm) {
		t.Fatalf("not a NoMatchError: %v", err)
	}
	if wantSeg >= 0 && nm.Segment != wantSeg {
		t.Errorf("Segment = %d, want %d (%v)", nm.Segment, wantSeg, nm)
	}
	if wantText != "" && !contains(nm.Reason, wantText) {
		t.Errorf("Reason = %q, want %q", nm.Reason, wantText)
	}
}

// Scenarios «Знак» and «Little-endian».
func TestScenarioSignAndLittleEndian(t *testing.T) {
	m, err := matchDataT(t, "<<X:8/signed>>", []byte{0xFF})
	if err != nil {
		t.Fatal(err)
	}
	if m["X"] != int64(-1) {
		t.Errorf("signed X = %v", m["X"])
	}
	m, err = matchDataT(t, "<<Y:8/unsigned>>", []byte{0xFF})
	if err != nil {
		t.Fatal(err)
	}
	if m["Y"] != int64(255) {
		t.Errorf("unsigned Y = %v", m["Y"])
	}
	m, err = matchDataT(t, "<<X:16/little>>", []byte{2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if m["X"] != int64(770) {
		t.Errorf("little X = %v", m["X"])
	}
}

// Scenario «Без байтового выравнивания».
func TestScenarioNoByteAlignment(t *testing.T) {
	m, err := matchDataT(t, "<<A:4, B:12>>", []byte{0xAB, 0xCD})
	if err != nil {
		t.Fatal(err)
	}
	if m["A"] != int64(0xA) || m["B"] != int64(0xBCD) {
		t.Errorf("map = %v", m)
	}
}

// Scenario «Совпадение префикса».
func TestScenarioLiteralPrefixMatch(t *testing.T) {
	m, err := matchDataT(t, `<<"GET ", Path/binary>>`, []byte("GET /index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m["Path"], []byte("/index.html")) {
		t.Errorf("Path = %#v", m["Path"])
	}
}

// Scenarios «Несовпадение литерала» and «Несовпадение описывает причину»
// (matching-engine); «Магическое число в hex» (pattern-language): hex literals
// compile and match.
func TestScenarioLiteralMismatch(t *testing.T) {
	wantMatchError(t, "<<0xCA, 0xFE, Data/binary>>", []byte{0xCA, 0xFF, 0x01}, 2, "0xFE")
	// «Несовпадение описывает причину»: at the first segment the literal 0xCA
	// against 0xAB; the reason names the literal segment.
	wantMatchError(t, "<<0xCA, Data/binary>>", []byte{0xAB}, 1, "0xCA")
}

// Scenarios «Классический length-prefix» and «Нулевая длина».
func TestScenarioLengthPrefix(t *testing.T) {
	data := []byte{3, 'a', 'b', 'c', 0xD1, 0x85, 0xD0, 0xB2, 0xD0, 0xBE, 0xD1, 0x81, 0xD1, 0x82}
	m, err := matchDataT(t, "<<Len:8, Payload:Len/binary, Rest/binary>>", data)
	if err != nil {
		t.Fatal(err)
	}
	if m["Len"] != int64(3) || !reflect.DeepEqual(m["Payload"], []byte("abc")) ||
		!reflect.DeepEqual(m["Rest"], []byte("хвост")) {
		t.Errorf("map = %v", m)
	}

	m, err = matchDataT(t, "<<Len:8, Payload:Len/binary, Rest/binary>>", []byte{0, 'x'})
	if err != nil {
		t.Fatal(err)
	}
	if m["Len"] != int64(0) || len(m["Payload"].([]byte)) != 0 || !reflect.DeepEqual(m["Rest"], []byte{'x'}) {
		t.Errorf("zero length: map = %v", m)
	}
}

// Scenarios (matching-engine): «Остаток из документации»,
// «Пустой вход с остаточным паттерном» (an empty input succeeds when the
// pattern ends with a tail binary; «Пустой остаток» is a special case of it).
func TestScenarioTailRest(t *testing.T) {
	m, err := matchDataT(t, "<<Len:8, Payload:Len/binary, Rest/binary>>", []byte{3, 'a', 'b', 'c', 0xD1, 0x85})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m["Rest"], []byte{0xD1, 0x85}) {
		t.Errorf("Rest = %#v", m["Rest"])
	}

	m, err = matchDataT(t, "<<A:8, Rest/binary>>", []byte{7})
	if err != nil {
		t.Fatal(err)
	}
	if m["A"] != int64(7) || len(m["Rest"].([]byte)) != 0 {
		t.Errorf("empty tail: %v", m)
	}

	m, err = matchDataT(t, "<<Rest/binary>>", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m["Rest"].([]byte)) != 0 {
		t.Errorf("empty input: %v", m)
	}
}

// Scenario «Делимость на явный unit»: a tail binary with unit 16.
func TestScenarioTailDivisibility(t *testing.T) {
	if _, err := matchDataT(t, "<<_/binary-unit:16>>", []byte("abcd")); err != nil {
		t.Errorf("abcd: %v", err)
	}
	wantMatchError(t, "<<_/binary-unit:16>>", []byte("abc"), 1, "24")
}

// Scenarios «Лишние байты», «Лишние байты допустимы с остатком»,
// «Нехватка данных».
func TestScenarioFullConsumption(t *testing.T) {
	wantMatchError(t, "<<A:8>>", []byte{1, 2}, 0, "8")
	if _, err := matchDataT(t, "<<A:8, _/binary>>", []byte{1, 2}); err != nil {
		t.Errorf("tail with an anonymous variable: %v", err)
	}
	// shortfall: 0 bits left for B:8 — segment 2
	wantMatchError(t, "<<A:16, B:8>>", []byte{1, 2}, 2, "only 0 left")
}

// Scenario «Широкий unsigned».
func TestScenarioWideUnsigned(t *testing.T) {
	m, err := matchDataT(t, "<<X:64/unsigned-little>>", []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := m["X"].(uint64); !ok || v != 18446744073709551615 {
		t.Errorf("X = %#v (%T)", m["X"], m["X"])
	}
}

// Scenario «Float и codepoint»: 1.5 as float32 + 'я'.
func TestScenarioFloatAndCodepoint(t *testing.T) {
	data := []byte{0x3F, 0xC0, 0x00, 0x00, 0xD1, 0x8F}
	m, err := matchDataT(t, "<<F:32/float, C/utf8>>", data)
	if err != nil {
		t.Fatal(err)
	}
	if m["F"] != float64(1.5) {
		t.Errorf("F = %v", m["F"])
	}
	if m["C"] != rune('я') {
		t.Errorf("C = %v (%T)", m["C"], m["C"])
	}
}

// Scenario «Многосимвольный UTF-8 поток»: two runes in a row.
func TestScenarioUTF8Stream(t *testing.T) {
	m, err := matchDataT(t, "<<C1/utf8, C2/utf8, Tail/binary>>", []byte("éя"))
	if err != nil {
		t.Fatal(err)
	}
	if m["C1"] != rune('é') || m["C2"] != rune('я') || len(m["Tail"].([]byte)) != 0 {
		t.Errorf("map = %v", m)
	}
}

// Scenario «Невалидный UTF-8»: extra bytes, shortfall, overlong, surrogate.
func TestScenarioInvalidUTF8(t *testing.T) {
	wantMatchError(t, "<<C/utf8, _/binary>>", []byte{0xFF, 0x00}, 1, "utf8")
	wantMatchError(t, "<<C/utf8, _/binary>>", []byte{0xC3}, 1, "") // shortfall
	// overlong: 0xC0 0x80
	wantMatchError(t, "<<C/utf8, _/binary>>", []byte{0xC0, 0x80}, 1, "utf8")
	// a surrogate in utf8: 0xED 0xA0 0x80 (U+D800)
	wantMatchError(t, "<<C/utf8, _/binary>>", []byte{0xED, 0xA0, 0x80}, 1, "utf8")
}

// Scenarios (matching-engine): «UTF-16 little-endian с суррогатной парой»,
// «UTF-16 big-endian по умолчанию», «Одиночный суррогат».
func TestScenarioUTF16(t *testing.T) {
	// little with the surrogate pair U+1F600: 3D D8 00 DE
	m, err := matchDataT(t, "<<C/utf16-little, _/binary>>", []byte{0x3D, 0xD8, 0x00, 0xDE})
	if err != nil {
		t.Fatal(err)
	}
	if m["C"] != rune(0x1F600) {
		t.Errorf("utf16le C = %#x", m["C"])
	}
	// big by default: D8 3D DE 00
	m, err = matchDataT(t, "<<C/utf16, _/binary>>", []byte{0xD8, 0x3D, 0xDE, 0x00})
	if err != nil {
		t.Fatal(err)
	}
	if m["C"] != rune(0x1F600) {
		t.Errorf("utf16be C = %#x", m["C"])
	}
	// lone surrogate: D8 00 + an extra byte
	wantMatchError(t, "<<C/utf16, _/binary>>", []byte{0xD8, 0x00, 0x41}, 1, "surrogate")
}

// Scenario «UTF-32»: little-endian by default and explicitly.
func TestScenarioUTF32(t *testing.T) {
	m, err := matchDataT(t, "<<C/utf32-little, _/binary>>", []byte{0x00, 0xF6, 0x01, 0x00})
	if err != nil {
		t.Fatal(err)
	}
	if m["C"] != rune(0x1F600) {
		t.Errorf("utf32le C = %#x", m["C"])
	}
	// out of range: 0x110000
	wantMatchError(t, "<<C/utf32, _/binary>>", []byte{0x00, 0x11, 0x00, 0x00}, 1, "range")
	// the surrogate U+D800 in utf32
	wantMatchError(t, "<<C/utf32, _/binary>>", []byte{0x00, 0x00, 0xD8, 0x00}, 1, "range")
}

// Scenario «Пустой паттерн»: an empty input succeeds; a non-empty one leaves
// extra bits.
func TestScenarioEmptyPattern(t *testing.T) {
	m, err := matchDataT(t, "<<>>", nil)
	if err != nil {
		t.Fatal(err)
	}
	if m == nil || len(m) != 0 {
		t.Errorf("map = %#v", m)
	}
	wantMatchError(t, "<<>>", []byte{1}, 0, "8")
}

// Scenario «Результат не алиасит вход»: mutating the input slice does not
// change the bound value.
func TestScenarioResultDoesNotAliasInput(t *testing.T) {
	in := []byte{'a', 'b', 'c', 'd', 'e'}
	m, err := matchDataT(t, "<<A:8, B/binary>>", in)
	if err != nil {
		t.Fatal(err)
	}
	rest := m["B"].([]byte)
	in[1] = 'X'
	if rest[0] != 'b' {
		t.Errorf("B aliases the input: %q", rest)
	}
	// and the result itself is copied on a repeat
	rest[0] = 'Y'
	if m2, _ := matchDataT(t, "<<A:8, B/binary>>", []byte("abcde")); m2["B"].([]byte)[0] != 'b' {
		t.Errorf("a repeated match is corrupted: %v", m2)
	}
}

// Bit-level (not byte-level) values: bitstring segments.
func TestScenarioBitsValues(t *testing.T) {
	// X:3/bits over the bits 101
	m, err := matchDataT(t, "<<X:3/bits, Rest/bits>>", []byte{0xA0})
	if err != nil {
		t.Fatal(err)
	}
	x := m["X"].(Bits)
	if x.Len() != 3 || x.Bytes()[0] != 0xA0 {
		t.Errorf("X = %+v", x)
	}
	// the remainder: 5 bits 00000
	r := m["Rest"].(Bits)
	if r.Len() != 5 {
		t.Errorf("Rest.Len = %d", r.Len())
	}

	// a bitstring too short for a tail binary: <<A:4, R/binary>> over 6 bits
	wantMatchError(t, "<<A:4, R/binary>>", []byte{0xF0}, 2, "divisible")
}

// Literals: byte, hex, signed, string.
func TestScenarioLiteralKinds(t *testing.T) {
	if _, err := matchDataT(t, "<<0xCA, 0xFE>>", []byte{0xCA, 0xFE}); err != nil {
		t.Errorf("hex: %v", err)
	}
	if _, err := matchDataT(t, "<<0, 255>>", []byte{0, 255}); err != nil {
		t.Errorf("dec: %v", err)
	}
	// a signed literal is compared with the signed interpretation
	// (the range was checked at compile time: 255 does not fit into signed 8)
	if _, err := matchDataT(t, "<<127:8/signed>>", []byte{0x7F}); err != nil {
		t.Errorf("signed lit: %v", err)
	}
	if _, err := matchDataT(t, `<<"AB", C/binary>>`, []byte{'A', 'B', 'C'}); err != nil {
		t.Errorf("string: %v", err)
	}
	// a string mismatch: the position of the second byte
	wantMatchError(t, `<<"AB", C/binary>>`, []byte{'A', 'X', 'C'}, 2, "")
}

// Comparing a float literal: exact IEEE equality.
func TestScenarioFloatLiteral(t *testing.T) {
	data := []byte{0x3F, 0xC0, 0x00, 0x00} // float32 1.5
	if _, err := matchDataT(t, "<<1.5:32/float>>", data); err != nil {
		t.Errorf("1.5: %v", err)
	}
	wantMatchError(t, "<<1.5:32/float>>", []byte{0x3F, 0x80, 0x00, 0x00}, 1, "1.5")
	// NaN never equals the literal 1.5
	wantMatchError(t, "<<1.5:32/float>>", []byte{0x7F, 0xC0, 0x00, 0x00}, 1, "1.5")
}

// Segment values and size variables from the "negative" scenarios: a size
// variable with a non-integer/negative value is impossible (only integers can
// be bound), and a width above 64 at runtime is impossible (compile) — but a
// size larger than the remainder gives a shortfall.
func TestScenarioVarSizeOvershoot(t *testing.T) {
	// N = 250, Payload:N/binary with 5 bytes left — a shortfall at Payload
	// (250×8 = 2000 bits are needed)
	wantMatchError(t, "<<N:8, Payload:N/binary, Rest/binary>>", []byte{250, 1, 2, 3, 4, 5}, 2, "2000")
	// N=0 for an integer segment: <<N:8, A:N>> is an empty segment
	m, err := matchDataT(t, "<<N:8, A:N, B:8>>", []byte{0, 0xAB})
	if err != nil {
		t.Fatal(err)
	}
	if m["A"] != int64(0) || m["B"] != int64(0xAB) {
		t.Errorf("map = %v", m)
	}
}
