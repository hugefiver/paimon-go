package ast

import (
	"encoding/json"
	"fmt"
	"strconv"

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
			arr, err := cur.Array()
			if err != nil {
				return Node{}, ErrNotExist
			}
			idx := int(x)
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
	var fp vfastjson.Parser
	if _, err := fp.Parse(raw); err != nil {
		return "", mapFastjsonError(raw, err)
	}
	return raw, 0
}

func scanFirstValueEnd(src string, start int) (int, bool) {
	switch src[start] {
	case '{', '[':
		return scanContainerEnd(src, start)
	case '"':
		return scanStringEnd(src, start)
	default:
		end := start
		for end < len(src) && !isJSONSpace(src[end]) && src[end] != ',' && src[end] != ']' && src[end] != '}' {
			end++
		}
		return end, end > start
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
