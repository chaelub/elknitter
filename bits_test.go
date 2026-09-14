package elknitter

import (
	"math"
	"reflect"
	"testing"
)

// Reading N bits from byte-aligned and unaligned positions, scenario
// «Без байтового выравнивания» (spec matching-engine).
func TestBitReaderAlignedAndUnaligned(t *testing.T) {
	data := []byte{0xAB, 0xCD}

	// from position 0: 4 bits, then 12 — segments follow one another unaligned
	r := newBitReader(data)
	a, ok := r.takeBits(4)
	if !ok || a != 0xA {
		t.Errorf("A = %#x, ok=%v", a, ok)
	}
	b, ok := r.takeBits(12)
	if !ok || b != 0xBCD {
		t.Errorf("B = %#x, ok=%v; want 0xBCD", b, ok)
	}
	if r.remaining() != 0 {
		t.Errorf("remaining = %d, want 0", r.remaining())
	}

	// from position 4 (unaligned): 8 bits
	r = newBitReader(data)
	if _, ok := r.takeBits(4); !ok {
		t.Fatal("4 bits were not available")
	}
	b, ok = r.takeBits(8)
	if !ok || b != 0xBC {
		t.Errorf("B = %#x, want 0xBC", b)
	}

	// spanning three bytes: 16 bits starting at bit 4 in the stream 0x01 0x02 0x03 0x04
	data = []byte{0x01, 0x02, 0x03, 0x04}
	r = newBitReader(data)
	if _, ok := r.takeBits(4); !ok {
		t.Fatal("not enough bits")
	}
	// window b4..b19: b4..b7 = low nibble of byte0 (0001) + byte1 (00000010)
	// + high nibble of byte2 (0000) = 0001 00000010 0000 = 0x1020
	v, ok := r.takeBits(16)
	if !ok || v != 0x1020 {
		t.Errorf("v = %#x, want 0x1020", v)
	}
}

// Extreme widths: 64 bits and 0 bits; skip must not move the read position by mistake.
func TestBitReaderWideAndZero(t *testing.T) {
	data := make([]byte, 8)
	for i := range data {
		data[i] = byte(i + 1)
	}
	r := newBitReader(data)
	v, ok := r.takeBits(64)
	if !ok || v != 0x0102030405060708 {
		t.Errorf("64 bits = %#x", v)
	}
	r = newBitReader(data)
	v, ok = r.takeBits(0)
	if !ok || v != 0 {
		t.Errorf("0 bits = %#x, ok=%v", v, ok)
	}
	if r.remaining() != 64 {
		t.Errorf("remaining = %d, want 64 (0 bits consumed nothing)", r.remaining())
	}

	// 9 bits from bit 7 in the stream 0x01 0x80: b7 = 1 (the last bit of byte 0),
	// b8..b15 = 10000000: window = 1<<8 | 0x80
	r = newBitReader([]byte{0x01, 0x80})
	if _, ok := r.takeBits(7); !ok {
		t.Fatal("not enough bits")
	}
	v, ok = r.takeBits(9)
	if !ok || v != 0x180 {
		t.Errorf("9 bits = %#x, want 0x180", v)
	}
	// 64 bits from bit 1: the 0xAA prefix bit shifts every data byte
	// by one bit position (the bits of 0xAA: 0101010 come first)
	r = newBitReader(append([]byte{0xAA}, []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}...))
	if _, ok := r.takeBits(1); !ok {
		t.Fatal("not enough bits")
	}
	v, ok = r.takeBits(64)
	if !ok || v != 0x54020406080A0C0E {
		t.Errorf("64 bits from an unaligned position = %#x", v)
	}
}

// Task 3.3: a bit shortfall is a flag, not a panic.
func TestBitReaderShortfall(t *testing.T) {
	r := newBitReader([]byte{0xFF})
	for _, w := range []int{9, 64, 8} {
		if w > 8 {
			if _, ok := r.takeBits(w); ok {
				t.Errorf("takeBits(%d) ok=true, want false", w)
			}
		}
	}
	// after exhaustion
	if _, ok := r.takeBits(8); !ok {
		t.Fatal("the first read of 8 bits must succeed")
	}
	if r.remaining() != 0 {
		t.Fatalf("remaining = %d", r.remaining())
	}
	if _, ok := r.takeBits(1); ok {
		t.Error("a read past the end of the input must return false")
	}
}

// Endianness: little for multiples of 8 reverses the bytes; for non-multiples
// it reverses the window.
func TestReverseWindow(t *testing.T) {
	// 16 bits: the bytes 0x02 0x03 in little = 0x0302
	if got := reverseWindow(0x0203, 16); got != 0x0302 {
		t.Errorf("little 16 = %#x, want 0x0302", got)
	}
	// 32 bits
	if got := reverseWindow(0x01020304, 32); got != 0x04030201 {
		t.Errorf("little 32 = %#x", got)
	}
	// not a multiple of 8: 12 bits 0xABC → bitwise reversal
	if got := reverseWindow(0xABC, 12); got != 0x3D5 {
		t.Errorf("little 12 = %#x, want 0x3D5", got)
	}
	// 4 bits
	if got := reverseWindow(0xA, 4); got != 0x5 {
		t.Errorf("little 4 = %#x, want 0x5", got)
	}
	// 64: a full reversal
	if got := reverseWindow(0x0102030405060708, 64); got != 0x0807060504030201 {
		t.Errorf("little 64 = %#x", got)
	}
}

// Sign: two's complement extension; unsigned narrower than 64 — int64;
// 64 — uint64.
func TestIntValueSign(t *testing.T) {
	if v := intValue(0xFF, 8, true, false); v != int64(-1) {
		t.Errorf("signed 0xFF = %v", v)
	}
	if v := intValue(0xFF, 8, false, false); v != int64(255) {
		t.Errorf("unsigned 0xFF = %v", v)
	}
	if v := intValue(0x7F, 8, true, false); v != int64(127) {
		t.Errorf("signed 0x7F = %v", v)
	}
	if v := intValue(0, 0, true, false); v != int64(0) {
		t.Errorf("W=0 signed = %v", v)
	}
	if v := intValue(0, 0, false, false); v != int64(0) {
		t.Errorf("W=0 unsigned = %v", v)
	}
	// unsigned 64 bits → uint64
	if v := intValue(0xFFFFFFFFFFFFFFFF, 64, false, false); v != uint64(0xFFFFFFFFFFFFFFFF) {
		t.Errorf("unsigned 64 = %v", v)
	}
	// signed 64: no extension needed — the full int64 range
	if v := intValue(0xFFFFFFFFFFFFFFFF, 64, true, false); v != int64(-1) {
		t.Errorf("signed 64 = %v", v)
	}
	// little applies before the sign: the bytes FE FF in the stream (raw 0xFEFF)
	// are the little-endian representation of -2 (FF FE with the high byte last)
	if v := intValue(0xFEFF, 16, true, true); v != int64(-2) {
		t.Errorf("little signed = %v; want -2", v)
	}
	// sign extension with the unaligned width 12: 0xFFF → -1
	if v := intValue(0xFFF, 12, true, false); v != int64(-1) {
		t.Errorf("signed 12 = %v", v)
	}
}

// float: 16/32/64 bits into float64 (design D10).
func TestFloatValue(t *testing.T) {
	// 1.5 as float32: 0x3FC00000
	if v := floatValue(0x3FC00000, 32, false); v != 1.5 {
		t.Errorf("float32 = %v", v)
	}
	if v := floatValue(0x3FF8000000000000, 64, false); v != 1.5 {
		t.Errorf("float64 = %v", v)
	}
	// float16 1.5: 0x3E00
	if v := floatValue(0x3E00, 16, false); v != 1.5 {
		t.Errorf("float16 1.5 = %v", v)
	}
	// float16: -2.0 = 0xC000; subnormal 2^-24 = 0x0001; inf; NaN
	if v := floatValue(0xC000, 16, false); v != -2.0 {
		t.Errorf("float16 -2 = %v", v)
	}
	if v := floatValue(0x0001, 16, false); v != math.Ldexp(1, -24) {
		t.Errorf("float16 min subnormal = %v", v)
	}
	if v := floatValue(0x7C00, 16, false); !math.IsInf(v, 1) {
		t.Errorf("float16 +inf = %v", v)
	}
	if v := floatValue(0x7E00, 16, false); !math.IsNaN(v) {
		t.Errorf("float16 NaN = %v", v)
	}
	// little: the bytes 0x00 0x3E (LE half 1.5) → raw big = 0x003E → reverse → 0x3E00
	if v := floatValue(0x003E, 16, true); v != 1.5 {
		t.Errorf("float16 little = %v", v)
	}
}

// Bits: Len and Bytes, including the incomplete last byte with zero padding.
func TestBitsValue(t *testing.T) {
	r := newBitReader([]byte{0xAB, 0xCD})
	if _, ok := r.takeBits(4); !ok {
		t.Fatal("not enough bits")
	}
	b := r.tailBits()
	if b.Len() != 12 {
		t.Errorf("Len = %d, want 12", b.Len())
	}
	want := []byte{0xBC, 0xD0} // bits 4..15 of the stream + 4 padding zeros
	if !reflect.DeepEqual(b.Bytes(), want) {
		t.Errorf("Bytes = %#x, want %#x", b.Bytes(), want)
	}
	// mutating the copy does not corrupt Bits (Bytes returns a fresh copy)
	got := b.Bytes()
	got[0] = 0
	if b.Bytes()[0] != 0xBC {
		t.Error("Bits.Bytes must return an independent copy")
	}
	// empty tail
	r = newBitReader([]byte{})
	if tb := r.tailBits(); tb.Len() != 0 || len(tb.Bytes()) != 0 {
		t.Errorf("empty tailBits = %+v", tb)
	}
	// aligned tail
	r = newBitReader([]byte{1, 2, 3})
	tb := r.tailBits()
	if tb.Len() != 24 || !reflect.DeepEqual(tb.Bytes(), []byte{1, 2, 3}) {
		t.Errorf("aligned tail = %+v", tb)
	}
}
