package elknitter

// The matching runtime: the loop over the compiled segments, binding values,
// comparing literals, the full-consumption rule. Errors are NoMatchError with
// a reason; no partial result is ever returned (atomicity).

import "fmt"

// matchData applies the compiled pattern to the data. On a mismatch it returns
// a *NoMatchError (class ErrNoMatch); the input is not modified.
func matchData(cp *compiledPattern, data []byte) (map[string]any, error) {
	vals := make([]any, len(cp.varNames))
	r := newBitReader(data)

	for i, seg := range cp.segs {
		// the tail segment: the remainder must be divisible by the unit
		if seg.isTail {
			rem := r.remaining()
			if rem%int64(seg.unit) != 0 {
				return nil, noMatch(i+1, "remaining %d bits are not divisible by unit %d of the tail segment", rem, seg.unit)
			}
			b := r.tailBits()
			var v any
			if b.Len()%8 == 0 {
				v = b.Bytes() // a byte-aligned remainder → []byte (a copy)
			} else {
				v = b // not a multiple of 8 → Bits with the exact length
			}
			if seg.kind == segBind {
				vals[seg.varSlot] = v
			}
			break // the tail is always the last segment
		}

		// the segment width: constant, or taken from a size variable
		w, err := segWidth(seg, cp, vals, i)
		if err != nil {
			return nil, err
		}

		var v any
		switch seg.typ {
		case typeInteger, typeFloat:
			raw, ok := r.takeBits(int(w))
			if !ok {
				return nil, noMatch(i+1, "need %d bits for segment %d, only %d left", w, i+1, r.remaining())
			}
			if seg.typ == typeFloat {
				v = floatValue(raw, int(w), seg.little)
			} else {
				v = intValue(raw, int(w), seg.signed, seg.little)
			}
		case typeBinary, typeBitstring:
			bv, err := takeBinaryValue(r, w)
			if err != nil {
				return nil, noMatch(i+1, "%s", err)
			}
			v = bv
		case typeUTF8, typeUTF16, typeUTF32:
			rn, err := decUTF(r, seg.typ, seg.little)
			if err != nil {
				return nil, noMatch(i+1, "%s", err)
			}
			v = rn
		}

		// literal comparison: the data is interpreted the same way as the segment
		if seg.kind == segLitInt {
			if !litIntMatches(v, seg.litInt, seg.signed) {
				return nil, noMatch(i+1, "expected literal 0x%X, got 0x%X", seg.litInt, v)
			}
		} else if seg.kind == segLitFloat {
			f := v.(float64)
			if f != seg.litFloat { // NaN is never equal — the literal does not match
				return nil, noMatch(i+1, "expected float literal %g, got %g", seg.litFloat, f)
			}
		} else if seg.kind == segBind {
			vals[seg.varSlot] = v
		}
	}

	// full consumption: without a tail segment the input must be exhausted
	if rem := r.remaining(); rem != 0 {
		return nil, noMatch(0, "%d bits left unconsumed after the end of the pattern", rem)
	}

	m := make(map[string]any, len(cp.varNames))
	for slot, name := range cp.varNames {
		m[name] = vals[slot]
	}
	return m, nil
}

// segWidth computes the width of a segment in bits: either the constant from
// compile, or size×unit from the bound variable. The size value must be a
// non-negative integer; excessive sizes are a mismatch.
func segWidth(seg *cseg, cp *compiledPattern, vals []any, segIdx int) (int64, error) {
	if seg.sizeIdx < 0 {
		return seg.width, nil
	}
	name := "size variable"
	if seg.sizeIdx < len(cp.varNames) {
		name = "size variable " + cp.varNames[seg.sizeIdx]
	}
	size, ok := asSize(vals[seg.sizeIdx])
	if !ok {
		return 0, noMatch(segIdx+1, "%s is not a non-negative integer", name)
	}
	if size > uint64(mathMaxInt64)/uint64(seg.unit) {
		return 0, noMatch(segIdx+1, "%s = %d × unit %d overflows", name, size, seg.unit)
	}
	w := int64(size) * int64(seg.unit)
	switch seg.typ {
	case typeInteger:
		if w > 64 {
			return 0, noMatch(segIdx+1, "integer segment wider than 64 bits (got %d)", w)
		}
	case typeFloat:
		if w != 16 && w != 32 && w != 64 {
			return 0, noMatch(segIdx+1, "float segment size must be 16, 32 or 64 bits, got %d", w)
		}
	case typeBinary:
		if w%8 != 0 {
			return 0, noMatch(segIdx+1, "binary segment width %d is not a multiple of 8", w)
		}
	}
	return w, nil
}

// asSize coerces a bound value to a non-negative integer (in units).
func asSize(v any) (uint64, bool) {
	switch n := v.(type) {
	case int64:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case uint64:
		return n, true
	}
	return 0, false
}

// takeBinaryValue reads w bits as the value of a binary/bitstring segment:
// a length that is a multiple of 8 gives a copy []byte; otherwise a Bits with
// the exact bit length (the incomplete last byte is aligned to the high bits).
func takeBinaryValue(r *bitReader, w int64) (any, error) {
	if w%8 == 0 {
		n := int(w / 8)
		if r.remaining() < int64(n)*8 {
			return nil, fmt.Errorf("need %d bits, only %d left", w, r.remaining())
		}
		out := make([]byte, n)
		if r.off&7 == 0 {
			copy(out, r.data[r.off>>3:r.off>>3+int64(n)])
			r.skip(w)
			return out, nil
		}
		for i := range out {
			v, _ := r.takeBits(8)
			out[i] = byte(v)
		}
		return out, nil
	}
	if r.remaining() < w {
		return nil, fmt.Errorf("need %d bits, only %d left", w, r.remaining())
	}
	n := int(w/8) + 1 // the last byte is incomplete
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		take := 8
		if left := w - int64(i*8); left < 8 {
			take = int(left)
		}
		v, _ := r.takeBits(take)
		out[i] = byte(v) << uint(8-take) // the incomplete byte goes to the high bits
	}
	return Bits{data: out, count: int(w)}, nil
}

// litIntMatches compares the interpreted integer v with the literal lit.
func litIntMatches(v any, lit uint64, signed bool) bool {
	if signed {
		if lit > uint64(mathMaxInt64) {
			return false // does not fit the signed range — never equal
		}
		return v.(int64) == int64(lit)
	}
	switch n := v.(type) {
	case int64:
		return uint64(n) == lit
	case uint64:
		return n == lit
	}
	return false
}

// noMatch builds a mismatch error with a reason and a 1-based segment number
// (0 — an error outside the segments, e.g. extra bits at the end).
func noMatch(segment int, format string, args ...any) error {
	return &NoMatchError{Segment: segment, Reason: fmt.Sprintf(format, args...)}
}
