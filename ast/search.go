package ast

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	nativetypes "github.com/bytedance/sonic/internal/native/types"
	vfastjson "github.com/valyala/fastjson"
)

// NewSearcher builds a Searcher for the given JSON source.
func NewSearcher(str string) *Searcher {
	return &Searcher{SearchOptions: SearchOptions{}, src: str}
}

// GetByPath parses the JSON source enough to resolve path and returns
// the matched node. When CopyReturn is false the returned Node may
// reference the searcher's internal buffers; when true a deep copy is
// returned. For this implementation a deep copy is always returned
// because parseRawToNode fully copies the fastjson value into Node
// storage that owns its own memory.
func (s *Searcher) GetByPath(path ...interface{}) (Node, error) {
	return s.getByPath(path, s.CopyReturn)
}

// GetByPathCopy is GetByPath with CopyReturn semantics. The returned
// node is always safe to retain beyond the next call to the searcher.
func (s *Searcher) GetByPathCopy(path ...interface{}) (Node, error) {
	return s.getByPath(path, true)
}

func (s *Searcher) getByPath(path []interface{}, copyReturn bool) (Node, error) {
	if s == nil {
		return Node{}, ErrNotExist
	}
	if s.ValidateJSON {
		if err := vfastjson.Validate(s.src); err != nil {
			return Node{}, mapFastjsonError(s.src, err)
		}
	}
	if len(path) == 0 {
		raw, code := firstValueRaw(s.src)
		if code != 0 {
			return Node{}, code
		}
		return NewRaw(raw), nil
	}
	if node, status := getPathRaw(s.src, path); status != rawPathNoFast {
		switch status {
		case rawPathFound:
			return node, nil
		case rawPathMissing:
			return Node{}, ErrNotExist
		case rawPathInvalid:
			return Node{}, SyntaxError{Src: s.src, Msg: "invalid JSON value", Code: nativetypes.ERR_INVALID_CHAR}
		}
	}
	var fp vfastjson.Parser
	v, err := fp.Parse(s.src)
	if err != nil {
		return Node{}, mapFastjsonError(s.src, err)
	}
	cur := v
	for _, step := range path {
		switch x := step.(type) {
		case string:
			o, err := cur.Object()
			if err != nil {
				return Node{}, ErrNotExist
			}
			next := o.Get(x)
			if next == nil {
				return Node{}, ErrNotExist
			}
			cur = next
		case int:
			arr, err := cur.Array()
			if err != nil {
				return Node{}, ErrNotExist
			}
			if x < 0 || x >= len(arr) {
				return Node{}, ErrNotExist
			}
			cur = arr[x]
		case int64:
			idx, ok := intFromInt64(x)
			if !ok {
				return Node{}, ErrNotExist
			}
			arr, err := cur.Array()
			if err != nil {
				return Node{}, ErrNotExist
			}
			if idx < 0 || idx >= len(arr) {
				return Node{}, ErrNotExist
			}
			cur = arr[idx]
		case json.Number:
			if idx, perr := strconv.Atoi(string(x)); perr == nil {
				arr, err := cur.Array()
				if err != nil {
					return Node{}, ErrNotExist
				}
				if idx < 0 || idx >= len(arr) {
					return Node{}, ErrNotExist
				}
				cur = arr[idx]
			} else {
				o, err := cur.Object()
				if err != nil {
					return Node{}, ErrNotExist
				}
				next := o.Get(string(x))
				if next == nil {
					return Node{}, ErrNotExist
				}
				cur = next
			}
		default:
			return Node{}, fmt.Errorf("%w: unsupported path element type %T", ErrUnsupportType, step)
		}
	}
	n, code := fastjsonValueToNode(cur)
	if code != 0 {
		return Node{}, code
	}
	_ = copyReturn // fastjsonValueToNode always produces an owning Node.
	return n, nil
}

func firstValueRaw(src string) (string, nativetypes.ParsingError) {
	start := 0
	for start < len(src) && isJSONSpace(src[start]) {
		start++
	}
	if start == len(src) {
		return "", nativetypes.ERR_EOF
	}
	end, ok := scanFirstValueEnd(src, start)
	if !ok {
		return "", nativetypes.ERR_MISMATCH
	}
	raw := src[start:end]
	if !validRootRaw(raw) {
		var fp vfastjson.Parser
		_, err := fp.Parse(raw)
		if err == nil {
			return "", nativetypes.ERR_INVALID_NUMBER_FMT
		}
		return "", mapFastjsonError(raw, err)
	}
	return raw, 0
}

type rawPathStatus int

const (
	rawPathNoFast rawPathStatus = iota
	rawPathFound
	rawPathMissing
	rawPathInvalid
)

func getPathRaw(src string, path []interface{}) (Node, rawPathStatus) {
	if !canScanASCII(src) {
		return Node{}, rawPathNoFast
	}
	start := skipJSONSpaceString(src, 0)
	if start == len(src) {
		return Node{}, rawPathInvalid
	}
	for _, step := range path {
		switch x := step.(type) {
		case string:
			var status rawPathStatus
			start, status = findObjectValueStartString(src, start, x)
			if status != rawPathFound {
				return Node{}, status
			}
		case int:
			var status rawPathStatus
			start, status = findArrayValueStartString(src, start, x)
			if status != rawPathFound {
				return Node{}, status
			}
		case int64:
			idx, ok := intFromInt64(x)
			if !ok {
				return Node{}, rawPathMissing
			}
			var status rawPathStatus
			start, status = findArrayValueStartString(src, start, idx)
			if status != rawPathFound {
				return Node{}, status
			}
		case json.Number:
			if idx, err := strconv.Atoi(string(x)); err == nil {
				var status rawPathStatus
				start, status = findArrayValueStartString(src, start, idx)
				if status != rawPathFound {
					return Node{}, status
				}
			} else {
				var status rawPathStatus
				start, status = findObjectValueStartString(src, start, string(x))
				if status != rawPathFound {
					return Node{}, status
				}
			}
		default:
			return Node{}, rawPathNoFast
		}
	}
	end, ok := scanValueEndString(src, start, 0)
	if !ok {
		return Node{}, rawPathInvalid
	}
	node := nodeFromRaw(src[start:end])
	if err := node.Check(); err != nil {
		return Node{}, rawPathInvalid
	}
	return node, rawPathFound
}

func nodeFromRaw(raw string) Node {
	if raw == "" {
		return Node{}
	}
	switch raw[0] {
	case 'n':
		return NewNull()
	case 't':
		return NewBool(true)
	case 'f':
		return NewBool(false)
	case '{', '[', '"':
		return NewRaw(raw)
	default:
		return NewNumber(raw)
	}
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
		valueEnd, ok := scanValueEndString(src, valueStart, 0)
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
		valueEnd, ok := scanValueEndString(src, i, 0)
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

func scanValueEndString(src string, start int, depth int) (int, bool) {
	if depth > vfastjson.MaxDepth || start >= len(src) {
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
	n, code := parseRawToNodeEx(src)
	if code != 0 {
		return 0, nil, code
	}
	v, err := n.interfaceWith(useNumber, false)
	if err != nil {
		return 0, nil, err
	}
	// This implementation parses the whole document eagerly; the end
	// offset is the length of the source because Validate / Parse reject
	// trailing bytes.
	return len(src), v, nil
}

// Compile-time checks that the error types satisfy the error interface.
var (
	_ error = (*SyntaxError)(nil)
	_ error = nativetypes.ParsingError(0)
)
