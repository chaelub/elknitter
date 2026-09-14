package elknitter

import (
	"errors"
	"testing"
)

// The helper compilation: parses and verifies a whole pattern.
func mustCompile(t *testing.T, src string) *compiledPattern {
	t.Helper()
	raws, err := parseSegments(src)
	if err != nil {
		t.Fatalf("%q: parse: %v", src, err)
	}
	cp, err := compileSegments(raws)
	if err != nil {
		t.Fatalf("%q: compile: %v", src, err)
	}
	return cp
}

func wantCompileError(t *testing.T, src string) {
	t.Helper()
	raws, err := parseSegments(src)
	if err == nil {
		_, err = compileSegments(raws)
	}
	if err == nil {
		t.Errorf("%q: a compile error was expected", src)
	} else if !errors.Is(err, ErrCompile) {
		t.Errorf("%q: the error is not of class ErrCompile: %v", src, err)
	}
}

// Scenarios (spec pattern-language): «Повторное связывание переменной»,
// «Несочетаемые литералы», «Ссылка вперёд на несвязанную переменную»,
// «Конфликт внутри группы», «Знак для не-integer типа»,
// «Запрещённые Size и unit у UTF-типов»,
// «Размер бинарного сегмента не кратен 8 битам»,
// «Целое шире 64 бит», «Остаточный сегмент в середине». Every class of static
// error gives ErrCompile rather than a panic.
func TestCompileErrorCorpus(t *testing.T) {
	cases := []string{
		// a duplicate binding
		"<<A:8, B:8, A:16>>",
		"<<A:8, A:8>>",
		// the size variable is not bound (or is bound later)
		"<<Payload:Len/binary, Len:8>>",
		"<<X:Y>>",
		"<<X:Len:8, Len:8>>",
		"<<X:Len:8>>",
		// the size refers to a non-integer variable
		"<<A:16/float, B:A:8>>",
		"<<A/utf8, B:A:8>>",
		// a tail binary/bitstring not at the end
		"<<A/binary, B/binary>>",
		"<<A/binary, B:8>>",
		"<<A/bits, B:8>>",
		// a binary segment (not a tail) whose width is not a multiple of 8
		"<<A:1/binary-unit:3, B:8>>",
		// an integer wider than 64 bits
		"<<A:65>>",
		"<<A:8/unit:32>>", // 8×32 = 256 bits
		// a float size outside {16,32,64}
		"<<A:24/float>>",
		"<<A:128/float>>",
		// a size on utf
		"<<A:8/utf8>>",
		"<<A:16/utf16>>",
		// a unit on utf
		"<<A/utf8-unit:1>>",
		"<<A/utf16-unit:2>>",
		// duplicates within specifier groups
		"<<A:16/integer-binary>>",
		"<<A:8/binary-bytes>>",
		"<<A:8/unsigned-signed>>",
		"<<A:8/big-little>>",
		"<<A:8/unit:2-unit:4>>",
		// signedness on a non-integer segment
		"<<A:8/binary-signed>>",
		"<<A:16/float-unsigned>>",
		"<<A/utf16-signed>>",
		// byte order for types where it does not apply
		"<<A/utf8-little>>",
		"<<A:8/binary-little>>",
		"<<A:8/bitstring-big>>",
		"<<A:8/bits-little>>",
		// the unit of a bitstring is always 1
		"<<A:8/bitstring-unit:2>>",
		"<<A:4/bits-unit:1-unit:1>>", // a duplicate unit
		// a unit outside the range 1..256
		"<<A:8/unit:0>>",
		"<<A:8/unit:257>>",
		// a constant literal outside the interpretation range
		"<<256:8>>",
		"<<0x100:8>>",
		"<<300>>", // the default size is 8
		"<<128:8/signed>>",
		// literals in the wrong segment
		"<<3.14:32>>", // a float literal requires /float
		"<<3:64/float>>",
		"<<5/binary>>",
		// a string with a size or a type
		`<<"ab":16>>`,
		`<<"ab":8>>`,
		`<<"ab"/binary>>`,
		// native is not supported
		"<<A:8/native>>",
		"<<A:16/native-little>>",
	}
	for _, src := range cases {
		wantCompileError(t, src)
	}
}

// Valid patterns compile; the exact structure of the csegs is checked.
func TestCompileDefaults(t *testing.T) {
	cp := mustCompile(t, "<<A, B:16, C:4/bits, D/binary>>")
	if len(cp.segs) != 4 {
		t.Fatalf("segments %d, want 4", len(cp.segs))
	}
	a, b, c, d := cp.segs[0], cp.segs[1], cp.segs[2], cp.segs[3]
	if a.typ != typeInteger || a.signed || a.little || a.width != 8 || a.unit != 1 {
		t.Errorf("A = %+v", a)
	}
	if b.typ != typeInteger || b.width != 16 {
		t.Errorf("B = %+v", b)
	}
	if c.typ != typeBitstring || c.unit != 1 || c.width != 4 {
		t.Errorf("C = %+v", c)
	}
	if d.typ != typeBinary || d.unit != 8 || !d.isTail || d.width != -1 {
		t.Errorf("D = %+v", d)
	}
	if len(cp.varNames) != 4 || cp.varNames[3] != "D" {
		t.Errorf("varNames = %v", cp.varNames)
	}
	for i, want := range []int{0, 1, 2, 3} {
		if cp.segs[i].varSlot != want {
			t.Errorf("segment %d varSlot = %d, want %d", i, cp.segs[i].varSlot, want)
		}
	}
}

// Scenario «Размер из ранее связанной переменной» (spec pattern-language):
// Len is bound before the size of Payload is computed; the reference index
// points at its binding slot.
func TestCompileSizeVariableResolved(t *testing.T) {
	cp := mustCompile(t, "<<Len:8, Payload:Len/binary, Rest/binary>>")
	if len(cp.segs) != 3 {
		t.Fatalf("segments %d", len(cp.segs))
	}
	if got := cp.segs[1].sizeIdx; got != 0 {
		t.Errorf("sizeIdx = %d, want 0 (the Len slot)", got)
	}
	if cp.segs[1].isTail {
		t.Error("Payload:Len/binary is not a tail")
	}
	if !cp.segs[2].isTail {
		t.Error("Rest/binary must be the tail")
	}
	if got := cp.segs[1].width; got != -1 {
		t.Errorf("the width of Payload = %d, want -1 (dynamic)", got)
	}
}

// A size may be reused several times after it has been bound.
func TestCompileSizeReuse(t *testing.T) {
	mustCompile(t, "<<N:8, A:N, B:N>>")
}

// Scenario «Префикс протокола» (spec pattern-language): a string literal
// expands into byte segments; an empty string gives zero segments.
func TestCompileStringExpansion(t *testing.T) {
	cp := mustCompile(t, `<<"GET ", Path/binary>>`)
	if len(cp.segs) != 5 { // 4 bytes of "GET " + Path
		t.Fatalf("segments %d, want 5", len(cp.segs))
	}
	for i := 0; i < 4; i++ {
		s := cp.segs[i]
		if s.kind != segLitInt || s.width != 8 || s.litInt != uint64("GET "[i]) {
			t.Errorf("byte %d = %+v", i, s)
		}
	}
	if cp.segs[4].kind != segBind || cp.segs[4].varSlot != 0 {
		t.Errorf("Path = %+v", cp.segs[4])
	}

	cp = mustCompile(t, `<<C:8, "">>`)
	if len(cp.segs) != 1 {
		t.Errorf("empty string: segments %d, want 1", len(cp.segs))
	}
}

// Each byte of a string is a separate segment in the indexing (also valid for
// 8-bit utf8 data; "GET" is single-byte).
func TestCompileWideAndSignedFlags(t *testing.T) {
	cp := mustCompile(t, "<<U:64, S:64/signed, B:64/unsigned, U8:8>>")
	u, s, b := cp.segs[0], cp.segs[1], cp.segs[2]
	if u.signed || u.typ != typeInteger || u.width != 64 {
		t.Errorf("U = %+v", u)
	}
	if !s.signed || s.width != 64 {
		t.Errorf("S = %+v", s)
	}
	if b.signed {
		t.Errorf("B = %+v", b)
	}
}

// Literal boundary values: fitting and not fitting.
func TestCompileLiteralBounds(t *testing.T) {
	mustCompile(t, "<<0:8, 255:8, 0:8/signed, 127:8/signed, 0:0, 0:1/signed, 1:1, 0xFFFFFFFFFFFFFFFF:64>>")
	wantCompileError(t, "<<1:0>>")
	wantCompileError(t, "<<1:1/signed>>")
	wantCompileError(t, "<<0x10000000000000000:64>>") // a broken number: >64-bit hex — a parse error
}

// Scenarios (spec pattern-language): «Анонимный пропуск», «Размер-литерал»,
// «Валидный паттерн не даёт ошибок компиляции» — genuinely valid binary forms
// from the reference.
func TestCompileValidErlangShapes(t *testing.T) {
	// scenario «Анонимный пропуск»: _:16 is skipped
	mustCompile(t, "<<_:16, X:32>>")
	// scenario «Размер-литерал»: a zero size is allowed
	mustCompile(t, "<<A:32, B:0, C:16>>")
	// "4 bytes of the middle": 4×8 = 32 bits — valid
	mustCompile(t, "<<M:4/binary, Rest/binary>>")
	// a tail binary with unit:16
	mustCompile(t, "<<A:8, Rest/binary-unit:16>>")
	// tail divisibility by the unit is checked on data, not at compile time
	mustCompile(t, "<<X:3/bits, Rest/bitstring>>")
	// float16
	mustCompile(t, "<<F:16/float>>")
	// utf16 with a byte order
	mustCompile(t, "<<Ch/utf16, Ch2/utf16-little>>")
	mustCompile(t, "<<Ch/utf32-big>>")
	// a literal in the signed range
	mustCompile(t, "<<127:8/signed, 200:8>>")
	// a size variable for integer and bitstring segments
	mustCompile(t, "<<N:8, A:N, B:N/bits>>")
	// any number of anonymous variables
	mustCompile(t, "<<_:8, _:16/little, Rest/binary>>")
	// a single unit: unit:256 with a small size is allowed for float
	mustCompile(t, "<<F:1/float-unit:64>>")
}

// Scenario «Пустой паттерн» (spec pattern-language): <<>> compiles without
// segments or variables.
func TestCompileEmpty(t *testing.T) {
	cp := mustCompile(t, "<<>>")
	if len(cp.segs) != 0 || len(cp.varNames) != 0 {
		t.Errorf("cp = %+v", cp)
	}
}

// Scenario «Полный набор спецификаторов» (spec pattern-language).
func TestScenarioFullSpecifierList(t *testing.T) {
	cp := mustCompile(t, "<<X:32/little-signed-integer>>")
	seg := cp.segs[0]
	if seg.typ != typeInteger || !seg.signed || !seg.little || seg.width != 32 {
		t.Errorf("X = %+v", seg)
	}
}

// Scenario «Произвольный порядок значений» (spec pattern-language).
func TestScenarioSpecifierAnyOrder(t *testing.T) {
	cp := mustCompile(t, "<<X:12/unsigned-little-integer>>")
	seg := cp.segs[0]
	if seg.typ != typeInteger || seg.signed || !seg.little || seg.width != 12 {
		t.Errorf("X = %+v", seg)
	}
}

// Scenario «Размеры по умолчанию» (spec pattern-language): C:4/binary means
// 4 units of 8 bits each, 32 bits in total.
func TestScenarioDefaultSizes(t *testing.T) {
	cp := mustCompile(t, "<<A, B:16, C:4/binary, Rest/binary>>")
	if len(cp.segs) != 4 {
		t.Fatalf("segments %d, want 4", len(cp.segs))
	}
	a, b, c, rest := cp.segs[0], cp.segs[1], cp.segs[2], cp.segs[3]
	if a.width != 8 || a.unit != 1 {
		t.Errorf("A = %+v", a)
	}
	if b.width != 16 {
		t.Errorf("B = %+v", b)
	}
	if c.typ != typeBinary || c.unit != 8 || c.width != 32 || c.isTail {
		t.Errorf("C = %+v", c)
	}
	if !rest.isTail || rest.typ != typeBinary {
		t.Errorf("Rest = %+v", rest)
	}
}

// Scenarios (spec pattern-language): «Незакрытый паттерн», «Пропущен
// разделитель», «Недопустимое имя», «Компиляция не требует данных» (task
// 2.4): compilation never panics on deliberately invalid pattern strings — any
// stage gives ErrCompile, and no data is needed.
func TestCompileNoPanicInvalidCorpus(t *testing.T) {
	cases := []string{
		"", "<<", ">>", "<>", "<<>>x", "x<<>>", "<<A", "<<A:8", "<<A:8,", "<<,>>",
		"<<A:8,>>", "<<A,,B>>", "<<A:8 B:8>>", "<<A@8>>", "<<_foo>>", "<<1x>>",
		"<<0x>>", "<<1.>>", "<<1e>>", "<<12abc>>", `<<"unterminated`,
		`<<"a\q">>`, "<<A:65>>", "<<A:8/unknown>>", "<<A:8/native>>",
		"<<A:8, A:8>>", "<<B:A:8>>", "<<A/binary, B:8>>", "<<A/binary-float>>",
		"<<A:16/utf16>>", "<<A:8/unit:0>>", "<<-1:8>>", "<<3.14>>", "<<256>>",
		"<<A:8/binary-binary>>", "<<A/B>>", "<<A:8/unit:300>>", "<<A:0x>>",
		"<<A:8 //>>", "<<A:8/big-little>>", "<<A:8/signed-signed>>",
		"<<A:8/bits-unit:2>>", "<<1:0>>", "<<A:8/binary-little>>", "<<A/utf8-big>>",
	}
	for _, src := range cases {
		wantCompileError(t, src) // parse or compile — an ErrCompile error, not a panic
	}
}
