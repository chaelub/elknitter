package elknitter

// Compilation: verifying rawSegments (the semantics of types, specifiers,
// sizes, binding) and building compiledPattern — a list of csegs ready for
// matching. Every static error from spec pattern-language is found here,
// before any data is touched. Compilation never panics.

import "fmt"

type segType uint8

const (
	typeInteger segType = iota
	typeFloat
	typeBinary
	typeBitstring
	typeUTF8
	typeUTF16
	typeUTF32
)

func (t segType) String() string {
	switch t {
	case typeInteger:
		return "integer"
	case typeFloat:
		return "float"
	case typeBinary:
		return "binary"
	case typeBitstring:
		return "bitstring"
	case typeUTF8:
		return "utf8"
	case typeUTF16:
		return "utf16"
	case typeUTF32:
		return "utf32"
	}
	return "?"
}

type csegKind uint8

const (
	segBind     csegKind = iota // binds a variable
	segSkip                     // the anonymous variable _
	segLitInt                   // integer literal
	segLitFloat                 // float literal
)

// cseg — a compiled segment.
type cseg struct {
	kind     csegKind
	typ      segType
	signed   bool
	little   bool
	unit     int   // the default size applied (integer/float: 1, binary: 8, bitstring: 1)
	isTail   bool  // binary/bitstring without an explicit size — the last segment
	width    int64 // fixed width size×unit; -1 = the size is given by a variable
	sizeIdx  int   // slot index of the size variable; -1 if none
	varSlot  int   // binding slot (kind==segBind); -1 otherwise
	litInt   uint64
	litFloat float64
}

type compiledPattern struct {
	segs     []*cseg
	varNames []string // variable names by slot
}

// bindRec — a variable bound in the part of the pattern already walked.
type bindRec struct {
	slot  int
	isInt bool // an integer segment (only those may be used in sizes)
}

type typeSpec struct {
	typ       segType
	typSet    bool
	signSet   bool
	signed    bool
	endianSet bool
	little    bool
	unitSet   bool
	unit      uint64
}

func compileSegments(raws []rawSegment) (*compiledPattern, error) {
	cp := &compiledPattern{}
	binds := map[string]bindRec{} // name → slot; a variable is bound once

	for i, r := range raws {
		last := i == len(raws)-1

		switch r.valueKind {
		case vString:
			// a string literal expands into byte segments
			if r.hasSize || len(r.specItems) > 0 {
				return nil, compileError(r.pos, "string literal must not have a size or type specifiers; it expands into bytes")
			}
			for _, b := range r.strBytes {
				cp.segs = append(cp.segs, &cseg{
					kind:    segLitInt,
					typ:     typeInteger,
					width:   8,
					sizeIdx: -1,
					varSlot: -1,
					litInt:  uint64(b),
				})
			}
			continue
		case vVar:
			if _, dup := binds[r.name]; dup {
				return nil, compileError(r.pos, "variable %q is bound more than once", r.name)
			}
		}

		ts, err := parseTypeSpecItems(r)
		if err != nil {
			return nil, err
		}

		seg, err := buildSeg(&r, ts, last)
		if err != nil {
			return nil, err
		}

		// a size variable must already be bound and of integer type
		if r.sizeIsVar {
			rec, ok := binds[r.sizeVar]
			if !ok {
				return nil, compileError(r.pos, "size refers to variable %q which is not bound yet (matching is left to right)", r.sizeVar)
			}
			if !rec.isInt {
				return nil, compileError(r.pos, "size must refer to an integer variable; %q is not an integer segment", r.sizeVar)
			}
			seg.sizeIdx = rec.slot
		}

		cp.segs = append(cp.segs, seg)

		// register the binding after the segment has been built successfully
		if r.valueKind == vVar {
			slot := len(cp.varNames)
			cp.varNames = append(cp.varNames, r.name)
			seg.varSlot = slot
			binds[r.name] = bindRec{slot: slot, isInt: seg.typ == typeInteger}
		}
	}
	return cp, nil
}

// buildSeg assembles a cseg from a raw segment and its specifiers, checking
// the static rules of compatibility and size.
func buildSeg(r *rawSegment, ts *typeSpec, last bool) (*cseg, error) {
	seg := &cseg{
		typ:     typeInteger,
		unit:    1,
		width:   -1,
		sizeIdx: -1,
		varSlot: -1,
	}
	if ts.typSet {
		seg.typ = ts.typ
	}
	if ts.signSet {
		if seg.typ != typeInteger {
			return nil, compileError(r.pos, "signedness is only allowed for integer segments, not %s", seg.typ)
		}
		seg.signed = ts.signed
	}
	if ts.unitSet {
		if ts.unit == 0 || ts.unit > 256 {
			return nil, compileError(r.pos, "unit must be in 1..256")
		}
		seg.unit = int(ts.unit)
	} else if seg.typ == typeBinary {
		seg.unit = 8
	}

	if ts.endianSet {
		switch seg.typ {
		case typeInteger, typeFloat, typeUTF16, typeUTF32:
			seg.little = ts.little
		case typeUTF8:
			return nil, compileError(r.pos, "endianness is not allowed for utf8 segments")
		default:
			return nil, compileError(r.pos, "endianness is only allowed for integer, float, utf16 and utf32 segments, not %s", seg.typ)
		}
	}

	isUTF := seg.typ == typeUTF8 || seg.typ == typeUTF16 || seg.typ == typeUTF32
	if isUTF {
		if r.hasSize {
			return nil, compileError(r.pos, "utf segments must not have a size")
		}
		if ts.unitSet {
			return nil, compileError(r.pos, "utf segments must not have a unit")
		}
	}
	if seg.typ == typeBitstring && ts.unitSet && ts.unit != 1 {
		return nil, compileError(r.pos, "unit of a bitstring segment is always 1")
	}

	// — size
	switch {
	case r.hasSize:
		if r.sizeIsVar {
			seg.width = -1 // dynamic: the value is taken during matching
		} else {
			if r.sizeVal > uint64(mathMaxInt64)/uint64(seg.unit) {
				return nil, compileError(r.pos, "segment size too large")
			}
			seg.width = int64(r.sizeVal) * int64(seg.unit)
		}
	case !isUTF:
		switch seg.typ {
		case typeInteger:
			seg.width = 8 // the default size
		case typeFloat:
			seg.width = 64
		case typeBinary, typeBitstring:
			if !last {
				return nil, compileError(r.pos, "a binary or bitstring segment without a size is only allowed at the end of the pattern")
			}
			seg.isTail = true
		}
	}

	// — width checks per type
	if seg.width >= 0 {
		switch seg.typ {
		case typeInteger:
			if seg.width > 64 {
				return nil, compileError(r.pos, "integer segment wider than 64 bits is not supported")
			}
		case typeFloat:
			if seg.width != 16 && seg.width != 32 && seg.width != 64 {
				return nil, compileError(r.pos, "float segment size must be 16, 32 or 64 bits, got %d", seg.width)
			}
		case typeBinary:
			if !seg.isTail && seg.width%8 != 0 {
				return nil, compileError(r.pos, "total width of a binary segment must be a multiple of 8, got %d", seg.width)
			}
		}
	}

	// — values: which literals are allowed in which segments
	switch r.valueKind {
	case vInt:
		if seg.typ != typeInteger {
			return nil, compileError(r.pos, "integer literal is only allowed in integer segments, not %s", seg.typ)
		}
		seg.kind = segLitInt
		seg.litInt = r.intVal
		if seg.width >= 0 {
			if err := checkIntRange(seg); err != nil {
				return nil, compileError(r.pos, "%s", err)
			}
		}
		// variable width: the range cannot be checked statically — it is compared at match time
	case vFloat:
		if seg.typ != typeFloat {
			return nil, compileError(r.pos, "float literal requires an explicit /float segment type, got %s", seg.typ)
		}
		seg.kind = segLitFloat
		seg.litFloat = r.floatVal
	case vAnon:
		seg.kind = segSkip
	case vVar:
		seg.kind = segBind
	}
	return seg, nil
}

// checkIntRange: a constant integer literal must fit the interpretation of
// the segment (unsigned: [0, 2^W); signed: [0, 2^(W-1)); literals are
// non-negative).
func checkIntRange(seg *cseg) error {
	w := seg.width
	if w == 0 {
		if seg.litInt == 0 {
			return nil
		}
		return fmt.Errorf("integer literal %d does not fit a 0-bit segment", seg.litInt)
	}
	if seg.signed {
		if seg.litInt >= uint64(1)<<uint(w-1) {
			return fmt.Errorf("integer literal %d does not fit a signed %d-bit segment", seg.litInt, w)
		}
	} else if w < 64 && seg.litInt >= uint64(1)<<uint(w) {
		return fmt.Errorf("integer literal %d does not fit an unsigned %d-bit segment", seg.litInt, w)
	}
	return nil
}

// parseTypeSpecItems validates the specifier list of a raw segment: the
// groups (type, sign, byte order), duplicates within a group and unknown
// names.
func parseTypeSpecItems(r rawSegment) (*typeSpec, error) {
	ts := &typeSpec{}
	count := map[string]int{}
	for _, it := range r.specItems {
		group := "type"
		if it.isUnit {
			group = "unit"
			ts.unitSet = true
			ts.unit = it.unit
		} else {
			switch it.text {
			case "integer", "float", "binary", "bytes", "bitstring", "bits", "utf8", "utf16", "utf32":
				ts.typSet = true
				ts.typ = typeByName(it.text)
			case "signed", "unsigned":
				group = "signedness"
				ts.signSet = true
				ts.signed = it.text == "signed"
			case "big", "little":
				group = "endianness"
				ts.endianSet = true
				ts.little = it.text == "little"
			case "native":
				return nil, compileError(it.pos, "endianness %q is not supported (machine-dependent); use big or little", it.text)
			default:
				return nil, compileError(it.pos, "unknown type specifier %q", it.text)
			}
		}
		count[group]++
		if count[group] > 1 {
			return nil, compileError(it.pos, "duplicate %s specifier", group)
		}
	}
	return ts, nil
}

func typeByName(name string) segType {
	switch name {
	case "integer":
		return typeInteger
	case "float":
		return typeFloat
	case "binary", "bytes":
		return typeBinary
	case "bitstring", "bits":
		return typeBitstring
	case "utf8":
		return typeUTF8
	case "utf16":
		return typeUTF16
	case "utf32":
		return typeUTF32
	}
	return typeInteger // unreachable: only called for known names
}

const mathMaxInt64 = int64(^uint64(0) >> 1)
