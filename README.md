# elknitter — binary matching with Erlang patterns

> This library was written using DeepSeek V4 Flash.

A Go library on the standard library alone: it parses binary data left to
right at the bit level, driven by Erlang [bit syntax][erlang] patterns.
A pattern is an ordinary or multiline string, the data is accepted as
`[]byte` or `io.Reader`, and the result is a `map[string]any`: the keys are
the pattern's variable names and the values are the parsed data in Go types.

[erlang]: https://www.erlang.org/doc/system/bit_syntax.html

```go
pattern := `<<Len:8, Payload:Len/binary, Rest/binary>>`
data := append([]byte{3}, []byte("abcхвост")...)

res, err := elknitter.Match(pattern, data)
// res["Len"]     == int64(3)
// res["Payload"] == []byte("abc")
// res["Rest"]    == []byte("хвост")
```

## Features

- Segments `Value`, `Value:Size`, `Value/Types`, `Value:Size/Types`;
  the size is a literal or a previously bound variable (length-prefix).
- Types: `integer` (the default), `float`, `binary`/`bytes`,
  `bitstring`/`bits`, `utf8`, `utf16`, `utf32`; `signed`/`unsigned`,
  `big`/`little`, `unit:N`.
- String literals expand into bytes — handy prefixes such as
  `<<"GET ", Path/binary>>`.
- A tail `binary`/`bitstring` at the end swallows the whole remaining input;
  without one the input must be exhausted exactly at the end of the pattern.
- Precise error classes: `ErrCompile` (the pattern), `ErrNoMatch` (the data,
  with details in `*NoMatchError`), `ErrInput` (reading an `io.Reader`).

## API

| Function | Purpose |
| --- | --- |
| `Compile(pattern)` | compiles into a `*Matcher` (once, match many times) |
| `Match(pattern, data)` | compile + match a `[]byte` |
| `MatchReader(pattern, r)` | compile + match an `io.Reader` |
| `(*Matcher).Match(data)` | match a `[]byte` |
| `(*Matcher).MatchReader(r)` | match an `io.Reader` |

A matcher is immutable after compilation: it is safe for concurrent use, and
an `io.Reader` is read in full before matching starts.

## Result values

| Segment | Go type |
| --- | --- |
| integer `signed` | `int64` |
| integer `unsigned` ≤ 63 bits | `int64` |
| integer `unsigned` 64 bits | `uint64` |
| float (16/32/64) | `float64` |
| binary / bitstring of a length divisible by 8 | `[]byte` (a copy) |
| bitstring of any other length | `Bits` (`Len()`, `Bytes()`) |
| utf8 / utf16 / utf32 | `rune` |

## Limitations

- Integers are at most 64 bits wide (in Erlang they have arbitrary
  precision); the `native` byte order is not supported.
- Binary result values are independent copies of the input: the input is
  never modified and the result is safe to keep, at the cost of copying.
- Float literals are compared with exact IEEE equality: NaN matches no
  literal at all.

## Development

```sh
go test ./... -race -count=1
```

The specification and the tasks live in `openspec/changes/erlang-binary-match/`.
