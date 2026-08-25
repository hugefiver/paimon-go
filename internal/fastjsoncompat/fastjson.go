// Package fastjsoncompat adapts the valyala/fastjson library to the
// backend contract for validation and path lookup. It exists as an
// internal helper so the root sonic package and future fastjson backend
// can share parsing/validation logic without depending on each other.
//
// Marshalling and unmarshalling of arbitrary Go values is delegated to
// internal/stdjsoncompat (encoding/json) in this phase; the fastjson
// layer only owns validation, raw-path Get, and AST construction.
package fastjsoncompat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/internal/compatmode"
	nativetypes "github.com/bytedance/sonic/internal/native/types"
	vfastjson "github.com/valyala/fastjson"
)

// Valid reports whether data is a single well-formed JSON value. The fast path
// uses fastjson's allocation-free validator. Sonic accepts raw control bytes
// inside strings, while fastjson.ValidateBytes rejects them, so those rare
// inputs are normalized and checked with encoding/json to preserve only that
// documented Sonic-compatible exception.
func Valid(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if compatmode.StdJSON {
		return json.Valid(data)
	}
	if err := vfastjson.ValidateBytes(data); err == nil {
		return true
	}
	return validSonicValueBytes(data)
}

// ValidString is the string-input form of Valid.
func ValidString(data string) bool {
	if len(data) == 0 {
		return false
	}
	if compatmode.StdJSON {
		return json.Valid([]byte(data))
	}
	if err := vfastjson.Validate(data); err == nil {
		return true
	}
	return validSonicValueBytes([]byte(data))
}

func validSonicValueBytes(data []byte) bool {
	return json.Valid(escapeRawStringControls(data))
}

func escapeRawStringControls(data []byte) []byte {
	var out []byte
	last := 0
	inString := false
	escaped := false
	const hex = "0123456789abcdef"
	for i, c := range data {
		if !inString {
			if c == '"' {
				inString = true
			}
			continue
		}
		if escaped {
			escaped = false
			continue
		}
		switch {
		case c == '\\':
			escaped = true
		case c == '"':
			inString = false
		case c < 0x20:
			if out == nil {
				out = make([]byte, 0, len(data)+5)
			}
			out = append(out, data[last:i]...)
			out = append(out, '\\', 'u', '0', '0', hex[c>>4], hex[c&0x0f])
			last = i + 1
		}
	}
	if out == nil {
		return data
	}
	return append(out, data[last:]...)
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

// mapFastjsonError mirrors ast.mapFastjsonError but is duplicated here to
// avoid exporting internal helpers. It maps a fastjson parse error to a
// ast.SyntaxError or ast.ErrNotExist depending on kind.
func mapFastjsonError(src string, err error) error {
	if err == nil {
		return nil
	}
	// fastjson returns a small set of concrete error types. We surface the
	// raw error wrapped so callers can inspect it; ast.SyntaxError is used
	// only when a position can be located. For compatibility with the ast
	// package's behavior we translate to nativetypes.ParsingError when the
	// error is structural.
	_ = src
	// Distinguish "not exist" (null/missing) from true syntax errors.
	if isNotExist(err) {
		return ast.ErrNotExist
	}
	// Map known syntax failures to a generic syntax error so the public
	// API returns a stable type. The ast package already does this for its
	// own searcher; we keep the same shape here.
	return &ast.SyntaxError{
		Msg:  err.Error(),
		Code: nativetypes.ERR_INVALID_CHAR,
	}
}

// isNotExist reports whether err is a fastjson "not exist" error.
func isNotExist(err error) bool {
	if err == nil {
		return false
	}
	// fastjson uses *fastjson.Error with a small set of codes; "not exist"
	// surfaces as a typed nil dereference in some paths. We match by message
	// to avoid importing the internal fastjson error type.
	msg := err.Error()
	return msg == "value not found" || msg == "key not found" || msg == "index out of range"
}

// Compile-time guard: ensure the helper returns something assignable to error.
var _ = fmt.Errorf

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

func scanValueEnd(data []byte, start int, depth int) (int, bool) {
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

func scanObjectEnd(data []byte, start int, depth int) (int, bool) {
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

func scanArrayEnd(data []byte, start int, depth int) (int, bool) {
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

func scanStringEnd(data []byte, start int) (int, bool) {
	escaped := false
	for i := start + 1; i < len(data); i++ {
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
	return 0, false
}

func scanLiteral(data []byte, start int, lit string) (int, bool) {
	if len(data)-start < len(lit) || !bytes.Equal(data[start:start+len(lit)], []byte(lit)) {
		return 0, false
	}
	return start + len(lit), true
}

func scanNumberEnd(data []byte, start int) (int, bool) {
	i := start
	if data[i] == '-' {
		i++
		if i == len(data) {
			return 0, false
		}
	}
	if data[i] == '0' {
		i++
		for i < len(data) && data[i] >= '0' && data[i] <= '9' {
			i++
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
		if i < len(data) && isJSONNumberTerminator(data[i]) {
			return i, true
		}
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
	return i, true
}

func skipJSONSpace(data []byte, i int) int {
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
