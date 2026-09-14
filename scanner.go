package elknitter

// The lexer for pattern strings. It works byte by byte (a pattern holds ASCII
// tokens and string literals, and the latter may carry arbitrary bytes). See
// spec pattern-language: the tokens << >> , : / , names [A-Za-z][A-Za-z0-9_]*,
// the anonymous variable _, integers (dec/hex), floats, and double-quoted
// strings with escapes.

import "fmt"

type tokenKind uint8

const (
	tEOF        tokenKind = iota
	tOpen                 // <<
	tClose                // >>
	tComma                // ,
	tColon                // :
	tSlash                // /
	tDash                 // -
	tUnderscore           // _
	tIdent                // a variable [A-Za-z][A-Za-z0-9_]*
	tInt                  // an integer: dec or 0x-hex
	tFloat                // a number with a dot or an exponent
	tString               // a string literal; text is the decoded bytes
)

type token struct {
	kind  tokenKind
	text  string // ident/int/float: as in the source; string: the decoded bytes
	off   int    // byte offset of the token start in the pattern string
	isVar bool   // tIdent is a variable name (not a specifier)
}

func (t token) String() string {
	if t.kind == tString {
		return fmt.Sprintf("%q", t.text)
	}
	if t.text != "" {
		return t.text
	}
	switch t.kind {
	case tEOF:
		return "end of pattern"
	case tOpen:
		return "<<"
	case tClose:
		return ">>"
	case tComma:
		return ","
	case tColon:
		return ":"
	case tSlash:
		return "/"
	case tUnderscore:
		return "_"
	}
	return "token"
}

// compileError builds a compile error of class ErrCompile with a position.
func compileError(off int, format string, args ...any) error {
	return fmt.Errorf("elknitter: %w: %s at byte offset %d", ErrCompile, fmt.Sprintf(format, args...), off)
}

type scanner struct {
	src string
	off int
}

func newScanner(src string) *scanner { return &scanner{src: src} }

func isIdentStart(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }
func isIdentPart(b byte) bool  { return isIdentStart(b) || b >= '0' && b <= '9' || b == '_' }
func isDigit(b byte) bool      { return b >= '0' && b <= '9' }
func isHexDigit(b byte) bool   { return isDigit(b) || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F' }
func isWhitespace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

func (s *scanner) peek() byte {
	if s.off >= len(s.src) {
		return 0
	}
	return s.src[s.off]
}

// skipSpace skips spaces and newlines between tokens.
func (s *scanner) skipSpace() {
	for s.off < len(s.src) && isWhitespace(s.src[s.off]) {
		s.off++
	}
}

// next returns the next token, or tEOF at the end. Every lexical error
// (unexpected character, malformed number, bad escape, unterminated string)
// is returned as an error of class ErrCompile.
func (s *scanner) next() (token, error) {
	s.skipSpace()
	if s.off >= len(s.src) {
		return token{kind: tEOF, off: s.off}, nil
	}
	start := s.off
	c := s.src[s.off]
	switch {
	case c == '<':
		if s.off+1 < len(s.src) && s.src[s.off+1] == '<' {
			s.off += 2
			return token{kind: tOpen, off: start}, nil
		}
		return token{}, compileError(start, "unexpected character %q, want %q", c, "<<")
	case c == '>':
		if s.off+1 < len(s.src) && s.src[s.off+1] == '>' {
			s.off += 2
			return token{kind: tClose, off: start}, nil
		}
		return token{}, compileError(start, "unexpected character %q, want %q", c, ">>")
	case c == ',':
		s.off++
		return token{kind: tComma, off: start}, nil
	case c == ':':
		s.off++
		return token{kind: tColon, off: start}, nil
	case c == '/':
		s.off++
		return token{kind: tSlash, off: start}, nil
	case c == '-':
		s.off++
		return token{kind: tDash, off: start}, nil
	case c == '_':
		if s.off+1 < len(s.src) && isIdentPart(s.src[s.off+1]) {
			return token{}, compileError(start, "identifiers must not start with '_'; use '_' alone for an anonymous variable")
		}
		s.off++
		return token{kind: tUnderscore, off: start}, nil
	case isIdentStart(c):
		for s.off < len(s.src) && isIdentPart(s.src[s.off]) {
			s.off++
		}
		return token{kind: tIdent, text: s.src[start:s.off], off: start}, nil
	case isDigit(c):
		return s.scanNumber(start)
	case c == '"':
		return s.scanString(start)
	default:
		return token{}, compileError(start, "unexpected character %q", c)
	}
}

// scanNumber parses an integer (dec, 0x-hex) or a float. Digits after '.'
// and after the exponent are mandatory; a number must not run into a letter.
func (s *scanner) scanNumber(start int) (token, error) {
	isFloat := false
	// hex
	if s.src[s.off] == '0' && s.off+1 < len(s.src) && (s.src[s.off+1] == 'x' || s.src[s.off+1] == 'X') {
		s.off += 2
		if !(s.off < len(s.src) && isHexDigit(s.src[s.off])) {
			return token{}, compileError(start, "malformed hex literal")
		}
		for s.off < len(s.src) && isHexDigit(s.src[s.off]) {
			s.off++
		}
		if s.off < len(s.src) && isIdentPart(s.src[s.off]) {
			return token{}, compileError(start, "malformed number")
		}
		return token{kind: tInt, text: s.src[start:s.off], off: start}, nil
	}
	for s.off < len(s.src) && isDigit(s.src[s.off]) {
		s.off++
	}
	if s.off < len(s.src) && s.src[s.off] == '.' {
		isFloat = true
		s.off++
		if !(s.off < len(s.src) && isDigit(s.src[s.off])) {
			return token{}, compileError(start, "malformed float literal: digits required after '.'")
		}
		for s.off < len(s.src) && isDigit(s.src[s.off]) {
			s.off++
		}
	}
	if s.off < len(s.src) && (s.src[s.off] == 'e' || s.src[s.off] == 'E') {
		isFloat = true
		s.off++
		if s.off < len(s.src) && (s.src[s.off] == '+' || s.src[s.off] == '-') {
			s.off++
		}
		if !(s.off < len(s.src) && isDigit(s.src[s.off])) {
			return token{}, compileError(start, "malformed float literal: digits required after exponent")
		}
		for s.off < len(s.src) && isDigit(s.src[s.off]) {
			s.off++
		}
	}
	if s.off < len(s.src) && isIdentPart(s.src[s.off]) {
		return token{}, compileError(start, "malformed number")
	}
	if isFloat {
		return token{kind: tFloat, text: s.src[start:s.off], off: start}, nil
	}
	return token{kind: tInt, text: s.src[start:s.off], off: start}, nil
}

// scanString parses a string literal with the escapes \\ \" \n \t \r \xNN.
func (s *scanner) scanString(start int) (token, error) {
	s.off++ // the opening quote
	var out []byte
	for {
		if s.off >= len(s.src) {
			return token{}, compileError(start, "unterminated string literal")
		}
		c := s.src[s.off]
		switch {
		case c == '"':
			s.off++
			return token{kind: tString, text: string(out), off: start}, nil
		case c == '\\':
			s.off++
			if s.off >= len(s.src) {
				return token{}, compileError(start, "unterminated string literal")
			}
			e := s.src[s.off]
			switch e {
			case '\\':
				out = append(out, '\\')
			case '"':
				out = append(out, '"')
			case 'n':
				out = append(out, '\n')
			case 't':
				out = append(out, '\t')
			case 'r':
				out = append(out, '\r')
			case 'x':
				if s.off+2 >= len(s.src) || !isHexDigit(s.src[s.off+1]) || !isHexDigit(s.src[s.off+2]) {
					return token{}, compileError(start, "invalid \\x escape: two hex digits required")
				}
				out = append(out, hexVal(s.src[s.off+1])<<4|hexVal(s.src[s.off+2]))
				s.off += 2
			default:
				return token{}, compileError(start, "unknown escape sequence \\%c", e)
			}
			s.off++
		default:
			out = append(out, c)
			s.off++
		}
	}
}

func hexVal(b byte) byte {
	switch {
	case b >= '0' && b <= '9':
		return b - '0'
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10
	default:
		return b - 'A' + 10
	}
}
