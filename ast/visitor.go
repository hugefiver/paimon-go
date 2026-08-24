package ast

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	nativetypes "github.com/bytedance/sonic/internal/native/types"
)

// Preorder walks the JSON document at src in document order, calling
// the visitor callbacks before descending into containers.
//
// OnObjectBegin / OnArrayBegin may return VisitOPSkip to skip the
// container's children; the corresponding end callback is still called
// immediately after the begin callback in that case, matching Sonic's
// behavior of emitting end events even when children are skipped.
func Preorder(src string, visitor Visitor, opts *VisitorOptions) error {
	if visitor == nil {
		return errors.New("nil visitor")
	}
	onlyNumber := opts != nil && opts.OnlyNumber
	p := newPreorderParser(src, onlyNumber)
	return p.walk(visitor)
}

// preorderParser is a streaming JSON parser specialized for Preorder.
// It scans bytes once and emits visitor callbacks in document order.
type preorderParser struct {
	src        string
	pos        int
	onlyNumber bool
}

func newPreorderParser(src string, onlyNumber bool) *preorderParser {
	return &preorderParser{src: src, onlyNumber: onlyNumber}
}

func (p *preorderParser) walk(visitor Visitor) error {
	p.skipWhitespace()
	if p.pos >= len(p.src) {
		return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_EOF, Msg: nativetypes.ERR_EOF.Message()}
	}
	return p.parseValue(visitor)
}

func (p *preorderParser) parseValue(visitor Visitor) error {
	if p.pos >= len(p.src) {
		return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_EOF, Msg: nativetypes.ERR_EOF.Message()}
	}
	switch p.src[p.pos] {
	case '{':
		return p.parseObject(visitor)
	case '[':
		return p.parseArray(visitor)
	case '"':
		s, err := p.parseString()
		if err != nil {
			return err
		}
		return visitor.OnString(s)
	case 't', 'f':
		return p.parseBool(visitor)
	case 'n':
		return p.parseNull(visitor)
	default:
		return p.parseNumber(visitor)
	}
}

func (p *preorderParser) parseObject(visitor Visitor) error {
	// Consume '{'.
	p.pos++
	p.skipWhitespace()
	// Estimate capacity from the source between '{' and matching '}'.
	capacity := p.guessContainerSize()
	if err := visitor.OnObjectBegin(capacity); err != nil {
		if errors.Is(err, VisitOPSkip) {
			// Skip the object body but still emit the end event.
			if err := p.skipContainer('{'); err != nil {
				return err
			}
			return visitor.OnObjectEnd()
		}
		return err
	}
	if p.pos >= len(p.src) {
		return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_EOF, Msg: nativetypes.ERR_EOF.Message()}
	}
	if p.src[p.pos] == '}' {
		p.pos++
		return visitor.OnObjectEnd()
	}
	for {
		p.skipWhitespace()
		if p.pos >= len(p.src) || p.src[p.pos] != '"' {
			return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_INVALID_CHAR, Msg: "expected object key"}
		}
		key, err := p.parseString()
		if err != nil {
			return err
		}
		if err := visitor.OnObjectKey(key); err != nil {
			if errors.Is(err, VisitOPSkip) {
				// Skip just this value.
				p.skipWhitespace()
				if p.pos >= len(p.src) || p.src[p.pos] != ':' {
					return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_INVALID_CHAR, Msg: "expected ':' after object key"}
				}
				p.pos++
				p.skipWhitespace()
				if err := p.skipValue(); err != nil {
					return err
				}
			} else {
				return err
			}
		} else {
			p.skipWhitespace()
			if p.pos >= len(p.src) || p.src[p.pos] != ':' {
				return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_INVALID_CHAR, Msg: "expected ':' after object key"}
			}
			p.pos++
			p.skipWhitespace()
			if err := p.parseValue(visitor); err != nil {
				return err
			}
		}
		p.skipWhitespace()
		if p.pos >= len(p.src) {
			return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_EOF, Msg: nativetypes.ERR_EOF.Message()}
		}
		switch p.src[p.pos] {
		case ',':
			p.pos++
			continue
		case '}':
			p.pos++
			return visitor.OnObjectEnd()
		default:
			return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_INVALID_CHAR, Msg: "expected ',' or '}'"}
		}
	}
}

func (p *preorderParser) parseArray(visitor Visitor) error {
	// Consume '['.
	p.pos++
	p.skipWhitespace()
	capacity := p.guessContainerSize()
	if err := visitor.OnArrayBegin(capacity); err != nil {
		if errors.Is(err, VisitOPSkip) {
			if err := p.skipContainer('['); err != nil {
				return err
			}
			return visitor.OnArrayEnd()
		}
		return err
	}
	if p.pos >= len(p.src) {
		return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_EOF, Msg: nativetypes.ERR_EOF.Message()}
	}
	if p.src[p.pos] == ']' {
		p.pos++
		return visitor.OnArrayEnd()
	}
	for {
		p.skipWhitespace()
		if err := p.parseValue(visitor); err != nil {
			return err
		}
		p.skipWhitespace()
		if p.pos >= len(p.src) {
			return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_EOF, Msg: nativetypes.ERR_EOF.Message()}
		}
		switch p.src[p.pos] {
		case ',':
			p.pos++
			continue
		case ']':
			p.pos++
			return visitor.OnArrayEnd()
		default:
			return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_INVALID_CHAR, Msg: "expected ',' or ']'"}
		}
	}
}

func (p *preorderParser) parseBool(visitor Visitor) error {
	if strings.HasPrefix(p.src[p.pos:], "true") {
		p.pos += 4
		return visitor.OnBool(true)
	}
	if strings.HasPrefix(p.src[p.pos:], "false") {
		p.pos += 5
		return visitor.OnBool(false)
	}
	return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_INVALID_CHAR, Msg: "invalid boolean literal"}
}

func (p *preorderParser) parseNull(visitor Visitor) error {
	if strings.HasPrefix(p.src[p.pos:], "null") {
		p.pos += 4
		return visitor.OnNull()
	}
	return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_INVALID_CHAR, Msg: "invalid null literal"}
}

func (p *preorderParser) parseNumber(visitor Visitor) error {
	start := p.pos
	// Scan a Sonic-compatible JSON number. Sonic preserves leading-zero
	// digit runs verbatim instead of truncating after the first zero.
	s := p.src
	if p.pos < len(s) && s[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(s) || s[p.pos] < '0' || s[p.pos] > '9' {
		return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_INVALID_NUMBER_FMT, Msg: "invalid number"}
	}
	if s[p.pos] == '0' {
		p.pos++
		for p.pos < len(s) && s[p.pos] >= '0' && s[p.pos] <= '9' {
			p.pos++
		}
	} else {
		for p.pos < len(s) && s[p.pos] >= '0' && s[p.pos] <= '9' {
			p.pos++
		}
	}
	if p.pos < len(s) && s[p.pos] == '.' {
		p.pos++
		if p.pos >= len(s) || s[p.pos] < '0' || s[p.pos] > '9' {
			return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_INVALID_NUMBER_FMT, Msg: "invalid fraction"}
		}
		for p.pos < len(s) && s[p.pos] >= '0' && s[p.pos] <= '9' {
			p.pos++
		}
	}
	if p.pos < len(s) && (s[p.pos] == 'e' || s[p.pos] == 'E') {
		p.pos++
		if p.pos < len(s) && isJSONNumberTerminator(s[p.pos]) {
			if p.onlyNumber {
				return visitor.OnFloat64(0, json.Number(s[start:p.pos]))
			}
			return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_INVALID_NUMBER_FMT, Msg: "invalid exponent"}
		}
		if p.pos < len(s) && (s[p.pos] == '+' || s[p.pos] == '-') {
			p.pos++
		}
		if p.pos >= len(s) || s[p.pos] < '0' || s[p.pos] > '9' {
			return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_INVALID_NUMBER_FMT, Msg: "invalid exponent"}
		}
		for p.pos < len(s) && s[p.pos] >= '0' && s[p.pos] <= '9' {
			p.pos++
		}
	}
	numStr := s[start:p.pos]
	num := json.Number(numStr)
	if p.onlyNumber {
		return visitor.OnFloat64(0, num)
	}
	// Prefer int64 when the number has no '.' or 'e' and fits.
	if !strings.ContainsAny(numStr, ".eE") {
		if i, err := strconv.ParseInt(numStr, 10, 64); err == nil {
			return visitor.OnInt64(i, num)
		}
	}
	f, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		// Sonic tolerates out-of-range floats (e.g. 1e999) by emitting
		// ±Inf rather than a syntax error.
		if numErr, ok := err.(*strconv.NumError); ok && numErr.Err == strconv.ErrRange {
			return visitor.OnFloat64(f, num)
		}
		return &SyntaxError{Pos: start, Src: p.src, Code: nativetypes.ERR_INVALID_NUMBER_FMT, Msg: err.Error()}
	}
	return visitor.OnFloat64(f, num)
}

func (p *preorderParser) parseString() (string, error) {
	// Assumes p.src[p.pos] == '"'.
	p.pos++
	var b strings.Builder
	s := p.src
	for p.pos < len(s) {
		c := s[p.pos]
		if c == '"' {
			p.pos++
			return b.String(), nil
		}
		if c == '\\' {
			p.pos++
			if p.pos >= len(s) {
				return "", &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_INVALID_ESCAPE, Msg: "truncated escape"}
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
				r, err := p.parseUnicodeEscape()
				if err != nil {
					return "", err
				}
				// Handle surrogate pair.
				if r >= 0xD800 && r <= 0xDBFF && p.pos+1 < len(s) && s[p.pos] == '\\' && s[p.pos+1] == 'u' {
					save := p.pos
					p.pos += 2
					r2, err := p.parseUnicodeEscape()
					if err != nil {
						return "", err
					}
					if r2 >= 0xDC00 && r2 <= 0xDFFF {
						r = 0x10000 + (r-0xD800)<<10 + (r2 - 0xDC00)
					} else {
						// Not a low surrogate: emit U+FFFD for the high
						// surrogate and rewind so the second escape is
						// processed on its own iteration (Sonic emits a
						// replacement per unpaired surrogate instead of
						// swallowing the second escape).
						p.pos = save
						r = 0xFFFD
					}
				} else if r >= 0xDC00 && r <= 0xDFFF {
					// Lone low surrogate.
					r = 0xFFFD
				}
				b.WriteRune(r)
			default:
				return "", &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_INVALID_ESCAPE, Msg: "unknown escape"}
			}
			continue
		}
		if c < 0x80 {
			b.WriteByte(c)
			p.pos++
			continue
		}
		_, size := utf8.DecodeRuneInString(s[p.pos:])
		if size == 1 {
			// Sonic preserves malformed bytes, but they must not consume a
			// following JSON delimiter such as the closing quote.
			b.WriteByte(c)
			p.pos++
			continue
		}
		b.WriteString(s[p.pos : p.pos+size])
		p.pos += size
	}
	return "", &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_EOF, Msg: "unterminated string"}
}

func (p *preorderParser) parseUnicodeEscape() (rune, error) {
	s := p.src
	if p.pos+4 > len(s) {
		return 0, &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_INVALID_UNICODE, Msg: "truncated \\u escape"}
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
			return 0, &SyntaxError{Pos: p.pos - 4 + i, Src: p.src, Code: nativetypes.ERR_INVALID_UNICODE, Msg: "invalid hex digit"}
		}
		r = r<<4 | d
	}
	return r, nil
}

// skipContainer skips the body of a container. p.pos must point at the first
// byte after open.
func (p *preorderParser) skipContainer(open byte) error {
	expectedClose := byte('}')
	if open == '[' {
		expectedClose = ']'
	}
	depth := 1
	inString := false
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if inString {
			if c == '\\' {
				p.pos += 2
				continue
			}
			if c == '"' {
				inString = false
			}
			p.pos++
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				if c != expectedClose {
					return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_INVALID_CHAR, Msg: "mismatched closing delimiter"}
				}
				p.pos++
				return nil
			}
		}
		p.pos++
	}
	return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_EOF, Msg: "unterminated container"}
}

// skipValue skips a single JSON value starting at p.pos.
func (p *preorderParser) skipValue() error {
	p.skipWhitespace()
	if p.pos >= len(p.src) {
		return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_EOF, Msg: nativetypes.ERR_EOF.Message()}
	}
	switch p.src[p.pos] {
	case '{', '[':
		open := p.src[p.pos]
		p.pos++
		return p.skipContainer(open)
	case '"':
		// Skip a string literal.
		p.pos++
		s := p.src
		for p.pos < len(s) {
			c := s[p.pos]
			if c == '\\' {
				p.pos += 2
				continue
			}
			if c == '"' {
				p.pos++
				return nil
			}
			p.pos++
		}
		return &SyntaxError{Pos: p.pos, Src: p.src, Code: nativetypes.ERR_EOF, Msg: "unterminated string"}
	default:
		// Scalar: consume until whitespace or delimiter.
		s := p.src
		for p.pos < len(s) {
			c := s[p.pos]
			if c == ',' || c == '}' || c == ']' || c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				return nil
			}
			p.pos++
		}
		return nil
	}
}

func (p *preorderParser) skipWhitespace() {
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

// guessContainerSize returns a rough estimate of the number of children
// of the container starting at p.pos-1's body. It scans the top level of
// the container counting top-level commas. The estimate is used only as
// a hint for visitors that pre-allocate capacity.
func (p *preorderParser) guessContainerSize() int {
	saved := p.pos
	depth := 1
	inString := false
	count := 0
	body := p.src[p.pos:]
	i := 0
	for i < len(body) && depth > 0 {
		c := body[i]
		if inString {
			if c == '\\' {
				i += 2
				continue
			}
			if c == '"' {
				inString = false
			}
			i++
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				break
			}
		case ',':
			if depth == 1 {
				count++
			}
		}
		i++
	}
	if i == 0 && depth > 0 {
		// Empty container.
		p.pos = saved
		return 0
	}
	// Non-empty containers have count+1 top-level elements.
	if i > 0 && depth == 0 && (count > 0 || i > 0) {
		// Detect empty container.
		// Find first non-whitespace byte in body.
		j := 0
		for j < len(body) && (body[j] == ' ' || body[j] == '\t' || body[j] == '\n' || body[j] == '\r') {
			j++
		}
		if j < len(body) && (body[j] == '}' || body[j] == ']') {
			return 0
		}
		p.pos = saved
		return count + 1
	}
	p.pos = saved
	return 0
}
