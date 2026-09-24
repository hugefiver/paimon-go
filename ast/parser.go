package ast

import (
	"fmt"

	nativetypes "github.com/bytedance/sonic/internal/native/types"
)

// NewParser builds a Parser for the given JSON source.
func NewParser(src string) *Parser {
	return &Parser{src: src}
}

// NewParserObj builds a Parser by value for callers that prefer the
// value-returning shape; it mirrors Sonic's NewParserObj constructor.
func NewParserObj(src string) Parser {
	return Parser{src: src}
}

// Parse parses the source into a Node. It returns the zero ParsingError
// on success; on failure it returns a non-zero descriptive code.
func (p *Parser) Parse() (Node, nativetypes.ParsingError) {
	if p == nil {
		return Node{typ: V_ERROR, exists: true, loaded: true, err: fmt.Errorf("nil parser")}, nativetypes.ERR_INVALID_CHAR
	}

	start := skipJSONSpaceString(p.src, p.pos)
	if start == len(p.src) {
		p.pos = start
		return Node{}, nativetypes.ERR_EOF
	}
	switch p.src[start] {
	case '[', '{':
		return p.parseContainer(start)
	case '"':
		end, ok := scanStringEnd(p.src, start)
		if !ok {
			p.pos = len(p.src)
			return Node{}, nativetypes.ERR_EOF
		}
		p.pos = end
		lp := localParser{src: p.src[start:end]}
		v, code := lp.parseString()
		if code == nativetypes.ERR_INVALID_UNICODE {
			code = nativetypes.ERR_INVALID_CHAR
		}
		if code != 0 {
			return Node{}, code
		}
		return NewString(v), 0
	case 't', 'f', 'n':
		literal := "true"
		value := NewBool(true)
		if p.src[start] == 'f' {
			literal, value = "false", NewBool(false)
		}
		if p.src[start] == 'n' {
			literal, value = "null", NewNull()
		}
		for i := range literal {
			p.pos = start + i
			if p.pos >= len(p.src) {
				return Node{}, nativetypes.ERR_EOF
			}
			if p.src[p.pos] != literal[i] {
				return Node{}, nativetypes.ERR_INVALID_CHAR
			}
		}
		p.pos = start + len(literal)
		return value, 0
	default:
		lp := localParser{src: p.src, pos: start}
		v, code := lp.parseNumber()
		p.pos = lp.pos
		if code != 0 {
			return Node{}, code
		}
		return NewNumber(v), 0
	}
}

// parseContainer returns an independently lazy node. A non-empty container
// leaves Parser at its first interior byte, while an empty one consumes its
// closer so callers can continue with the next top-level token.
func (p *Parser) parseContainer(start int) (Node, nativetypes.ParsingError) {
	typ, closer := V_ARRAY, byte(']')
	if p.src[start] == '{' {
		typ, closer = V_OBJECT, '}'
	}
	p.pos = start + 1
	interior := skipJSONSpaceString(p.src, p.pos)
	if interior < len(p.src) && p.src[interior] == closer {
		p.pos = interior + 1
		if typ == V_ARRAY {
			return NewArray(nil), 0
		}
		return NewObject(nil), 0
	}

	return newParserContainerNode(p.src[start:], typ), 0
}

func newParserContainerNode(raw string, typ int) Node {
	return Node{typ: typ, exists: true, loaded: true, raw: raw, lazyPos: 1}
}

// ExportError converts a ParsingError code returned by Parse into an
// error value carrying source position information.
func (p *Parser) ExportError(code nativetypes.ParsingError) error {
	if code == 0 {
		return nil
	}
	pos := p.pos
	return &SyntaxError{Pos: pos, Src: p.src, Code: code, Msg: code.Message()}
}

// Pos returns the parser's current source position, useful after a
// failed Parse to point at the offending byte.
func (p *Parser) Pos() int {
	if p == nil {
		return 0
	}
	return p.pos
}
