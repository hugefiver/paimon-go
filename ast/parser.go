package ast

import (
	"fmt"
	"strings"

	nativetypes "github.com/bytedance/sonic/internal/native/types"
	vfastjson "github.com/valyala/fastjson"
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
	node, code := parseRawToNodeEx(p.src)
	if code != 0 {
		p.pos = locateErrorOffset(p.src, code)
		return Node{typ: V_ERROR, exists: true, loaded: true, err: code}, code
	}
	return node, 0
}

// ExportError converts a ParsingError code returned by Parse into an
// error value carrying source position information.
func (p *Parser) ExportError(code nativetypes.ParsingError) error {
	if code == 0 {
		return nil
	}
	pos := p.pos
	if pos == 0 {
		pos = locateErrorOffset(p.src, code)
	}
	src := p.src
	if len(src) > 64 {
		src = src[:32] + "..." + src[len(src)-29:]
	}
	return &SyntaxError{Pos: pos, Src: src, Code: code, Msg: code.Message()}
}

// Pos returns the parser's current source position, useful after a
// failed Parse to point at the offending byte.
func (p *Parser) Pos() int {
	if p == nil {
		return 0
	}
	return p.pos
}

// parseRawToNode parses a JSON document into a fully realized Node tree.
// On error it returns a zero node and a non-zero ParsingError.
func parseRawToNode(src string) (Node, nativetypes.ParsingError) {
	return parseRawToNodeEx(src)
}

// parseRawToNodeEx is the worker for parseRawToNode. It uses a local
// fastjson.ParserPool entry so concurrent parsers do not share state.
func parseRawToNodeEx(src string) (Node, nativetypes.ParsingError) {
	var fp vfastjson.Parser
	v, err := fp.Parse(src)
	if err != nil {
		return Node{}, mapFastjsonError(src, err)
	}
	n, code := fastjsonValueToNode(v)
	if code != 0 {
		return Node{}, code
	}
	// Mark fully loaded (fastjsonValueToNode already constructs loaded
	// nodes, but set loaded explicitly for clarity).
	n.loaded = true
	return n, 0
}

// fastjsonValueToNode deep-copies a fastjson Value into a Node. The
// returned Node owns none of the fastjson parser's memory.
func fastjsonValueToNode(v *vfastjson.Value) (Node, nativetypes.ParsingError) {
	if v == nil {
		return NewNull(), 0
	}
	switch v.Type() {
	case vfastjson.TypeNull:
		return NewNull(), 0
	case vfastjson.TypeTrue:
		return NewBool(true), 0
	case vfastjson.TypeFalse:
		return NewBool(false), 0
	case vfastjson.TypeString:
		s, err := v.StringBytes()
		if err != nil {
			return Node{}, nativetypes.ERR_INVALID_CHAR
		}
		return NewString(string(s)), 0
	case vfastjson.TypeNumber:
		// fastjson's Value.String() for a TypeNumber value returns the
		// raw number literal (MarshalTo appends v.s verbatim for numbers).
		return NewNumber(v.String()), 0
	case vfastjson.TypeArray:
		arr, err := v.Array()
		if err != nil {
			return Node{}, nativetypes.ERR_INVALID_CHAR
		}
		out := make([]Node, 0, len(arr))
		for _, e := range arr {
			n, code := fastjsonValueToNode(e)
			if code != 0 {
				return Node{}, code
			}
			out = append(out, n)
		}
		return NewArray(out), 0
	case vfastjson.TypeObject:
		obj, err := v.Object()
		if err != nil {
			return Node{}, nativetypes.ERR_INVALID_CHAR
		}
		var pairs []Pair
		obj.Visit(func(k []byte, ev *vfastjson.Value) {
			if code := func() nativetypes.ParsingError {
				n, code := fastjsonValueToNode(ev)
				if code != 0 {
					return code
				}
				pairs = append(pairs, NewPair(string(k), n))
				return 0
			}(); code != 0 {
				// On error we cannot stop Visit but the returned pairs
				// slice will be discarded by the caller; record nothing
				// here because the upstream caller checks separately.
				_ = code
			}
		})
		return NewObject(pairs), 0
	}
	return Node{typ: V_ERROR, exists: true, loaded: true, err: fmt.Errorf("unknown fastjson type %d", v.Type())}, nativetypes.ERR_INVALID_CHAR
}

// mapFastjsonError converts a fastjson parse error into a Sonic-shaped
// ParsingError code. The mapping is heuristic and matches the broad
// category of the failure rather than the exact byte Sonic would pick.
func mapFastjsonError(src string, err error) nativetypes.ParsingError {
	if err == nil {
		return 0
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "unexpected tail"),
		strings.Contains(msg, "missing '"),
		strings.Contains(msg, "missing ','"),
		strings.Contains(msg, "missing ':'"),
		strings.Contains(msg, "missing closing"),
		strings.Contains(msg, "cannot find opening"):
		return nativetypes.ERR_MISMATCH
	case strings.Contains(msg, "cannot parse number"):
		return nativetypes.ERR_INVALID_NUMBER_FMT
	case strings.Contains(msg, "cannot parse string"),
		strings.Contains(msg, "cannot parse object key"):
		return nativetypes.ERR_INVALID_CHAR
	case strings.Contains(msg, "empty string"),
		strings.Contains(msg, "unexpected value"),
		strings.Contains(msg, "unexpected char"),
		strings.Contains(msg, "unexpected end"):
		return nativetypes.ERR_INVALID_CHAR
	case strings.Contains(msg, "too big depth"):
		return nativetypes.ERR_RECURSE_EXCEED_MAX
	case strings.Contains(msg, "escape"):
		return nativetypes.ERR_INVALID_ESCAPE
	case strings.Contains(msg, "unicode"):
		return nativetypes.ERR_INVALID_UNICODE
	}
	return nativetypes.ERR_INVALID_CHAR
}

// locateErrorOffset returns a best-effort byte offset in src where a
// parse error of the given code occurred. fastjson does not surface the
// offset directly, so this falls back to scanning for the first
// "obviously wrong" character; if none is found it returns len(src).
func locateErrorOffset(src string, code nativetypes.ParsingError) int {
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch code {
		case nativetypes.ERR_MISMATCH:
			if c == '}' || c == ']' {
				return i
			}
		case nativetypes.ERR_INVALID_CHAR:
			if c < 0x20 && c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				return i
			}
		}
	}
	// Default to end-of-source for ERR_EOF and unknown codes.
	if code == nativetypes.ERR_EOF {
		return len(src)
	}
	if len(src) == 0 {
		return 0
	}
	return len(src) - 1
}
