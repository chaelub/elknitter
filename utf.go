package elknitter

// Decoding UTF segments from the bitstream: utf8 (RFC 3629), utf16 (RFC 2781,
// surrogate pairs), utf32. An invalid sequence or a byte shortfall is an error
// (a mismatch); the result is a code point in the range
// 0..0xD7FF | 0xE000..0x10FFFF.

import "fmt"

// decUTF reads one code point of a utf8/utf16/utf32 segment.
func decUTF(r *bitReader, typ segType, little bool) (rune, error) {
	switch typ {
	case typeUTF8:
		return decUTF8(r)
	case typeUTF16:
		return decUTF16(r, little)
	default: // typeUTF32
		return decUTF32(r, little)
	}
}

// readByte reads the next byte of the stream (shifting when the position is
// unaligned). false — the byte is not available.
func readByte(r *bitReader) (byte, bool) {
	if r.remaining() < 8 {
		return 0, false
	}
	if r.off&7 == 0 {
		b := r.data[r.off>>3]
		r.skip(8)
		return b, true
	}
	v, ok := r.takeBits(8)
	return byte(v), ok
}

func decUTF8(r *bitReader) (rune, error) {
	b0, ok := readByte(r)
	if !ok {
		return 0, fmt.Errorf("need more bits to decode a utf8 character")
	}
	switch {
	case b0 < 0x80: // 1 byte
		return rune(b0), nil
	case b0 < 0xC2: // 0x80..0xC1: a continuation or an overlong lead
		return 0, fmt.Errorf("invalid utf8 lead byte 0x%02X", b0)
	case b0 < 0xE0: // 2 bytes: 0xC2..0xDF
		return utf8Seq(r, rune(b0&0x1F), 2, 0x80, 0xBF)
	case b0 < 0xF0: // 3 bytes: 0xE0..0xEF
		min, max := byte(0x80), byte(0xBF)
		if b0 == 0xE0 {
			min = 0xA0 // no overlong sequences (cp >= 0x800)
		} else if b0 == 0xED {
			max = 0x9F // no surrogates U+D800..U+DFFF
		}
		return utf8Seq(r, rune(b0&0x0F), 3, min, max)
	default: // 4 bytes: 0xF0..0xF4
		if b0 > 0xF4 {
			return 0, fmt.Errorf("invalid utf8 lead byte 0x%02X", b0)
		}
		min, max := byte(0x80), byte(0xBF)
		if b0 == 0xF0 {
			min = 0x90 // cp >= 0x10000
		} else if b0 == 0xF4 {
			max = 0x8F // cp <= 0x10FFFF
		}
		return utf8Seq(r, rune(b0&0x07), 4, min, max)
	}
}

// utf8Seq reads the remaining n-1 continuation bytes: the first one must lie
// in [min, max] (the ranges of the special leads), the rest in [0x80, 0xBF].
func utf8Seq(r *bitReader, cp rune, n int, min, max byte) (rune, error) {
	for i := 1; i < n; i++ {
		b, ok := readByte(r)
		if !ok {
			return 0, fmt.Errorf("need more bits to decode a utf8 character")
		}
		lo, hi := byte(0x80), byte(0xBF)
		if i == 1 {
			lo, hi = min, max
		}
		if b < lo || b > hi {
			return 0, fmt.Errorf("invalid utf8 continuation byte 0x%02X", b)
		}
		cp = cp<<6 | rune(b&0x3F)
	}
	return cp, nil
}

// unit16 reads a 16-bit unit honoring the byte order.
func unit16(r *bitReader, little bool) (uint16, bool) {
	if r.remaining() < 16 {
		return 0, false
	}
	raw, _ := r.takeBits(16)
	if little {
		raw = reverseWindow(raw, 16)
	}
	return uint16(raw), true
}

func decUTF16(r *bitReader, little bool) (rune, error) {
	u1, ok := unit16(r, little)
	if !ok {
		return 0, fmt.Errorf("need more bits to decode a utf16 character")
	}
	switch {
	case u1 >= 0xD800 && u1 <= 0xDBFF: // high surrogate: a pair is mandatory
		u2, ok := unit16(r, little)
		if !ok {
			return 0, fmt.Errorf("need more bits for the utf16 low surrogate")
		}
		if u2 < 0xDC00 || u2 > 0xDFFF {
			return 0, fmt.Errorf("utf16 high surrogate without a low surrogate")
		}
		return rune(u1-0xD800)<<10 | rune(u2-0xDC00) + 0x10000, nil
	case u1 >= 0xDC00 && u1 <= 0xDFFF:
		return 0, fmt.Errorf("utf16 lone low surrogate")
	default:
		return rune(u1), nil
	}
}

func decUTF32(r *bitReader, little bool) (rune, error) {
	// the 4 bytes are read as a big-endian window; little reverses the window
	hi, ok := unit16(r, false)
	if !ok {
		return 0, fmt.Errorf("need more bits to decode a utf32 character")
	}
	lo, ok := unit16(r, false)
	if !ok {
		return 0, fmt.Errorf("need more bits to decode a utf32 character")
	}
	raw := uint64(hi)<<16 | uint64(lo)
	if little {
		raw = reverseWindow(raw, 32)
	}
	cp := rune(raw)
	if cp > 0x10FFFF || cp >= 0xD800 && cp <= 0xDFFF {
		return 0, fmt.Errorf("utf32 code point out of the Unicode range")
	}
	return cp, nil
}
