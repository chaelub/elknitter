package elknitter

import (
	"errors"
	"reflect"
	"testing"
)

// Scenario «Трёхсегментный паттерн» (spec pattern-language).
func TestScenarioThreeSegmentPattern(t *testing.T) {
	segs, err := parseSegments("<<A, B:16, Rest/binary>>")
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 3 {
		t.Fatalf("segments %d, want 3", len(segs))
	}
	a, b, rest := segs[0], segs[1], segs[2]
	if a.valueKind != vVar || a.name != "A" {
		t.Errorf("segment 0 = %+v", a)
	}
	if b.valueKind != vVar || b.name != "B" || !b.hasSize || b.sizeIsVar || b.sizeVal != 16 {
		t.Errorf("segment 1 = %+v", b)
	}
	if rest.valueKind != vVar || rest.name != "Rest" || rest.hasSize {
		t.Errorf("segment 2 = %+v", rest)
	}
	if len(rest.specItems) != 1 || rest.specItems[0].text != "binary" {
		t.Errorf("Rest specifiers = %+v", rest.specItems)
	}
}

// Scenario «Пустой паттерн» (spec pattern-language).
func TestScenarioEmptyPatternCompiles(t *testing.T) {
	segs, err := parseSegments("<<>>")
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 0 {
		t.Errorf("segments %d, want 0", len(segs))
	}
}

// Scenario «Незакрытый паттерн» (spec pattern-language).
func TestScenarioUnclosedPattern(t *testing.T) {
	for _, src := range []string{"<<A:8", "<<A:8, B", "<<A"} {
		if _, err := parseSegments(src); !errors.Is(err, ErrCompile) {
			t.Errorf("%q: error = %v, want ErrCompile", src, err)
		}
	}
}

// Scenario «Пропущен разделитель» (spec pattern-language).
func TestScenarioMissingSeparator(t *testing.T) {
	if _, err := parseSegments("<<A:8 B:8>>"); !errors.Is(err, ErrCompile) {
		t.Errorf("error = %v, want ErrCompile", err)
	}
}

func TestParserRejectsJunk(t *testing.T) {
	for _, src := range []string{
		"",          // an empty string
		"A:8>>",     // does not start with <<
		"<<A:8>> x", // junk after >>
		"<<A:8,>>",  // trailing comma
		"<<,A>>",    // an empty segment
		"<<A,,B>>",  // a double comma
	} {
		if _, err := parseSegments(src); err == nil {
			t.Errorf("%q: a parse error was expected", src)
		} else if !errors.Is(err, ErrCompile) {
			t.Errorf("%q: the error is not ErrCompile: %v", src, err)
		}
	}
}

// Spaces around ':' and '/' inside a segment are part of the general rule
// "spaces and newlines are ignored".
func TestParserSpacesInsideSegment(t *testing.T) {
	for _, src := range []string{
		"<<A:8/binary>>",
		"<<A : 8 / binary>>",
		"<<A\n:\t8\n/binary>>",
	} {
		segs, err := parseSegments(src)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if len(segs) != 1 {
			t.Errorf("%q: segments %d, want 1", src, len(segs))
		}
	}
}

func TestParserFourSegmentForms(t *testing.T) {
	segs, err := parseSegments(`<<X, Y:4, 0xCA/little, 3.14:64/float, _:3/bits>>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 5 {
		t.Fatalf("segments %d, want 5", len(segs))
	}
	if !(segs[0].valueKind == vVar && segs[0].name == "X") {
		t.Errorf("segment 0 = %+v", segs[0])
	}
	if !(segs[1].valueKind == vVar && segs[1].hasSize && segs[1].sizeVal == 4) {
		t.Errorf("segment 1 = %+v", segs[1])
	}
	if !(segs[2].valueKind == vInt && segs[2].intVal == 0xCA && len(segs[2].specItems) == 1) {
		t.Errorf("segment 2 = %+v", segs[2])
	}
	if !(segs[3].valueKind == vFloat && segs[3].floatVal == 3.14 && segs[3].hasSize) {
		t.Errorf("segment 3 = %+v", segs[3])
	}
	if !(segs[4].valueKind == vAnon && segs[4].sizeVal == 3) {
		t.Errorf("segment 4 = %+v", segs[4])
	}
}

func TestParserStringLiteralSegment(t *testing.T) {
	segs, err := parseSegments(`<<"GET ", Path/binary>>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 2 {
		t.Fatalf("segments %d, want 2 (string + Path)", len(segs))
	}
	if segs[0].valueKind != vString || string(segs[0].strBytes) != "GET " {
		t.Errorf("segment 0 = %+v", segs[0])
	}
	if segs[1].valueKind != vVar || segs[1].name != "Path" {
		t.Errorf("segment 1 = %+v", segs[1])
	}
}

func TestParserMultilinePattern(t *testing.T) {
	src := "<<Len:8,\n  Payload : Len/binary,\n  Rest/binary>>"
	segs, err := parseSegments(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 3 {
		t.Fatalf("segments %d, want 3", len(segs))
	}
	if !(segs[1].sizeIsVar && segs[1].sizeVar == "Len") {
		t.Errorf("segment 1 = %+v", segs[1])
	}
}

func TestParserSizeErrors(t *testing.T) {
	for _, src := range []string{
		"<<X:1.5>>", // a float as a size
		"<<X:Y:8>>", // two sizes
		"<<X:_>>",   // an anonymous variable as a size
		`<<X:"a">>`, // a string as a size
	} {
		if _, err := parseSegments(src); !errors.Is(err, ErrCompile) {
			t.Errorf("%q: error = %v, want ErrCompile", src, err)
		}
	}
}

func TestParserSpecListErrors(t *testing.T) {
	for _, src := range []string{
		"<<X:8/>>",           // an empty specifier list
		"<<X:8/-signed>>",    // a dash without an element
		"<<X:8/signed->>",    // a dash without an element at the end
		"<<X:8/unit>>",       // unit without a colon
		"<<X:8/unit:>>",      // unit without a value
		"<<X:8/unit:1.5>>",   // a non-integer unit value
		"<<X:8/little/big>>", // a repeated '/'
	} {
		if _, err := parseSegments(src); !errors.Is(err, ErrCompile) {
			t.Errorf("%q: error = %v, want ErrCompile", src, err)
		}
	}
}

func TestParseIntegerLiterals(t *testing.T) {
	segs, err := parseSegments("<<0, 255, 0xCA, 0Xca>>")
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 4 {
		t.Fatalf("segments %d", len(segs))
	}
	vals := []uint64{0, 255, 0xCA, 0xCA}
	for i, want := range vals {
		if segs[i].valueKind != vInt || segs[i].intVal != want {
			t.Errorf("segment %d = %+v, want %d", i, segs[i], want)
		}
	}
}

// Segments with string literals, sizes and types are expanded/rejected in
// compile.go (task 2.2: structure is checked here, semantics there).
func TestParserSegmentsPositions(t *testing.T) {
	segs, err := parseSegments("<<A, B:16>>")
	if err != nil {
		t.Fatal(err)
	}
	// A starts at byte 2 (after <<), B — after ", "
	if want := []int{2, 5}; !reflect.DeepEqual([]int{segs[0].pos, segs[1].pos}, want) {
		t.Errorf("positions = %v, want %v", []int{segs[0].pos, segs[1].pos}, want)
	}
}
