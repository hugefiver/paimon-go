package ast

import (
	nativetypes "github.com/bytedance/sonic/internal/native/types"
	"strings"
)

// maxParseDepth mirrors Sonic's MAX_RECURSE limit (4096).
const maxParseDepth = 4096

// localParser is a recursive-descent JSON parser producing fully
// realized Node trees. It exists so string unescaping follows Sonic
// semantics (unpaired surrogates become U+FFFD; unknown escapes are
// errors instead of fastjson's best-effort literal passthrough) and the
// nesting limit matches Sonic's MAX_RECURSE (4096) rather than
// fastjson's MaxDepth (300).
type localParser struct {
	src string
	pos int
}

// parseRawToNodeLocal parses src into a Node tree using localParser.
func parseRawToNodeLocal(src string) (Node, nativetypes.ParsingError) {
	p := &localParser{src: src}
	p.skipSpace()
	if p.atEnd() {
		return Node{}, nativetypes.ERR_EOF
	}
	n, code := p.parseValue(0)
	if code != 0 {
		return Node{}, code
	}
	p.skipSpace()
	if !p.atEnd() {
		return Node{}, nativetypes.ERR_MISMATCH
	}
	return n, 0
}

func (p *localParser) atEnd() bool { return p.pos >= len(p.src) }

func (p *localParser) skipSpace() {
	for !p.atEnd() {
		switch p.src[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *localParser) parseValue(depth int) (Node, nativetypes.ParsingError) {
	if depth > maxParseDepth {
		return Node{}, nativetypes.ERR_RECURSE_EXCEED_MAX
	}
	if p.atEnd() {
		return Node{}, nativetypes.ERR_EOF
	}
	switch p.src[p.pos] {
	case '{':
		return p.parseObject(depth + 1)
	case '[':
		return p.parseArray(depth + 1)
	case '"':
		s, code := p.parseString()
		if code != 0 {
			return Node{}, code
		}
		return NewString(s), 0
	case 't':
		if code := p.parseLiteral("true"); code != 0 {
			return Node{}, code
		}
		return NewBool(true), 0
	case 'f':
		if code := p.parseLiteral("false"); code != 0 {
			return Node{}, code
		}
		return NewBool(false), 0
	case 'n':
		if code := p.parseLiteral("null"); code != 0 {
			return Node{}, code
		}
		return NewNull(), 0
	default:
		lit, code := p.parseNumber()
		if code != 0 {
			return Node{}, code
		}
		return NewNumber(lit), 0
	}
}

func (p *localParser) parseLiteral(lit string) nativetypes.ParsingError {
	if strings.HasPrefix(p.src[p.pos:], lit) {
		p.pos += len(lit)
		return 0
	}
	return nativetypes.ERR_INVALID_CHAR
}

func (p *localParser) parseObject(depth int) (Node, nativetypes.ParsingError) {
	p.pos++ // consume '{'
	p.skipSpace()
	var pairs []Pair
	if !p.atEnd() && p.src[p.pos] == '}' {
		p.pos++
		return NewObject(pairs), 0
	}
	for {
		p.skipSpace()
		if p.atEnd() || p.src[p.pos] != '"' {
			return Node{}, nativetypes.ERR_INVALID_CHAR
		}
		key, code := p.parseString()
		if code != 0 {
			return Node{}, code
		}
		p.skipSpace()
		if p.atEnd() || p.src[p.pos] != ':' {
			return Node{}, nativetypes.ERR_INVALID_CHAR
		}
		p.pos++
		p.skipSpace()
		val, code := p.parseValue(depth)
		if code != 0 {
			return Node{}, code
		}
		pairs = append(pairs, NewPair(key, val))
		p.skipSpace()
		if p.atEnd() {
			return Node{}, nativetypes.ERR_EOF
		}
		switch p.src[p.pos] {
		case ',':
			p.pos++
			continue
		case '}':
			p.pos++
			return NewObject(pairs), 0
		default:
			return Node{}, nativetypes.ERR_INVALID_CHAR
		}
	}
}

func (p *localParser) parseArray(depth int) (Node, nativetypes.ParsingError) {
	p.pos++ // consume '['
	p.skipSpace()
	var arr []Node
	if !p.atEnd() && p.src[p.pos] == ']' {
		p.pos++
		return NewArray(arr), 0
	}
	for {
		p.skipSpace()
		val, code := p.parseValue(depth)
		if code != 0 {
			return Node{}, code
		}
		arr = append(arr, val)
		p.skipSpace()
		if p.atEnd() {
			return Node{}, nativetypes.ERR_EOF
		}
		switch p.src[p.pos] {
		case ',':
			p.pos++
			continue
		case ']':
			p.pos++
			return NewArray(arr), 0
		default:
			return Node{}, nativetypes.ERR_INVALID_CHAR
		}
	}
}

// parseNumber scans a JSON number literal. Like Sonic it accepts
// leading-zero digit runs verbatim ("0123" stays "0123").
func (p *localParser) parseNumber() (string, nativetypes.ParsingError) {
	start := p.pos
	s := p.src
	if !p.atEnd() && s[p.pos] == '-' {
		p.pos++
	}
	if p.atEnd() || s[p.pos] < '0' || s[p.pos] > '9' {
		return "", nativetypes.ERR_INVALID_NUMBER_FMT
	}
	if s[p.pos] == '0' {
		p.pos++
		for !p.atEnd() && s[p.pos] >= '0' && s[p.pos] <= '9' {
			p.pos++
		}
	} else {
		for !p.atEnd() && s[p.pos] >= '0' && s[p.pos] <= '9' {
			p.pos++
		}
	}
	if !p.atEnd() && s[p.pos] == '.' {
		p.pos++
		if p.atEnd() || s[p.pos] < '0' || s[p.pos] > '9' {
			return "", nativetypes.ERR_INVALID_NUMBER_FMT
		}
		for !p.atEnd() && s[p.pos] >= '0' && s[p.pos] <= '9' {
			p.pos++
		}
	}
	if !p.atEnd() && (s[p.pos] == 'e' || s[p.pos] == 'E') {
		p.pos++
		if !p.atEnd() && (s[p.pos] == '+' || s[p.pos] == '-') {
			p.pos++
		}
		if p.atEnd() || s[p.pos] < '0' || s[p.pos] > '9' {
			return "", nativetypes.ERR_INVALID_NUMBER_FMT
		}
		for !p.atEnd() && s[p.pos] >= '0' && s[p.pos] <= '9' {
			p.pos++
		}
	}
	return s[start:p.pos], 0
}

// parseString parses a JSON string literal starting at the opening
// quote. Escapes follow Sonic: \uXXXX unpaired surrogates become
// U+FFFD; valid surrogate pairs combine; unknown escapes are
// ERR_INVALID_ESCAPE; raw control bytes are tolerated (documented
// Sonic-compatible leniency) and passed through unchanged.
func (p *localParser) parseString() (string, nativetypes.ParsingError) {
	p.pos++ // consume opening quote
	var b strings.Builder
	s := p.src
	for !p.atEnd() {
		c := s[p.pos]
		if c == '"' {
			p.pos++
			return b.String(), 0
		}
		if c == '\\' {
			p.pos++
			if p.atEnd() {
				return "", nativetypes.ERR_INVALID_ESCAPE
			}
			switch s[p.pos] {
			case '"', '\\', '/':
				b.WriteByte(s[p.pos])
				p.pos++
			case 'b':
				b.WriteByte('\b')
				p.pos++
			case 'f':
				b.WriteByte('\f')
				p.pos++
			case 'n':
				b.WriteByte('\n')
				p.pos++
			case 'r':
				b.WriteByte('\r')
				p.pos++
			case 't':
				b.WriteByte('\t')
				p.pos++
			case 'u':
				p.pos++
				r, code := p.parseUnicodeEscape()
				if code != 0 {
					return "", code
				}
				// High surrogate: try to combine with a following \uYYYY.
				if r >= 0xD800 && r <= 0xDBFF {
					if p.pos+1 < len(s) && s[p.pos] == '\\' && s[p.pos+1] == 'u' {
						save := p.pos
						p.pos += 2
						r2, code := p.parseUnicodeEscape()
						if code == 0 && r2 >= 0xDC00 && r2 <= 0xDFFF {
							r = 0x10000 + (r-0xD800)<<10 + (r2 - 0xDC00)
						} else {
							// Not a low surrogate: emit U+FFFD for the
							// high surrogate and re-process the second
							// escape from its start.
							p.pos = save
							r = 0xFFFD
						}
					} else {
						r = 0xFFFD
					}
				} else if r >= 0xDC00 && r <= 0xDFFF {
					// Lone low surrogate.
					r = 0xFFFD
				}
				b.WriteRune(r)
			default:
				return "", nativetypes.ERR_INVALID_ESCAPE
			}
			continue
		}
		b.WriteByte(c)
		p.pos++
	}
	return "", nativetypes.ERR_EOF
}

func (p *localParser) parseUnicodeEscape() (rune, nativetypes.ParsingError) {
	s := p.src
	if p.pos+4 > len(s) {
		return 0, nativetypes.ERR_INVALID_ESCAPE
	}
	hex := s[p.pos : p.pos+4]
	p.pos += 4
	var r rune
	for i := 0; i < 4; i++ {
		c := hex[i]
		var d rune
		switch {
		case c >= '0' && c <= '9':
			d = rune(c - '0')
		case c >= 'a' && c <= 'f':
			d = rune(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = rune(c-'A') + 10
		default:
			return 0, nativetypes.ERR_INVALID_ESCAPE
		}
		r = r<<4 | d
	}
	return r, 0
}

// unescapeSonicString unescapes raw JSON string contents (without the
// surrounding quotes) using Sonic semantics. It shares parseString's
// rules and is used when a raw string body must be decoded standalone.
func unescapeSonicString(raw string) (string, nativetypes.ParsingError) {
	p := &localParser{src: "\"" + raw + "\"", pos: 0}
	s, code := p.parseString()
	if code != 0 {
		return "", code
	}
	if !p.atEnd() {
		return "", nativetypes.ERR_INVALID_ESCAPE
	}
	return s, 0
}
