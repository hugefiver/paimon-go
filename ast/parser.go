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

// parseRawToNodeEx is the worker for parseRawToNode. It uses the local
// recursive-descent parser (not fastjson) so string unescaping follows
// Sonic semantics (unpaired surrogates -> U+FFFD, unknown escapes are
// errors) and the nesting limit matches Sonic's MAX_RECURSE (4096).
func parseRawToNodeEx(src string) (Node, nativetypes.ParsingError) {
	return parseRawToNodeLocal(src)
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
		// fastjson's unescape is "best-effort": an unpaired \uD800
		// surrogate stays as literal backslash text instead of U+FFFD,
		// which breaks string equality and object-key lookup. Re-quote
		// the (already unescaped) content and decode through the local
		// parser's unescape rules is not possible (the escape structure
		// is lost), so accept fastjson's decoding for the searcher
		// fallback path; the primary Parser path no longer routes
		// through fastjson.
		s, err := v.StringBytes()
		if err != nil {
			return Node{}, nativetypes.ERR_INVALID_CHAR
		}
		return NewString(string(s)), 0
	case vfastjson.TypeNumber:
		// fastjson's Value.String() for a TypeNumber value returns the
		// raw number literal (MarshalTo appends v.s verbatim for numbers).
		num, ok := sonicNumberLiteral(v.String())
		if !ok {
			return Node{}, nativetypes.ERR_INVALID_NUMBER_FMT
		}
		return NewNumber(num), 0
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
		var firstErr nativetypes.ParsingError
		obj.Visit(func(k []byte, ev *vfastjson.Value) {
			if firstErr != 0 {
				return
			}
			if code := func() nativetypes.ParsingError {
				n, code := fastjsonValueToNode(ev)
				if code != 0 {
					return code
				}
				pairs = append(pairs, NewPair(string(k), n))
				return 0
			}(); code != 0 {
				firstErr = code
			}
		})
		if firstErr != 0 {
			return Node{}, firstErr
		}
		return NewObject(pairs), 0
	}
	return Node{typ: V_ERROR, exists: true, loaded: true, err: fmt.Errorf("unknown fastjson type %d", v.Type())}, nativetypes.ERR_INVALID_CHAR
}

// sonicNumberLiteral validates a JSON number literal. Leading zeros
// followed by more digits (e.g. "0123") consume the full digit run so
// the raw literal is preserved verbatim, matching Sonic's lenient
// number handling instead of silently truncating the value.
func sonicNumberLiteral(lit string) (string, bool) {
	if lit == "" {
		return "", false
	}
	i := 0
	if lit[i] == '-' {
		i++
		if i == len(lit) {
			return "", false
		}
	}
	intStart := i
	if lit[i] == '0' {
		i++
		// Consume any additional digits so leading-zero literals such as
		// "0123" round-trip as-is rather than being cut to "0".
		for i < len(lit) && lit[i] >= '0' && lit[i] <= '9' {
			i++
		}
	} else if lit[i] >= '1' && lit[i] <= '9' {
		for i < len(lit) && lit[i] >= '0' && lit[i] <= '9' {
			i++
		}
	} else {
		return "", false
	}
	if i < len(lit) && lit[i] == '.' {
		i++
		if i == len(lit) || lit[i] < '0' || lit[i] > '9' {
			return "", false
		}
		for i < len(lit) && lit[i] >= '0' && lit[i] <= '9' {
			i++
		}
	}
	if i < len(lit) && (lit[i] == 'e' || lit[i] == 'E') {
		i++
		if i < len(lit) && (lit[i] == '+' || lit[i] == '-') {
			i++
		}
		if i == len(lit) || lit[i] < '0' || lit[i] > '9' {
			return "", false
		}
		for i < len(lit) && lit[i] >= '0' && lit[i] <= '9' {
			i++
		}
	}
	if i != len(lit) || i == intStart {
		return "", false
	}
	return lit, true
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
