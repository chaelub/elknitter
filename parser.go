package elknitter

// Parsing a pattern into the intermediate representation rawSegment. Only the
// structure is checked here (segment forms, separators, brackets); the
// semantics of types and specifiers live in compile.go. See spec
// pattern-language.

import (
	"fmt"
	"strconv"
)

type valueKind uint8

const (
	vVar valueKind = iota
	vAnon
	vInt
	vFloat
	vString
)

type rawSegment struct {
	valueKind valueKind
	name      string  // vVar
	intVal    uint64  // vInt
	floatVal  float64 // vFloat
	strBytes  []byte  // vString — the decoded bytes of the string literal

	hasSize   bool
	sizeIsVar bool
	sizeVar   string // the name of the size variable
	sizeVal   uint64 // the literal size (in units)

	specItems []specItem // the elements of the specifier list after '/'
	pos       int        // the offset of the segment start
}

// specItem — one element of the specifier list before it is split on dashes
// (compile.go does the splitting): either a plain name or unit:N.
type specItem struct {
	text   string // the specifier name
	isUnit bool
	unit   uint64 // the N of unit:N
	pos    int
}

// segParser — a scanner with one-token pushback: it can look at a separator
// without consuming it.
type segParser struct {
	s   *scanner
	buf *token
}

func (p *segParser) next() (token, error) {
	if p.buf != nil {
		t := *p.buf
		p.buf = nil
		return t, nil
	}
	return p.s.next()
}

func (p *segParser) push(t token) { p.buf = &t }

// parseSegments parses the pattern string. String literals stay in the
// segments as they are (vString): expanding them into byte segments is done
// by compile.go together with the rest of the verification.
func parseSegments(src string) ([]rawSegment, error) {
	p := &segParser{s: newScanner(src)}

	first, err := p.next()
	if err != nil {
		return nil, err
	}
	if first.kind == tEOF {
		return nil, compileError(first.off, "pattern must be enclosed in << and >>")
	}
	if first.kind != tOpen {
		return nil, compileError(first.off, "pattern must start with <<")
	}

	var segs []rawSegment
	for {
		token, err := p.next()
		if err != nil {
			return nil, err
		}
		switch token.kind {
		case tClose:
			tail, err := p.next()
			if err != nil {
				return nil, err
			}
			if tail.kind != tEOF {
				return nil, compileError(tail.off, "unexpected %s after >>", tail)
			}
			return segs, nil
		case tEOF:
			return nil, compileError(token.off, "pattern must end with >>")
		case tComma:
			return nil, compileError(token.off, "empty segment between commas")
		}

		seg, err := parseSegment(p, token)
		if err != nil {
			return nil, err
		}
		segs = append(segs, seg)

		sep, err := p.next()
		if err != nil {
			return nil, err
		}
		switch sep.kind {
		case tComma:
			// a segment after a comma is mandatory: no trailing comma
			nxt, err := p.next()
			if err != nil {
				return nil, err
			}
			switch nxt.kind {
			case tClose, tEOF:
				return nil, compileError(sep.off, "trailing comma is not allowed")
			case tComma:
				return nil, compileError(nxt.off, "empty segment between commas")
			}
			p.push(nxt)
		case tClose:
			// push the separator back: the loop will handle tClose and finish
			p.push(sep)
		default:
			return nil, compileError(sep.off, "expected ',' or '>>' after segment, got %s", sep)
		}
	}
}

// parseSegment parses a single segment of the form Value[:Size][/TypeSpecifierList].
// The first token (the segment value) has already been read and is passed in
// v. The separator after the segment is not read by the parser — parseSegments
// handles it.
func parseSegment(p *segParser, v token) (rawSegment, error) {
	seg := rawSegment{pos: v.off}

	switch v.kind {
	case tIdent:
		seg.valueKind = vVar
		seg.name = v.text
	case tUnderscore:
		seg.valueKind = vAnon
	case tInt:
		val, err := parseIntLit(v.text)
		if err != nil {
			return rawSegment{}, compileError(v.off, "%s", err)
		}
		seg.valueKind = vInt
		seg.intVal = val
	case tFloat:
		f, err := strconv.ParseFloat(v.text, 64)
		if err != nil {
			return rawSegment{}, compileError(v.off, "invalid float literal %q", v.text)
		}
		seg.valueKind = vFloat
		seg.floatVal = f
	case tString:
		seg.valueKind = vString
		seg.strBytes = []byte(v.text)
	default:
		return rawSegment{}, compileError(v.off, "unexpected %s as segment value", v)
	}

	// optional :Size
	tk, err := p.next()
	if err != nil {
		return rawSegment{}, err
	}
	if tk.kind == tColon {
		sz, err := p.next()
		if err != nil {
			return rawSegment{}, err
		}
		switch sz.kind {
		case tInt:
			val, err := parseIntLit(sz.text)
			if err != nil {
				return rawSegment{}, compileError(sz.off, "%s", err)
			}
			seg.hasSize = true
			seg.sizeVal = val
		case tIdent:
			seg.hasSize = true
			seg.sizeIsVar = true
			seg.sizeVar = sz.text
		default:
			return rawSegment{}, compileError(sz.off, "segment size must be a non-negative integer or a previously bound variable, got %s", sz)
		}
		tk, err = p.next()
		if err != nil {
			return rawSegment{}, err
		}
	}

	// optional /TypeSpecifierList
	if tk.kind == tSlash {
		items, sep, err := parseTypeSpec(p)
		if err != nil {
			return rawSegment{}, err
		}
		seg.specItems = items
		if sep.kind != tComma && sep.kind != tClose {
			return rawSegment{}, compileError(sep.off, "expected ',' or '>>' after segment, got %s", sep)
		}
		p.push(sep)
		return seg, nil
	}
	// otherwise tk is the segment separator
	if tk.kind != tComma && tk.kind != tClose && tk.kind != tEOF {
		return rawSegment{}, compileError(tk.off, "expected ',', '/' or '>>' after segment value, got %s", tk)
	}
	p.push(tk)
	return seg, nil
}

// parseTypeSpec parses the specifier list after '/': the elements are
// separated by the '-' token, and an element is either a name or unit:N. It
// returns the list and the separator (',' or '>>') that ended it; a list that
// is not terminated is a syntax error.
func parseTypeSpec(p *segParser) ([]specItem, token, error) {
	var items []specItem
	for {
		tk, err := p.next()
		if err != nil {
			return nil, token{}, err
		}
		if tk.kind != tIdent {
			return nil, token{}, compileError(tk.off, "expected type specifier, got %s", tk)
		}
		if tk.text == "unit" {
			c, err := p.next()
			if err != nil {
				return nil, token{}, err
			}
			if c.kind != tColon {
				return nil, token{}, compileError(c.off, "expected ':' after unit")
			}
			n, err := p.next()
			if err != nil {
				return nil, token{}, err
			}
			if n.kind != tInt {
				return nil, token{}, compileError(n.off, "unit value must be an integer, got %s", n)
			}
			val, err := parseIntLit(n.text)
			if err != nil {
				return nil, token{}, compileError(n.off, "%s", err)
			}
			items = append(items, specItem{isUnit: true, unit: val, pos: tk.off})
		} else {
			items = append(items, specItem{text: tk.text, pos: tk.off})
		}

		sep, err := p.next()
		if err != nil {
			return nil, token{}, err
		}
		switch sep.kind {
		case tDash:
			// the next element
		case tComma, tClose, tEOF:
			return items, sep, nil
		default:
			return nil, token{}, compileError(sep.off, "unexpected %s in type specifier list", sep)
		}
	}
}

// parseIntLit parses an integer literal: decimal or 0x/0X hex.
func parseIntLit(text string) (uint64, error) {
	base, digits := 10, text
	if len(text) > 2 && text[0] == '0' && (text[1] == 'x' || text[1] == 'X') {
		base, digits = 16, text[2:]
	}
	val, err := strconv.ParseUint(digits, base, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid integer literal %q", text)
	}
	return val, nil
}
