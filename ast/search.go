package ast

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	nativetypes "github.com/bytedance/sonic/internal/native/types"
)

// NewSearcher builds a Searcher for the given JSON source. ValidateJSON
// defaults to true, matching Sonic v1.15.2.
func NewSearcher(str string) *Searcher {
	return &Searcher{SearchOptions: SearchOptions{ValidateJSON: true}, src: str}
}

// GetByPath parses the JSON source enough to resolve path and returns
// the matched node. When CopyReturn is false the returned Node may reference
// the selected substring in the searcher's source; when true it owns an exact
// copy of that substring.
func (s *Searcher) GetByPath(path ...interface{}) (Node, error) {
	return s.getByPath(path, s.SearchOptions)
}

// GetByPathCopy is GetByPath with CopyReturn semantics. The returned
// node is always safe to retain beyond the next call to the searcher.
// Like Sonic, this persists CopyReturn=true on the searcher.
func (s *Searcher) GetByPathCopy(path ...interface{}) (Node, error) {
	s.CopyReturn = true
	return s.getByPath(path, s.SearchOptions)
}

func (s *Searcher) getByPath(path []interface{}, opts SearchOptions) (Node, error) {
	if s == nil {
		return Node{}, ErrNotExist
	}
	if raw, status := getPathRaw(s.src, path, opts.ValidateJSON); status != rawPathNoFast {
		switch status {
		case rawPathFound:
			if opts.CopyReturn {
				raw = strings.Clone(raw)
			}
			return newUncheckedRaw(raw, opts), nil
		case rawPathMissing:
			return Node{}, ErrNotExist
		case rawPathInvalid:
			return Node{}, SyntaxError{Src: s.src, Msg: "invalid JSON value", Code: nativetypes.ERR_INVALID_CHAR}
		}
	}
	// Local-parser fallback: parse the whole document into an owning
	// Node tree, then walk the path. This avoids fastjson's
	// best-effort string unescaping (unpaired surrogates stay literal)
	// and its MaxDepth=300 nesting limit.
	root, code := parseRawToNodeLocal(s.src)
	if code != 0 {
		return Node{}, code
	}
	cur := &root
	for _, step := range path {
		if cur == nil || !cur.exists {
			return Node{}, ErrNotExist
		}
		switch x := step.(type) {
		case string:
			next := cur.Get(x)
			if !next.Exists() {
				return Node{}, ErrNotExist
			}
			cur = next
		case int:
			next := cur.Index(x)
			if !next.Exists() {
				return Node{}, ErrNotExist
			}
			cur = next
		case int64:
			idx, ok := intFromInt64(x)
			if !ok {
				return Node{}, ErrNotExist
			}
			next := cur.Index(idx)
			if !next.Exists() {
				return Node{}, ErrNotExist
			}
			cur = next
		case json.Number:
			if idx, perr := strconv.Atoi(string(x)); perr == nil {
				next := cur.Index(idx)
				if !next.Exists() {
					return Node{}, ErrNotExist
				}
				cur = next
			} else {
				next := cur.Get(string(x))
				if !next.Exists() {
					return Node{}, ErrNotExist
				}
				cur = next
			}
		default:
			return Node{}, fmt.Errorf("%w: unsupported path element type %T", ErrUnsupportType, step)
		}
	}
	return *cur, nil
}

type rawPathStatus int

const (
	rawPathNoFast rawPathStatus = iota
	rawPathFound
	rawPathMissing
	rawPathInvalid
)

func getPathRaw(src string, path []interface{}, validate bool) (string, rawPathStatus) {
	if !canScanASCII(src) {
		return "", rawPathNoFast
	}
	start := skipJSONSpaceString(src, 0)
	if start == len(src) {
		return "", rawPathInvalid
	}
	for _, step := range path {
		switch x := step.(type) {
		case string:
			var status rawPathStatus
			start, status = findObjectValueStartString(src, start, x)
			if status != rawPathFound {
				return "", status
			}
		case int:
			var status rawPathStatus
			start, status = findArrayValueStartString(src, start, x)
			if status != rawPathFound {
				return "", status
			}
		case int64:
			idx, ok := intFromInt64(x)
			if !ok {
				return "", rawPathMissing
			}
			var status rawPathStatus
			start, status = findArrayValueStartString(src, start, idx)
			if status != rawPathFound {
				return "", status
			}
		case json.Number:
			if idx, err := strconv.Atoi(string(x)); err == nil {
				var status rawPathStatus
				start, status = findArrayValueStartString(src, start, idx)
				if status != rawPathFound {
					return "", status
				}
			} else {
				var status rawPathStatus
				start, status = findObjectValueStartString(src, start, string(x))
				if status != rawPathFound {
					return "", status
				}
			}
		default:
			return "", rawPathNoFast
		}
	}
	return searchValueRaw(src, start, validate)
}

func searchValueRaw(src string, start int, validate bool) (string, rawPathStatus) {
	if start >= len(src) {
		return "", rawPathInvalid
	}
	var (
		end int
		ok  bool
	)
	if src[start] == '{' || src[start] == '[' {
		end, ok = scanContainerEnd(src, start)
	} else {
		end, ok = scanValueEndString(src, start, 0)
	}
	if !ok {
		return "", rawPathInvalid
	}
	raw := src[start:end]
	// Structurally closed string tokens preserve Sonic's raw compatibility,
	// including unknown escapes. Validation still checks other non-string syntax.
	if validate && raw[0] != '"' {
		bareExponent := isBareExponent(raw) && validScannedRootRaw(src, start, end)
		if !validRootRaw(raw) && !bareExponent {
			return "", rawPathInvalid
		}
		if _, code := parseRawToNodeLocal(raw); code != 0 && code != nativetypes.ERR_INVALID_ESCAPE && !bareExponent {
			return "", rawPathInvalid
		}
	}
	if !validate && (raw[0] == '{' || raw[0] == '[') {
		// Preserve loose containers without accepting malformed number tokens.
		if _, code := parseRawToNodeLocal(raw); code == nativetypes.ERR_INVALID_NUMBER_FMT {
			return "", rawPathInvalid
		}
	}
	return raw, rawPathFound
}

func newUncheckedRaw(raw string, opts SearchOptions) Node {
	typ := V_NUMBER
	switch raw[0] {
	case 'n':
		typ = V_NULL
	case 't':
		typ = V_TRUE
	case 'f':
		typ = V_FALSE
	case '{':
		typ = V_OBJECT
	case '[':
		typ = V_ARRAY
	case '"':
		typ = V_STRING
	}
	node := Node{typ: typ, exists: true, raw: raw}
	if opts.ConcurrentRead {
		node.mu = &sync.RWMutex{}
	}
	return node
}

func canScanASCII(src string) bool {
	return true
}

func findObjectValueStartString(src string, start int, key string) (int, rawPathStatus) {
	if start >= len(src) || src[start] != '{' {
		return 0, rawPathMissing
	}
	i := skipJSONSpaceString(src, start+1)
	if i < len(src) && src[i] == '}' {
		return 0, rawPathMissing
	}
	for i < len(src) {
		if src[i] != '"' {
			return 0, rawPathInvalid
		}
		keyStart := i + 1
		keyEnd, ok := scanStringEnd(src, i)
		if !ok {
			return 0, rawPathInvalid
		}
		matches, ok := jsonKeyMatchesString(src[keyStart:keyEnd-1], key)
		if !ok {
			return 0, rawPathNoFast
		}
		i = skipJSONSpaceString(src, keyEnd)
		if i >= len(src) || src[i] != ':' {
			return 0, rawPathInvalid
		}
		valueStart := skipJSONSpaceString(src, i+1)
		if matches {
			if valueStart >= len(src) {
				return 0, rawPathInvalid
			}
			return valueStart, rawPathFound
		}
		valueEnd, ok := scanPreTargetValueEndString(src, valueStart)
		if !ok {
			return 0, rawPathInvalid
		}
		i = skipJSONSpaceString(src, valueEnd)
		if i >= len(src) {
			return 0, rawPathInvalid
		}
		switch src[i] {
		case ',':
			i = skipJSONSpaceString(src, i+1)
		case '}':
			return 0, rawPathMissing
		default:
			return 0, rawPathInvalid
		}
	}
	return 0, rawPathInvalid
}

func findArrayValueStartString(src string, start int, idx int) (int, rawPathStatus) {
	if idx < 0 {
		return 0, rawPathMissing
	}
	if start >= len(src) || src[start] != '[' {
		return 0, rawPathMissing
	}
	i := skipJSONSpaceString(src, start+1)
	if i < len(src) && src[i] == ']' {
		return 0, rawPathMissing
	}
	cur := 0
	for i < len(src) {
		if cur == idx {
			return i, rawPathFound
		}
		valueEnd, ok := scanPreTargetValueEndString(src, i)
		if !ok {
			return 0, rawPathInvalid
		}
		cur++
		i = skipJSONSpaceString(src, valueEnd)
		if i >= len(src) {
			return 0, rawPathInvalid
		}
		switch src[i] {
		case ',':
			i = skipJSONSpaceString(src, i+1)
		case ']':
			return 0, rawPathMissing
		default:
			return 0, rawPathInvalid
		}
	}
	return 0, rawPathInvalid
}

// maxScanDepth mirrors Sonic's MAX_RECURSE limit (4096); fastjson's
// MaxDepth of 300 would reject valid deep documents Sonic accepts.
const maxScanDepth = 4096

func scanPreTargetValueEndString(src string, start int) (int, bool) {
	if start < len(src) && (src[start] == '{' || src[start] == '[') {
		return scanContainerEnd(src, start)
	}
	return scanValueEndString(src, start, 0)
}

func scanValueEndString(src string, start int, depth int) (int, bool) {
	if depth > maxScanDepth || start >= len(src) {
		return 0, false
	}
	switch src[start] {
	case '{':
		return scanObjectEndString(src, start, depth+1)
	case '[':
		return scanArrayEndString(src, start, depth+1)
	case '"':
		return scanStringEnd(src, start)
	case 't':
		return scanLiteralString(src, start, "true")
	case 'f':
		return scanLiteralString(src, start, "false")
	case 'n':
		return scanLiteralString(src, start, "null")
	default:
		if src[start] == '-' || (src[start] >= '0' && src[start] <= '9') {
			return scanNumberEndString(src, start)
		}
	}
	return 0, false
}

func scanObjectEndString(src string, start int, depth int) (int, bool) {
	i := skipJSONSpaceString(src, start+1)
	if i < len(src) && src[i] == '}' {
		return i + 1, true
	}
	for i < len(src) {
		if src[i] != '"' {
			return 0, false
		}
		keyEnd, ok := scanStringEnd(src, i)
		if !ok {
			return 0, false
		}
		i = skipJSONSpaceString(src, keyEnd)
		if i >= len(src) || src[i] != ':' {
			return 0, false
		}
		i = skipJSONSpaceString(src, i+1)
		valueEnd, ok := scanValueEndString(src, i, depth)
		if !ok {
			return 0, false
		}
		i = skipJSONSpaceString(src, valueEnd)
		if i >= len(src) {
			return 0, false
		}
		switch src[i] {
		case ',':
			i = skipJSONSpaceString(src, i+1)
		case '}':
			return i + 1, true
		default:
			return 0, false
		}
	}
	return 0, false
}

func scanArrayEndString(src string, start int, depth int) (int, bool) {
	i := skipJSONSpaceString(src, start+1)
	if i < len(src) && src[i] == ']' {
		return i + 1, true
	}
	for i < len(src) {
		valueEnd, ok := scanValueEndString(src, i, depth)
		if !ok {
			return 0, false
		}
		i = skipJSONSpaceString(src, valueEnd)
		if i >= len(src) {
			return 0, false
		}
		switch src[i] {
		case ',':
			i = skipJSONSpaceString(src, i+1)
		case ']':
			return i + 1, true
		default:
			return 0, false
		}
	}
	return 0, false
}

func scanLiteralString(src string, start int, lit string) (int, bool) {
	if len(src)-start < len(lit) || src[start:start+len(lit)] != lit {
		return 0, false
	}
	return start + len(lit), true
}

func scanNumberEndString(src string, start int) (int, bool) {
	i := start
	if src[i] == '-' {
		i++
		if i == len(src) {
			return 0, false
		}
	}
	if src[i] == '0' {
		i++
		for i < len(src) && src[i] >= '0' && src[i] <= '9' {
			i++
		}
	} else if src[i] >= '1' && src[i] <= '9' {
		for i < len(src) && src[i] >= '0' && src[i] <= '9' {
			i++
		}
	} else {
		return 0, false
	}
	if i < len(src) && src[i] == '.' {
		i++
		if i == len(src) || src[i] < '0' || src[i] > '9' {
			return 0, false
		}
		for i < len(src) && src[i] >= '0' && src[i] <= '9' {
			i++
		}
	}
	if i < len(src) && (src[i] == 'e' || src[i] == 'E') {
		i++
		if i < len(src) && isJSONNumberTerminator(src[i]) {
			return i, true
		}
		if i < len(src) && (src[i] == '+' || src[i] == '-') {
			i++
		}
		if i == len(src) || src[i] < '0' || src[i] > '9' {
			return 0, false
		}
		for i < len(src) && src[i] >= '0' && src[i] <= '9' {
			i++
		}
	}
	return i, true
}

func skipJSONSpaceString(src string, i int) int {
	for i < len(src) && isJSONSpace(src[i]) {
		i++
	}
	return i
}

func asciiKeyEqualString(raw string, key string) bool {
	return raw == key
}

func jsonKeyMatchesString(raw string, key string) (bool, bool) {
	if strings.IndexByte(raw, '\\') < 0 {
		return asciiKeyEqualString(raw, key), true
	}
	var decoded string
	if err := json.Unmarshal([]byte("\""+raw+"\""), &decoded); err != nil {
		return false, false
	}
	return decoded == key, true
}

func validRootRaw(raw string) bool {
	start := skipJSONSpaceString(raw, 0)
	if start == len(raw) {
		return false
	}
	end, ok := scanValueEndString(raw, start, 0)
	if !ok {
		return false
	}
	return skipJSONSpaceString(raw, end) == len(raw)
}

// validScannedRootRaw preserves the source delimiter that made a bare
// exponent scannable. Once sliced, that context is unavailable to
// validRootRaw, so accept only that exact upstream-compatible exception.
func validScannedRootRaw(src string, start int, end int) bool {
	raw := src[start:end]
	if validRootRaw(raw) {
		return true
	}
	if len(raw) == 0 || end == len(src) || !isJSONNumberTerminator(src[end]) {
		return false
	}
	last := raw[len(raw)-1]
	if last != 'e' && last != 'E' {
		return false
	}
	return validRootRaw(raw[:len(raw)-1])
}

func scanFirstValueEnd(src string, start int) (int, bool) {
	switch src[start] {
	case '{', '[':
		return scanContainerEnd(src, start)
	case '"':
		return scanStringEnd(src, start)
	case 't':
		return scanLiteralString(src, start, "true")
	case 'f':
		return scanLiteralString(src, start, "false")
	case 'n':
		return scanLiteralString(src, start, "null")
	default:
		if src[start] == '-' || (src[start] >= '0' && src[start] <= '9') {
			return scanNumberEndString(src, start)
		}
		return 0, false
	}
}

func scanContainerEnd(src string, start int) (int, bool) {
	expects := make([]byte, 0, 8)
	push := func(c byte) {
		if c == '{' {
			expects = append(expects, '}')
		} else {
			expects = append(expects, ']')
		}
	}
	push(src[start])
	inString := false
	escaped := false
	for i := start + 1; i < len(src); i++ {
		c := src[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			push(c)
		case '}', ']':
			if len(expects) == 0 || c != expects[len(expects)-1] {
				return 0, false
			}
			expects = expects[:len(expects)-1]
			if len(expects) == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}

func scanStringEnd(src string, start int) (int, bool) {
	escaped := false
	for i := start + 1; i < len(src); i++ {
		c := src[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			return i + 1, true
		}
	}
	return 0, false
}

func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t'
}

func isJSONNumberTerminator(c byte) bool {
	return isJSONSpace(c) || c == ',' || c == ']' || c == '}'
}

// Loads parses src as a single JSON value and returns it as an
// interface{}. Numbers are returned as float64. The first return value
// is the byte offset where the parsed value ended.
func Loads(src string) (int, interface{}, error) {
	return loadsImpl(src, false)
}

// LoadsUseNumber is like Loads but numbers are returned as json.Number.
func LoadsUseNumber(src string) (int, interface{}, error) {
	return loadsImpl(src, true)
}

func loadsImpl(src string, useNumber bool) (int, interface{}, error) {
	// Parse with the local parser and report the value-end position
	// like Sonic's Parser.Pos() (the cursor after the parsed value), not
	// len(src).
	p := &localParser{src: src}
	p.skipSpace()
	if p.atEnd() {
		return 0, nil, nativetypes.ERR_EOF
	}
	n, code := p.parseValue(0)
	if code != 0 {
		return 0, nil, code
	}
	pos := p.pos
	v, err := n.interfaceWith(useNumber, false)
	if err != nil {
		return 0, nil, err
	}
	return pos, v, nil
}

// Compile-time checks that the error types satisfy the error interface.
var (
	_ error = (*SyntaxError)(nil)
	_ error = nativetypes.ParsingError(0)
)
