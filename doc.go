// Package elknitter parses binary data with Erlang bit-syntax patterns:
// a pattern is given as a string literal, the data as a []byte or an
// io.Reader, and the result is a map[string]any whose keys are the variable
// names of the pattern and whose values are the parsed data in Go types.
//
// # Pattern
//
// A pattern is an ordinary or multi-line string of the form
// <<Segment, Segment, ...>>. A segment is Value, Value:Size, Value/Types or
// Value:Size/Types, where Value is a variable, the anonymous variable `_`,
// an integer literal (decimal or 0x..), a float literal, or a string literal
// (expanded into bytes). Size is a literal or the name of a previously bound
// variable; the number of bits in a segment is Size×unit. Types after "/":
// integer (the default), float, binary (bytes), bitstring (bits), utf8,
// utf16, utf32, plus signed/unsigned, big/little and unit:N. A binary or
// bitstring without a size is allowed only as the last segment and absorbs
// the whole remaining input.
//
// Values: signed integer — int64; unsigned narrower than 63 bits — int64;
// unsigned 64 bits — uint64; float — float64; binary and bitstring whose
// length is a multiple of 8 bits — a copy []byte; bitstring of non-multiple
// length — Bits (the exact bit length); utf8/utf16/utf32 — rune. The input
// is not modified, and binary values do not alias it.
//
// # Example
//
// The classic length-prefix, the reference example from the Erlang
// documentation (erlang_binary_matching.md): the variable Len is bound to
// the first byte and sets the size of the Payload segment:
//
//	pattern := `<<Len:8, Payload:Len/binary, Rest/binary>>`
//	data := append([]byte{3}, []byte("abctail")...)
//	res, err := elknitter.Match(pattern, data)
//	// res["Len"]     == int64(3)
//	// res["Payload"] == []byte("abc")
//	// res["Rest"]    == []byte("tail")
//
// # Errors
//
// Compiling a pattern is done by Compile (errors of class ErrCompile).
// Matching is done by Matcher.Match / Matcher.MatchReader (a mismatch is
// ErrNoMatch, with details in *NoMatchError: the segment number and the
// reason; a read error from an io.Reader is ErrInput). The classes are
// distinguishable via errors.Is; a matcher is immutable and usable from
// several goroutines.
//
// # Limitations
//
//   - Integer literals and values are at most 64 bits wide (unlike Erlang
//     with its arbitrary precision); native endianness is not supported.
//   - Binary values in the result are independent copies of the input (safe
//     to keep and mutate after the match; the price is copying).
//   - Float literals are compared by exact IEEE equality: NaN matches no
//     literal, and 0.0 != -0.0.
package elknitter
