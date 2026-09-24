// Package fastjsoncompat implements Sonic-compatible raw validation and path
// lookup without building an intermediate tree. The byte and string validators
// share a scanner; long string tokens use the standard library's byte search.
package fastjsoncompat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/internal/compatmode"
	nativetypes "github.com/bytedance/sonic/internal/native/types"
)

// Valid checks one complete value using native Sonic's structural string
// rules. String contents may contain raw controls and unknown escapes; decoding
// those strings is a separate operation. Strict mode retains standard JSON rules.
func Valid(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if compatmode.StdJSON {
		return json.Valid(data)
	}
	return validSonicValue(data)
}

// ValidString is the string-input form of Valid.
func ValidString(data string) bool {
	if len(data) == 0 {
		return false
	}
	if compatmode.StdJSON {
		return json.Valid([]byte(data))
	}
	return validSonicValue(data)
}

type jsonInput interface{ []byte | string }

func validSonicValue[T jsonInput](data T) bool {
	start := skipJSONSpace(data, 0)
	end, ok := scanValueEnd(data, start, 0)
	return ok && skipJSONSpace(data, end) == len(data)
}

// Get resolves path against data and returns the matching AST node.
//
// Explicit SearchOptions use Searcher semantics: ValidateJSON checks only the
// selected value, while CopyReturn copies that selected raw value. With no
// options, non-empty paths retain the existing getPathASCII fast path. An
// empty path preserves NewSearcher's default validation for the first value.
//
// An invalid path element type or a missing node yields ast.ErrNotExist
// or ast.ErrUnsupportType wrapped with context.
func Get(data []byte, opts ast.SearchOptions, path ...interface{}) (ast.Node, error) {
	if len(data) == 0 {
		return ast.Node{}, ast.ErrNotExist
	}
	if compatmode.StdJSON {
		return getStdJSON(data, opts, path...)
	}
	if shouldUseSearcher(opts) {
		return getWithSearcher(data, opts, path...)
	}
	if len(path) > 0 {
		node, status := getPathASCII(data, path)
		switch status {
		case scanFound:
			return node, nil
		case scanMissing:
			return ast.Node{}, ast.ErrNotExist
		case scanInvalid:
			return ast.Node{}, scanSyntaxError(data)
		}
	}
	return ast.NewSearcher(string(data)).GetByPath()
}

// GetString keeps the input as a string throughout lookup. Searcher owns only
// the selected substring when CopyReturn is set; unrelated input is never
// copied. Strict mode validates the whole document before selecting a value.
func GetString(data string, opts ast.SearchOptions, path ...interface{}) (ast.Node, error) {
	if compatmode.StdJSON {
		if !json.Valid([]byte(data)) {
			return ast.Node{}, &ast.SyntaxError{Src: data, Msg: "invalid JSON value", Code: nativetypes.ERR_INVALID_CHAR}
		}
		opts.ValidateJSON = false
	}
	s := ast.NewSearcher(data)
	s.SearchOptions = opts
	return s.GetByPath(path...)
}

func shouldUseSearcher(opts ast.SearchOptions) bool {
	return opts.ValidateJSON || opts.CopyReturn || opts.ConcurrentRead
}

func shouldUseStrictSearcher(opts ast.SearchOptions) bool {
	return opts.CopyReturn || opts.ConcurrentRead
}

func getWithSearcher(data []byte, opts ast.SearchOptions, path ...interface{}) (ast.Node, error) {
	s := ast.NewSearcher(string(data))
	s.SearchOptions = opts
	return s.GetByPath(path...)
}

func getStdJSON(data []byte, opts ast.SearchOptions, path ...interface{}) (ast.Node, error) {
	if !json.Valid(data) {
		dec := json.NewDecoder(bytes.NewReader(data))
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return ast.Node{}, &ast.SyntaxError{Msg: err.Error(), Code: nativetypes.ERR_INVALID_CHAR}
		}
		if err := rejectStdJSONTrailing(dec); err != nil {
			return ast.Node{}, &ast.SyntaxError{Msg: err.Error(), Code: nativetypes.ERR_INVALID_CHAR}
		}
	}
	if shouldUseStrictSearcher(opts) {
		opts.ValidateJSON = false
		return getWithSearcher(data, opts, path...)
	}
	if len(path) > 0 {
		node, status := getPathASCII(data, path)
		switch status {
		case scanFound:
			return node, nil
		case scanMissing:
			return ast.Node{}, ast.ErrNotExist
		case scanInvalid:
			return ast.Node{}, scanSyntaxError(data)
		}
	}
	opts.ValidateJSON = false
	return getWithSearcher(data, opts, path...)
}

func rejectStdJSONTrailing(dec *json.Decoder) error {
	var extra struct{}
	err := dec.Decode(&extra)
	if err == nil {
		return fmt.Errorf("invalid trailing data after top-level value")
	}
	if err.Error() == "EOF" {
		return nil
	}
	return err
}

func scanSyntaxError(data []byte) error {
	return &ast.SyntaxError{
		Src:  string(data),
		Msg:  "invalid JSON value",
		Code: nativetypes.ERR_INVALID_CHAR,
	}
}

type scanStatus int

const (
	scanNoFast scanStatus = iota
	scanFound
	scanMissing
	scanInvalid
)

func getPathASCII(data []byte, path []interface{}) (ast.Node, scanStatus) {
	if !canScanASCII(data) {
		return ast.Node{}, scanNoFast
	}
	rootStart := skipJSONSpace(data, 0)
	if rootStart == len(data) {
		return ast.Node{}, scanInvalid
	}
	start := rootStart
	for _, step := range path {
		switch x := step.(type) {
		case string:
			var status scanStatus
			start, status = findObjectValueStart(data, start, x)
			if status != scanFound {
				return ast.Node{}, status
			}
		case int:
			var status scanStatus
			start, status = findArrayValueStart(data, start, x)
			if status != scanFound {
				return ast.Node{}, status
			}
		case int64:
			idx, ok := intFromInt64(x)
			if !ok {
				return ast.Node{}, scanMissing
			}
			var status scanStatus
			start, status = findArrayValueStart(data, start, idx)
			if status != scanFound {
				return ast.Node{}, status
			}
		case json.Number:
			if idx, err := strconv.Atoi(string(x)); err == nil {
				var status scanStatus
				start, status = findArrayValueStart(data, start, idx)
				if status != scanFound {
					return ast.Node{}, status
				}
			} else {
				var status scanStatus
				start, status = findObjectValueStart(data, start, string(x))
				if status != scanFound {
					return ast.Node{}, status
				}
			}
		default:
			return ast.Node{}, scanNoFast
		}
	}
	end, ok := scanValueEnd(data, start, 0)
	if !ok {
		return ast.Node{}, scanInvalid
	}
	return nodeFromScannedRaw(data[start:end]), scanFound
}

func canScanASCII(data []byte) bool {
	return true
}

func nodeFromScannedRaw(raw []byte) ast.Node {
	if len(raw) == 0 {
		return ast.Node{}
	}
	switch raw[0] {
	case 'n':
		return ast.NewNull()
	case 't':
		return ast.NewBool(true)
	case 'f':
		return ast.NewBool(false)
	case '{', '[', '"':
		return ast.NewRaw(string(raw))
	default:
		return ast.NewNumber(string(raw))
	}
}

func findObjectValueStart(data []byte, start int, key string) (int, scanStatus) {
	if start >= len(data) || data[start] != '{' {
		return 0, scanMissing
	}
	i := skipJSONSpace(data, start+1)
	if i < len(data) && data[i] == '}' {
		return 0, scanMissing
	}
	for i < len(data) {
		if data[i] != '"' {
			return 0, scanInvalid
		}
		keyStart := i + 1
		keyEnd, ok := scanStringEnd(data, i)
		if !ok {
			return 0, scanInvalid
		}
		matches, ok := jsonKeyMatches(data[keyStart:keyEnd-1], key)
		if !ok {
			return 0, scanNoFast
		}
		i = skipJSONSpace(data, keyEnd)
		if i >= len(data) || data[i] != ':' {
			return 0, scanInvalid
		}
		valueStart := skipJSONSpace(data, i+1)
		if matches {
			if valueStart >= len(data) {
				return 0, scanInvalid
			}
			return valueStart, scanFound
		}
		valueEnd, ok := scanPreTargetValueEnd(data, valueStart)
		if !ok {
			return 0, scanInvalid
		}
		i = skipJSONSpace(data, valueEnd)
		if i >= len(data) {
			return 0, scanInvalid
		}
		switch data[i] {
		case ',':
			i = skipJSONSpace(data, i+1)
		case '}':
			return 0, scanMissing
		default:
			return 0, scanInvalid
		}
	}
	return 0, scanInvalid
}

func findArrayValueStart(data []byte, start int, idx int) (int, scanStatus) {
	if idx < 0 {
		return 0, scanMissing
	}
	if start >= len(data) || data[start] != '[' {
		return 0, scanMissing
	}
	i := skipJSONSpace(data, start+1)
	if i < len(data) && data[i] == ']' {
		return 0, scanMissing
	}
	cur := 0
	for i < len(data) {
		if cur == idx {
			return i, scanFound
		}
		valueEnd, ok := scanPreTargetValueEnd(data, i)
		if !ok {
			return 0, scanInvalid
		}
		cur++
		i = skipJSONSpace(data, valueEnd)
		if i >= len(data) {
			return 0, scanInvalid
		}
		switch data[i] {
		case ',':
			i = skipJSONSpace(data, i+1)
		case ']':
			return 0, scanMissing
		default:
			return 0, scanInvalid
		}
	}
	return 0, scanInvalid
}

func findObjectValue(data []byte, start, end int, key string) (int, int, scanStatus) {
	if start >= end || data[start] != '{' {
		return 0, 0, scanMissing
	}
	i := skipJSONSpace(data, start+1)
	if i < end && data[i] == '}' {
		return 0, 0, scanMissing
	}
	for i < end {
		if data[i] != '"' {
			return 0, 0, scanInvalid
		}
		keyStart := i + 1
		keyEnd, ok := scanStringEnd(data, i)
		if !ok {
			return 0, 0, scanInvalid
		}
		matches, ok := jsonKeyMatches(data[keyStart:keyEnd-1], key)
		if !ok {
			return 0, 0, scanNoFast
		}
		i = skipJSONSpace(data, keyEnd)
		if i >= end || data[i] != ':' {
			return 0, 0, scanInvalid
		}
		valueStart := skipJSONSpace(data, i+1)
		valueEnd, ok := scanValueEnd(data, valueStart, 0)
		if !ok || valueEnd > end {
			return 0, 0, scanInvalid
		}
		if matches {
			return valueStart, valueEnd, scanFound
		}
		i = skipJSONSpace(data, valueEnd)
		if i >= end {
			return 0, 0, scanInvalid
		}
		switch data[i] {
		case ',':
			i = skipJSONSpace(data, i+1)
		case '}':
			return 0, 0, scanMissing
		default:
			return 0, 0, scanInvalid
		}
	}
	return 0, 0, scanInvalid
}

func findArrayValue(data []byte, start, end int, idx int) (int, int, scanStatus) {
	if idx < 0 {
		return 0, 0, scanMissing
	}
	if start >= end || data[start] != '[' {
		return 0, 0, scanMissing
	}
	i := skipJSONSpace(data, start+1)
	if i < end && data[i] == ']' {
		return 0, 0, scanMissing
	}
	cur := 0
	for i < end {
		valueStart := i
		valueEnd, ok := scanValueEnd(data, valueStart, 0)
		if !ok || valueEnd > end {
			return 0, 0, scanInvalid
		}
		if cur == idx {
			return valueStart, valueEnd, scanFound
		}
		cur++
		i = skipJSONSpace(data, valueEnd)
		if i >= end {
			return 0, 0, scanInvalid
		}
		switch data[i] {
		case ',':
			i = skipJSONSpace(data, i+1)
		case ']':
			return 0, 0, scanMissing
		default:
			return 0, 0, scanInvalid
		}
	}
	return 0, 0, scanInvalid
}

// maxScanDepth mirrors Sonic's MAX_RECURSE limit (4096); fastjson's
// MaxDepth of 300 would reject valid deep documents Sonic accepts.
const maxScanDepth = 4096

func scanPreTargetValueEnd(data []byte, start int) (int, bool) {
	if start < len(data) && (data[start] == '{' || data[start] == '[') {
		return scanContainerEnd(data, start)
	}
	return scanValueEnd(data, start, 0)
}

func scanContainerEnd(data []byte, start int) (int, bool) {
	expects := make([]byte, 0, 8)
	push := func(c byte) bool {
		if len(expects) == maxScanDepth {
			return false
		}
		if c == '{' {
			expects = append(expects, '}')
		} else {
			expects = append(expects, ']')
		}
		return true
	}
	if !push(data[start]) {
		return 0, false
	}
	inString := false
	escaped := false
	for i := start + 1; i < len(data); i++ {
		c := data[i]
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
			if !push(c) {
				return 0, false
			}
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

func scanValueEnd[T jsonInput](data T, start int, depth int) (int, bool) {
	if depth > maxScanDepth || start >= len(data) {
		return 0, false
	}
	switch data[start] {
	case '{':
		return scanObjectEnd(data, start, depth+1)
	case '[':
		return scanArrayEnd(data, start, depth+1)
	case '"':
		return scanStringEnd(data, start)
	case 't':
		return scanLiteral(data, start, "true")
	case 'f':
		return scanLiteral(data, start, "false")
	case 'n':
		return scanLiteral(data, start, "null")
	default:
		if data[start] == '-' || (data[start] >= '0' && data[start] <= '9') {
			return scanNumberEnd(data, start)
		}
	}
	return 0, false
}

func scanObjectEnd[T jsonInput](data T, start int, depth int) (int, bool) {
	if depth > maxScanDepth {
		return 0, false
	}
	i := skipJSONSpace(data, start+1)
	if i < len(data) && data[i] == '}' {
		return i + 1, true
	}
	for i < len(data) {
		if data[i] != '"' {
			return 0, false
		}
		keyEnd, ok := scanStringEnd(data, i)
		if !ok {
			return 0, false
		}
		i = skipJSONSpace(data, keyEnd)
		if i >= len(data) || data[i] != ':' {
			return 0, false
		}
		i = skipJSONSpace(data, i+1)
		valueEnd, ok := scanValueEnd(data, i, depth)
		if !ok {
			return 0, false
		}
		i = skipJSONSpace(data, valueEnd)
		if i >= len(data) {
			return 0, false
		}
		switch data[i] {
		case ',':
			i = skipJSONSpace(data, i+1)
		case '}':
			return i + 1, true
		default:
			return 0, false
		}
	}
	return 0, false
}

func scanArrayEnd[T jsonInput](data T, start int, depth int) (int, bool) {
	if depth > maxScanDepth {
		return 0, false
	}
	i := skipJSONSpace(data, start+1)
	if i < len(data) && data[i] == ']' {
		return i + 1, true
	}
	for i < len(data) {
		valueEnd, ok := scanValueEnd(data, i, depth)
		if !ok {
			return 0, false
		}
		i = skipJSONSpace(data, valueEnd)
		if i >= len(data) {
			return 0, false
		}
		switch data[i] {
		case ',':
			i = skipJSONSpace(data, i+1)
		case ']':
			return i + 1, true
		default:
			return 0, false
		}
	}
	return 0, false
}

// Short keys avoid search-call overhead. For longer strings, locate quotes
// with the optimized byte search and count their immediately preceding slashes
// to distinguish escaped quotes without rescanning the whole string body.
func scanStringEnd[T jsonInput](data T, start int) (int, bool) {
	escaped := false
	i := start + 1
	shortEnd := min(len(data), i+16)
	for ; i < shortEnd; i++ {
		if escaped {
			escaped = false
			continue
		}
		if data[i] == '\\' {
			escaped = true
			continue
		}
		if data[i] == '"' {
			return i + 1, true
		}
	}
	for i < len(data) {
		off := -1
		switch src := any(data).(type) {
		case string:
			off = strings.IndexByte(src[i:], '"')
		case []byte:
			off = bytes.IndexByte(src[i:], '"')
		}
		if off < 0 {
			return 0, false
		}
		quote := i + off
		slash := quote - 1
		for slash > start && data[slash] == '\\' {
			slash--
		}
		if (quote-slash-1)%2 == 0 {
			return quote + 1, true
		}
		i = quote + 1
	}
	return 0, false
}

func scanLiteral[T jsonInput](data T, start int, lit string) (int, bool) {
	if len(data)-start < len(lit) || string(data[start:start+len(lit)]) != lit {
		return 0, false
	}
	return start + len(lit), true
}

func scanNumberEnd[T jsonInput](data T, start int) (int, bool) {
	i := start
	if data[i] == '-' {
		i++
		if i == len(data) {
			return 0, false
		}
	}
	if data[i] == '0' {
		i++
		// Native Sonic finishes a zero immediately unless a fraction or
		// exponent follows. In particular, a selected "01" is the value 0.
		if i == len(data) || data[i] != '.' && data[i] != 'e' && data[i] != 'E' {
			return i, true
		}
	} else if data[i] >= '1' && data[i] <= '9' {
		for i < len(data) && data[i] >= '0' && data[i] <= '9' {
			i++
		}
	} else {
		return 0, false
	}
	if i < len(data) && data[i] == '.' {
		i++
		if i == len(data) || data[i] < '0' || data[i] > '9' {
			return 0, false
		}
		for i < len(data) && data[i] >= '0' && data[i] <= '9' {
			i++
		}
	}
	if i < len(data) && (data[i] == 'e' || data[i] == 'E') {
		i++
		if i < len(data) && (data[i] == '+' || data[i] == '-') {
			i++
		}
		if i == len(data) || data[i] < '0' || data[i] > '9' {
			return 0, false
		}
		for i < len(data) && data[i] >= '0' && data[i] <= '9' {
			i++
		}
	}
	if i < len(data) {
		switch data[i] {
		case '+', '-', '.', 'e', 'E':
			return 0, false
		}
	}
	return i, true
}

func skipJSONSpace[T jsonInput](data T, i int) int {
	for i < len(data) {
		switch data[i] {
		case ' ', '\n', '\r', '\t':
			i++
		default:
			return i
		}
	}
	return i
}

func isJSONNumberTerminator(c byte) bool {
	switch c {
	case ' ', '\n', '\r', '\t', ',', ']', '}':
		return true
	default:
		return false
	}
}

func asciiKeyEqual(raw []byte, key string) bool {
	if len(raw) != len(key) {
		return false
	}
	for i := range raw {
		if raw[i] != key[i] {
			return false
		}
	}
	return true
}

func jsonKeyMatches(raw []byte, key string) (bool, bool) {
	if bytes.IndexByte(raw, '\\') < 0 {
		return asciiKeyEqual(raw, key), true
	}
	quoted := make([]byte, 0, len(raw)+2)
	quoted = append(quoted, '"')
	quoted = append(quoted, raw...)
	quoted = append(quoted, '"')
	var decoded string
	if err := json.Unmarshal(quoted, &decoded); err != nil {
		return false, false
	}
	return decoded == key, true
}

func intFromInt64(idx int64) (int, bool) {
	if idx > math.MaxInt || idx < math.MinInt {
		return 0, false
	}
	return int(idx), true
}
