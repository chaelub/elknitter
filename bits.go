package elknitter

// The bit layer of matching: an MSB-first bit reader (Erlang's order),
// assembling values from arbitrary bit positions, byte order, sign. Bits is
// the result type for bitstrings whose length is not a multiple of 8.

import (
	"math"
	"math/bits"
)

// Bits — the value of a bit segment with an exact bit length that is not
// necessarily a multiple of 8. It holds a copy of the significant bits: they
// occupy the high bits of every byte (an MSB-first stream), and the low bits
// of the last byte are zero-padded.
type Bits struct {
	data  []byte
	count int
}

// Len returns the length in bits.
func (b Bits) Len() int { return b.count }

// Bytes returns a copy of the data: (count+7)/8 bytes, with the incomplete
// last byte zero-padded in its low bits.
func (b Bits) Bytes() []byte { return append([]byte(nil), b.data...) }

// bitReader moves through the input bit by bit; segments follow one another
// without byte alignment.
type bitReader struct {
	data []byte
	off  int64 // current bit position
}

func newBitReader(data []byte) *bitReader { return &bitReader{data: data} }

func (r *bitReader) remaining() int64 { return int64(len(r.data))*8 - r.off }

func (r *bitReader) skip(w int64) { r.off += w }

// takeBits reads w bits (0..64) as an unsigned integer: the first bit of the
// window is the most significant one (big-endian interpretation of the
// window). ok=false means there are not enough bits.
func (r *bitReader) takeBits(w int) (uint64, bool) {
	if w == 0 {
		return 0, true
	}
	if w > 64 || r.remaining() < int64(w) {
		return 0, false
	}
	// fast path: byte-aligned and w a multiple of 8
	if r.off&7 == 0 && w&7 == 0 && w >= 8 {
		base := r.off >> 3
		var v uint64
		for i := int64(0); i < int64(w)>>3; i++ {
			v = v<<8 | uint64(r.data[base+i])
		}
		r.off += int64(w)
		return v, true
	}
	var acc uint64
	rem := w
	pos := r.off
	for rem > 0 {
		b := r.data[pos>>3]
		s := int(pos & 7) // bit counted from the most significant: 0 = MSB
		take := 8 - s
		if take > rem {
			take = rem
		}
		// fragment: bits [s, s+take) of the byte, the first one most significant in acc
		frag := (b >> uint(8-s-take)) & byte(0xFF>>uint(8-take))
		acc = acc<<uint(take) | uint64(frag)
		rem -= take
		pos += int64(take)
	}
	r.off = pos
	return acc, true
}

// tailBits returns a copy of all remaining bits as Bits (significant bits in
// the high positions of the bytes, the last byte zero-padded). An aligned
// tail is copied directly; an unaligned one is assembled byte by byte.
func (r *bitReader) tailBits() Bits {
	count := r.remaining()
	if count == 0 {
		return Bits{count: 0}
	}
	n := int((count + 7) / 8) // full bytes needed for storage
	out := make([]byte, n)
	if r.off&7 == 0 {
		copy(out, r.data[r.off>>3:r.off>>3+int64(n)])
		r.skip(count)
	} else {
		full := n - 1
		if count&7 == 0 {
			full = n
		}
		for i := 0; i < full; i++ {
			v, _ := r.takeBits(8)
			out[i] = byte(v)
		}
		if full < n {
			last := int(count) - full*8 // 1..7 bits
			v, _ := r.takeBits(last)
			out[full] = byte(v << uint(8-last)) // significant bits in the high positions
		}
	}
	return Bits{data: out, count: int(count)}
}

// intValue interprets the raw bits of a window of w bits as an integer
// according to the byte order and the sign. It returns uint64 for unsigned
// w==64, and int64 otherwise.
func intValue(raw uint64, w int, signed, little bool) any {
	if little {
		raw = reverseWindow(raw, w)
	}
	if w == 0 {
		return int64(0)
	}
	if signed {
		return int64(raw) << (64 - w) >> (64 - w)
	}
	if w == 64 {
		return raw
	}
	return int64(raw)
}

// reverseWindow brings a window of w bits to little-endian interpretation: a
// multiple of 8 reverses the byte order, otherwise the whole window is
// reversed bit by bit.
func reverseWindow(raw uint64, w int) uint64 {
	if w%8 == 0 {
		var rev uint64
		for i, n := 0, w/8; i < n; i++ {
			rev = rev<<8 | (raw>>uint(8*i))&0xFF
		}
		return rev
	}
	return bits.Reverse64(raw) >> uint(64-w)
}

// floatValue interprets the raw bits of a window of w bits (16/32/64) as an
// IEEE-754 float.
func floatValue(raw uint64, w int, little bool) float64 {
	if little {
		raw = reverseWindow(raw, w)
	}
	switch w {
	case 16:
		return float64FromFloat16(uint16(raw))
	case 32:
		return float64(math.Float32frombits(uint32(raw)))
	default: // 64
		return math.Float64frombits(raw)
	}
}

// float64FromFloat16 widens an IEEE-754 half (1 sign / 5 exp / 10 mantissa
// bits) to float64 without loss: math.Float16frombits does not exist in the
// standard library.
func float64FromFloat16(u uint16) float64 {
	exp := uint16(u>>10) & 0x1F
	mant := uint16(u & 0x3FF)
	sign := float64(1)
	if u>>15 == 1 {
		sign = -1
	}
	switch {
	case exp == 0:
		return sign * math.Ldexp(float64(mant), -24) // subnormal (including +-0)
	case exp == 0x1F:
		if mant == 0 {
			return sign * math.Inf(1)
		}
		return math.NaN()
	default:
		return sign * math.Ldexp(float64(1024+int(mant)), int(exp)-15-10)
	}
}
