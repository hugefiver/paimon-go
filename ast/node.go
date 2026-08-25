package ast

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"

	nativetypes "github.com/bytedance/sonic/internal/native/types"
)

// ---------------------------------------------------------------------------
// Constructors
// ---------------------------------------------------------------------------

// NewNull builds a null Node.
func NewNull() Node { return Node{typ: V_NULL, exists: true, loaded: true} }

// NewBool builds a boolean Node.
func NewBool(v bool) Node {
	if v {
		return Node{typ: V_TRUE, exists: true, loaded: true, boolv: true}
	}
	return Node{typ: V_FALSE, exists: true, loaded: true}
}

// NewString builds a string Node.
func NewString(v string) Node { return Node{typ: V_STRING, exists: true, loaded: true, str: v} }

// NewNumber builds a number Node from a JSON number literal.
func NewNumber(v string) Node {
	return Node{typ: V_NUMBER, exists: true, loaded: true, num: json.Number(v)}
}

// NewArray builds an array Node from a slice of children. The slice is
// copied so the caller may keep mutating its own slice after construction.
func NewArray(v []Node) Node {
	return Node{typ: V_ARRAY, exists: true, loaded: true, arr: append([]Node(nil), v...)}
}

// NewObject builds an object Node from a slice of pairs. The slice is
// copied so the caller may keep mutating its own slice after construction.
func NewObject(v []Pair) Node {
	return Node{typ: V_OBJECT, exists: true, loaded: true, obj: append([]Pair(nil), v...)}
}

// NewPair builds a Pair.
func NewPair(key string, val Node) Pair { return Pair{Key: key, Value: val} }

// NewRaw builds a raw JSON Node. The Node is not parsed until Load / LoadAll
// is called. Before loading, it reports the type of its root JSON token and
// IsRaw() as true.
func NewRaw(j string) Node {
	start := 0
	for start < len(j) && isJSONSpace(j[start]) {
		start++
	}
	if start == len(j) {
		return Node{typ: V_ERROR, exists: true, loaded: true, err: SyntaxError{Pos: start, Src: j, Code: nativetypes.ERR_EOF}}
	}
	end, ok := scanFirstValueEnd(j, start)
	if !ok {
		return Node{typ: V_ERROR, exists: true, loaded: true, err: SyntaxError{Pos: len(j), Src: j, Code: nativetypes.ERR_MISMATCH}}
	}
	raw := j[start:end]
	if !validScannedRootRaw(j, start, end) {
		return Node{typ: V_ERROR, exists: true, loaded: true, err: SyntaxError{Pos: len(raw), Src: raw, Code: nativetypes.ERR_INVALID_CHAR}}
	}
	typ := V_NUMBER
	switch raw[0] {
	case 'n':
		typ = V_NULL
	case 't':
		typ = V_TRUE
	case 'f':
		typ = V_FALSE
	case '[':
		typ = V_ARRAY
	case '{':
		typ = V_OBJECT
	case '"':
		typ = V_STRING
	}
	return Node{typ: typ, exists: true, raw: raw}
}

// NewRawConcurrentRead builds a lazy raw node whose first materialization is
// safe for concurrent pointer-receiver readers. Type, IsRaw, Error, and all
// mutations remain outside that concurrent-read contract. It allocates a mutex
// only when NewRaw produced a valid lazy node.
func NewRawConcurrentRead(j string) Node {
	node := NewRaw(j)
	if node.isRawUnlocked() {
		node.mu = &sync.RWMutex{}
	}
	return node
}

// NewBytes builds a string Node whose value is the base64 (RFC 4648)
// encoding of src. This mirrors Sonic's NewBytes, which produces a JSON
// string (not raw bytes), allowing the value to round-trip through
// JSON marshaling.
func NewBytes(src []byte) Node {
	if len(src) == 0 {
		panic("empty src bytes")
	}
	return NewString(base64.StdEncoding.EncodeToString(src))
}

// NewAny builds a Node wrapping an arbitrary Go value. The node's Type()
// reports V_ANY (Sonic semantics). The original Go value is retained rather
// than converted through JSON so its dynamic type and identity are preserved.
func NewAny(v interface{}) Node {
	switch x := v.(type) {
	case Node:
		return x
	case *Node:
		return *x
	default:
		return Node{typ: V_ANY, exists: true, loaded: true, any: v}
	}
}

// ---------------------------------------------------------------------------
// Type / state queries
// ---------------------------------------------------------------------------

// Type returns the node's type tag. Unloaded raw nodes report the type of
// their root JSON token. Absent nodes return V_NONE.
//
// Deprecated: not concurrent safe. Use TypeSafe instead.
func (n Node) Type() int { return n.typ }

// TypeSafe is identical to Type for this implementation; it is kept on
// the surface because Sonic's TypeSafe panics on an unloaded raw node
// and Type does not. The fastjson-backed implementation here always has
// the same cheap behavior, so they are equivalent.
func (n *Node) TypeSafe() int {
	if n == nil {
		return V_NONE
	}
	n.lockRead()
	defer n.unlockRead()
	return n.typ
}

// Exists reports whether the node refers to a value that exists in the parsed
// JSON. It follows Sonic's type-state predicate: nil, V_NONE, and V_ERROR do
// not exist; every other type exists, including unloaded raw nodes.
func (n *Node) Exists() bool {
	if n == nil {
		return false
	}
	n.lockRead()
	defer n.unlockRead()
	return n.typ != V_ERROR && n.typ != V_NONE
}

// Valid reports whether the node is not an error node. It follows Sonic's
// predicate: nil and V_ERROR are invalid; V_NONE/absent and unloaded raw nodes
// are valid.
func (n *Node) Valid() bool {
	if n == nil {
		return false
	}
	n.lockRead()
	defer n.unlockRead()
	return n.typ != V_ERROR
}

// IsRaw reports whether the node is an unloaded raw JSON node.
//
// Deprecated: not concurrent safe.
func (n Node) IsRaw() bool { return !n.loaded && n.raw != "" }

// Error returns the node's error string. For an absent node it is empty
// (Sonic semantics); for an error node it is the underlying error's
// message; otherwise it is empty. Like Sonic's value-receiver Error method,
// it is not concurrent safe.
func (n Node) Error() string {
	if !n.exists {
		return ""
	}
	if n.err != nil {
		return n.err.Error()
	}
	return ""
}

// Check returns nil unless the receiver is nil or an error node.
func (n *Node) Check() error {
	if n == nil {
		return ErrNotExist
	}
	n.lockRead()
	defer n.unlockRead()
	return n.checkUnlocked()
}

func (n *Node) checkUnlocked() error {
	if n.typ != V_ERROR {
		return nil
	}
	if n.err != nil {
		return n.err
	}
	return n
}

// ---------------------------------------------------------------------------
// Load / LoadAll
// ---------------------------------------------------------------------------

// Load parses the raw JSON of this node only. Container children remain
// raw and are parsed lazily on first access. For this implementation Load
// is equivalent to LoadAll because the fastjson parser deep-copies the
// whole value into Node structures eagerly.
func (n *Node) Load() error { return n.LoadAll() }

// LoadAll parses the raw JSON of this node and recursively all of its
// children into fully realized Node values. An error node propagates
// its underlying error (Sonic semantics) instead of reporting success.
func (n *Node) LoadAll() error {
	if n == nil {
		return ErrNotExist
	}
	if n.mu == nil {
		return n.loadAllUnlocked()
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.loadAllUnlocked()
}

func (n *Node) loadAllUnlocked() error {
	if err := n.checkUnlocked(); err != nil {
		return err
	}
	if !n.isRawUnlocked() {
		return nil
	}
	parsed, perr := parseRawToNode(n.raw)
	if perr != 0 {
		n.typ = V_ERROR
		n.loaded = true
		n.err = perr
		return perr
	}
	assignLoaded(n, &parsed)
	return nil
}

func assignLoaded(dst, src *Node) {
	dst.typ = src.typ
	dst.exists = src.exists
	dst.loaded = src.loaded
	dst.boolv = src.boolv
	dst.raw = src.raw
	dst.str = src.str
	dst.num = src.num
	dst.arr = src.arr
	dst.obj = src.obj
	dst.any = src.any
	dst.err = src.err
}

// ---------------------------------------------------------------------------
// Conversions
// ---------------------------------------------------------------------------

// Bool returns the boolean value of the node. Non-strict bool conversion
// follows Sonic: JSON booleans; numbers (non-zero -> true); the strings
// "true"/"false"; null -> false.
func (n *Node) Bool() (bool, error) {
	if n == nil {
		return false, ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return false, err
	}
	if !n.exists {
		return false, ErrNotExist
	}
	switch n.typ {
	case V_TRUE:
		return true, nil
	case V_FALSE:
		return false, nil
	case V_NULL:
		return false, nil
	case V_NUMBER:
		f, err := strconv.ParseFloat(string(n.num), 64)
		if err != nil {
			return false, err
		}
		return f != 0, nil
	case V_STRING:
		if n.str == "true" {
			return true, nil
		}
		if n.str == "false" {
			return false, nil
		}
		b, err := strconv.ParseBool(n.str)
		if err != nil {
			return false, fmt.Errorf("cannot convert string %q to bool", n.str)
		}
		return b, nil
	case V_ANY:
		return anyBool(n.any)
	}
	return false, fmt.Errorf("cannot convert node type %d to bool", n.typ)
}

// StrictBool returns the boolean value but only if the node is a real
// JSON boolean.
func (n *Node) StrictBool() (bool, error) {
	if n == nil {
		return false, ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return false, err
	}
	if !n.exists {
		return false, ErrNotExist
	}
	switch n.typ {
	case V_TRUE:
		return true, nil
	case V_FALSE:
		return false, nil
	case V_ANY:
		return anyStrictBool(n.any)
	}
	return false, fmt.Errorf("not a bool node, type %d", n.typ)
}

// Int64 returns the node's value as an int64. Non-strict conversion
// follows Sonic: JSON numbers (integers, or the integral part when the
// number parses only as float); numeric strings; true -> 1, false/null
// -> 0.
func (n *Node) Int64() (int64, error) {
	if n == nil {
		return 0, ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return 0, err
	}
	if !n.exists {
		return 0, ErrNotExist
	}
	switch n.typ {
	case V_NUMBER:
		if i, err := strconv.ParseInt(string(n.num), 10, 64); err == nil {
			return i, nil
		}
		f, err := strconv.ParseFloat(string(n.num), 64)
		if err != nil {
			return 0, err
		}
		return int64(f), nil
	case V_STRING:
		if i, err := strconv.ParseInt(n.str, 10, 64); err == nil {
			return i, nil
		}
		f, err := strconv.ParseFloat(n.str, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot convert string %q to int64", n.str)
		}
		return int64(f), nil
	case V_TRUE:
		return 1, nil
	case V_FALSE, V_NULL:
		return 0, nil
	case V_ANY:
		return anyInt64(n.any)
	}
	return 0, fmt.Errorf("cannot convert node type %d to int64", n.typ)
}

// StrictInt64 returns the int64 value but only if the node is a JSON
// number whose numeric value is an integer.
func (n *Node) StrictInt64() (int64, error) {
	if n == nil {
		return 0, ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return 0, err
	}
	if !n.exists {
		return 0, ErrNotExist
	}
	if n.typ == V_ANY {
		return anyStrictInt64(n.any)
	}
	if n.typ != V_NUMBER {
		return 0, fmt.Errorf("not a number node, type %d", n.typ)
	}
	return strconv.ParseInt(string(n.num), 10, 64)
}

// Float64 returns the node's value as a float64. Non-strict conversion
// follows Sonic: JSON numbers; numeric strings; true -> 1, false/null
// -> 0.
func (n *Node) Float64() (float64, error) {
	if n == nil {
		return 0, ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return 0, err
	}
	if !n.exists {
		return 0, ErrNotExist
	}
	switch n.typ {
	case V_NUMBER:
		return strconv.ParseFloat(string(n.num), 64)
	case V_STRING:
		f, err := strconv.ParseFloat(n.str, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot convert string %q to float64", n.str)
		}
		return f, nil
	case V_TRUE:
		return 1, nil
	case V_FALSE, V_NULL:
		return 0, nil
	case V_ANY:
		return anyFloat64(n.any)
	}
	return 0, fmt.Errorf("cannot convert node type %d to float64", n.typ)
}

// StrictFloat64 returns the float64 value but only if the node is a JSON
// number.
func (n *Node) StrictFloat64() (float64, error) {
	if n == nil {
		return 0, ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return 0, err
	}
	if !n.exists {
		return 0, ErrNotExist
	}
	if n.typ == V_ANY {
		return anyStrictFloat64(n.any)
	}
	if n.typ != V_NUMBER {
		return 0, fmt.Errorf("not a number node, type %d", n.typ)
	}
	return strconv.ParseFloat(string(n.num), 64)
}

// Number returns the node's value as a json.Number. Non-strict conversion
// follows Sonic: JSON numbers; numeric strings; true -> "1", false/null
// -> "0".
func (n *Node) Number() (json.Number, error) {
	if n == nil {
		return "", ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return "", err
	}
	if !n.exists {
		return "", ErrNotExist
	}
	switch n.typ {
	case V_NUMBER:
		return n.num, nil
	case V_STRING:
		if _, err := strconv.ParseInt(n.str, 10, 64); err == nil {
			return json.Number(n.str), nil
		}
		if _, err := strconv.ParseFloat(n.str, 64); err == nil {
			return json.Number(n.str), nil
		}
		return "", fmt.Errorf("cannot convert string %q to number", n.str)
	case V_TRUE:
		return json.Number("1"), nil
	case V_FALSE, V_NULL:
		return json.Number("0"), nil
	case V_ANY:
		return anyNumber(n.any)
	}
	return "", fmt.Errorf("cannot convert node type %d to number", n.typ)
}

// StrictNumber returns the json.Number but only if the node is a JSON
// number.
func (n *Node) StrictNumber() (json.Number, error) {
	if n == nil {
		return "", ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return "", err
	}
	if !n.exists {
		return "", ErrNotExist
	}
	if n.typ == V_ANY {
		return anyStrictNumber(n.any)
	}
	if n.typ != V_NUMBER {
		return "", fmt.Errorf("not a number node, type %d", n.typ)
	}
	return n.num, nil
}

// String returns the node's string value. Non-strict conversion accepts
// JSON strings, numbers, and booleans; null returns "".
func (n *Node) String() (string, error) {
	if n == nil {
		return "", ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return "", err
	}
	if !n.exists {
		return "", ErrNotExist
	}
	switch n.typ {
	case V_STRING:
		return n.str, nil
	case V_NUMBER:
		return string(n.num), nil
	case V_TRUE:
		return "true", nil
	case V_FALSE:
		return "false", nil
	case V_NULL:
		return "", nil
	case V_ANY:
		return anyString(n.any)
	}
	return "", fmt.Errorf("cannot convert node type %d to string", n.typ)
}

// StrictString returns the string value but only if the node is a JSON
// string.
func (n *Node) StrictString() (string, error) {
	if n == nil {
		return "", ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return "", err
	}
	if !n.exists {
		return "", ErrNotExist
	}
	if n.typ == V_ANY {
		return anyStrictString(n.any)
	}
	if n.typ != V_STRING {
		return "", fmt.Errorf("not a string node, type %d", n.typ)
	}
	return n.str, nil
}

func anyBool(v interface{}) (bool, error) {
	switch v := v.(type) {
	case bool:
		return v, nil
	case int:
		return v != 0, nil
	case int8:
		return v != 0, nil
	case int16:
		return v != 0, nil
	case int32:
		return v != 0, nil
	case int64:
		return v != 0, nil
	case uint:
		return v != 0, nil
	case uint8:
		return v != 0, nil
	case uint16:
		return v != 0, nil
	case uint32:
		return v != 0, nil
	case uint64:
		return v != 0, nil
	case float32:
		return v != 0, nil
	case float64:
		return v != 0, nil
	case string:
		return strconv.ParseBool(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i != 0, nil
		}
		f, err := v.Float64()
		if err != nil {
			return false, err
		}
		return f != 0, nil
	default:
		return false, ErrUnsupportType
	}
}

func anyStrictBool(v interface{}) (bool, error) {
	if v, ok := v.(bool); ok {
		return v, nil
	}
	return false, ErrUnsupportType
}

func anyInt64(v interface{}) (int64, error) {
	switch v := v.(type) {
	case bool:
		if v {
			return 1, nil
		}
		return 0, nil
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return int64(v), nil
	case float32:
		return int64(v), nil
	case float64:
		return int64(v), nil
	case string:
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i, nil
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, err
		}
		return int64(f), nil
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i, nil
		}
		f, err := v.Float64()
		if err != nil {
			return 0, err
		}
		return int64(f), nil
	default:
		return 0, ErrUnsupportType
	}
}

func anyStrictInt64(v interface{}) (int64, error) {
	switch v := v.(type) {
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return int64(v), nil
	case json.Number:
		return v.Int64()
	default:
		return 0, ErrUnsupportType
	}
}

func anyFloat64(v interface{}) (float64, error) {
	switch v := v.(type) {
	case bool:
		if v {
			return 1, nil
		}
		return 0, nil
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case string:
		return strconv.ParseFloat(v, 64)
	case json.Number:
		return v.Float64()
	default:
		return 0, ErrUnsupportType
	}
}

func anyStrictFloat64(v interface{}) (float64, error) {
	switch v := v.(type) {
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	default:
		return 0, ErrUnsupportType
	}
}

func anyNumber(v interface{}) (json.Number, error) {
	switch v := v.(type) {
	case bool:
		return boolNumber(v), nil
	case int:
		return boolNumber(v != 0), nil
	case int8:
		return boolNumber(v != 0), nil
	case int16:
		return boolNumber(v != 0), nil
	case int32:
		return boolNumber(v != 0), nil
	case int64:
		return boolNumber(v != 0), nil
	case uint:
		return boolNumber(v != 0), nil
	case uint8:
		return boolNumber(v != 0), nil
	case uint16:
		return boolNumber(v != 0), nil
	case uint32:
		return boolNumber(v != 0), nil
	case uint64:
		return boolNumber(v != 0), nil
	case float32:
		return boolNumber(v != 0), nil
	case float64:
		return boolNumber(v != 0), nil
	case string:
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return "", err
		}
		return json.Number(v), nil
	case json.Number:
		return v, nil
	default:
		return "", ErrUnsupportType
	}
}

func anyStrictNumber(v interface{}) (json.Number, error) {
	if number, ok := v.(json.Number); ok {
		return number, nil
	}
	return "", ErrUnsupportType
}

func boolNumber(v bool) json.Number {
	if v {
		return json.Number("1")
	}
	return json.Number("0")
}

func anyString(v interface{}) (string, error) {
	switch v := v.(type) {
	case bool:
		return strconv.FormatBool(v), nil
	case int:
		return strconv.Itoa(int(v)), nil
	case int8:
		return strconv.Itoa(int(v)), nil
	case int16:
		return strconv.Itoa(int(v)), nil
	case int32:
		return strconv.Itoa(int(v)), nil
	case int64:
		return strconv.Itoa(int(v)), nil
	case uint:
		return strconv.Itoa(int(v)), nil
	case uint8:
		return strconv.Itoa(int(v)), nil
	case uint16:
		return strconv.Itoa(int(v)), nil
	case uint32:
		return strconv.Itoa(int(v)), nil
	case uint64:
		return strconv.Itoa(int(v)), nil
	case float32:
		return strconv.FormatFloat(float64(v), 'g', -1, 64), nil
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	case string:
		return v, nil
	case json.Number:
		return v.String(), nil
	default:
		return "", ErrUnsupportType
	}
}

func anyStrictString(v interface{}) (string, error) {
	if v, ok := v.(string); ok {
		return v, nil
	}
	return "", ErrUnsupportType
}

// ---------------------------------------------------------------------------
// Container accessors
// ---------------------------------------------------------------------------

// Len returns the number of children of an array or object node, the
// length of a string node, and 0 for absent/null nodes (Sonic
// semantics).
func (n *Node) Len() (int, error) {
	if n == nil {
		return 0, ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return 0, err
	}
	if n.typ == V_NONE || n.typ == V_NULL {
		return 0, nil
	}
	if !n.exists {
		return 0, ErrNotExist
	}
	switch n.typ {
	case V_ARRAY:
		return len(n.arr), nil
	case V_OBJECT:
		return len(n.obj), nil
	case V_STRING:
		return len(n.str), nil
	}
	return 0, fmt.Errorf("node type %d has no length", n.typ)
}

// Cap returns the capacity of an array or object node's backing storage.
func (n *Node) Cap() (int, error) {
	if n == nil {
		return 0, ErrNotExist
	}
	if err := n.Check(); err != nil {
		return 0, err
	}
	if err := n.ensureLoaded(); err != nil {
		return 0, err
	}
	switch n.typ {
	case V_ARRAY:
		return cap(n.arr), nil
	case V_OBJECT:
		return cap(n.obj), nil
	case V_NONE, V_NULL:
		return 0, nil
	default:
		return 0, ErrUnsupportType
	}
}

// Get returns a pointer to the child of an object node by key. It only
// addresses objects (Sonic semantics); array lookups go through Index.
// The returned pointer aliases the node's internal storage and is
// invalidated by mutations that reallocate the underlying slice.
func (n *Node) Get(key string) *Node {
	if n == nil {
		return newMissing()
	}
	if err := n.Check(); err != nil {
		return newErrorNode(err)
	}
	if err := n.ensureLoaded(); err != nil {
		return newErrorNode(err)
	}
	if !n.exists {
		return newMissing()
	}
	if n.typ == V_OBJECT {
		for i := range n.obj {
			if n.obj[i].Key == key {
				return &n.obj[i].Value
			}
		}
		return nil
	}
	return newErrorNode(ErrUnsupportType)
}

// Index returns a pointer to the i-th child of an array node, or to the
// value of the i-th pair of an object node (Sonic semantics: both
// container kinds are indexable). Positive out-of-range array indices
// return nil, while object indices return an ErrNotExist error node.
func (n *Node) Index(idx int) *Node {
	if n == nil {
		return newMissing()
	}
	if err := n.Check(); err != nil {
		return newErrorNode(err)
	}
	if err := n.ensureLoaded(); err != nil {
		return newErrorNode(err)
	}
	if !n.exists {
		return newMissing()
	}
	switch n.typ {
	case V_ARRAY:
		if idx < 0 || idx >= len(n.arr) {
			if idx >= 0 {
				return nil
			}
			return newMissing()
		}
		return &n.arr[idx]
	case V_OBJECT:
		if idx < 0 || idx >= len(n.obj) {
			if idx >= 0 {
				return newErrorNode(ErrNotExist)
			}
			return newMissing()
		}
		return &n.obj[idx].Value
	}
	return newErrorNode(ErrUnsupportType)
}

// GetByPath walks the node following a path of string keys and integer
// indices and returns a pointer to the resolved child.
func (n *Node) GetByPath(path ...interface{}) *Node {
	cur := n
	for _, step := range path {
		if cur == nil {
			return newMissing()
		}
		switch s := step.(type) {
		case string:
			cur = cur.Get(s)
		case int:
			cur = cur.Index(s)
		case int64:
			idx, ok := intFromInt64(s)
			if !ok {
				return newMissing()
			}
			cur = cur.Index(idx)
		case json.Number:
			if idx, err := strconv.Atoi(string(s)); err == nil {
				cur = cur.Index(idx)
			} else {
				cur = cur.Get(string(s))
			}
		default:
			return newMissing()
		}
	}
	return cur
}

// IndexOrGet returns the child of an object node by key after first
// trying to match the idx-th pair's key (Sonic semantics: object-only
// lookup).
func (n *Node) IndexOrGet(idx int, key string) *Node {
	if n == nil {
		return newMissing()
	}
	if err := n.ensureLoaded(); err != nil {
		return newErrorNode(err)
	}
	if !n.exists {
		return newMissing()
	}
	if n.typ != V_OBJECT {
		return newMissing()
	}
	if idx >= 0 && idx < len(n.obj) && n.obj[idx].Key == key {
		return &n.obj[idx].Value
	}
	for i := range n.obj {
		if n.obj[i].Key == key {
			return &n.obj[i].Value
		}
	}
	return newMissing()
}

// IndexOrGetWithIdx is like IndexOrGet but also returns the resolved
// position of the child in the parent's underlying slice, or -1 when
// the child is not present.
func (n *Node) IndexOrGetWithIdx(idx int, key string) (*Node, int) {
	if n == nil {
		return newMissing(), -1
	}
	if err := n.ensureLoaded(); err != nil {
		return newErrorNode(err), -1
	}
	if !n.exists {
		return newMissing(), -1
	}
	if n.typ != V_OBJECT {
		return newErrorNode(ErrUnsupportType), idx
	}
	for i := range n.obj {
		if n.obj[i].Key == key {
			return &n.obj[i].Value, i
		}
	}
	return newMissing(), -1
}

// IndexPair returns a pointer to the i-th Pair of an object node.
func (n *Node) IndexPair(idx int) *Pair {
	if n == nil {
		return nil
	}
	if err := n.ensureLoaded(); err != nil {
		return nil
	}
	if !n.exists {
		return nil
	}
	if n.typ != V_OBJECT {
		return nil
	}
	if idx < 0 || idx >= len(n.obj) {
		return nil
	}
	return &n.obj[idx]
}

// ---------------------------------------------------------------------------
// Iteration
// ---------------------------------------------------------------------------

// ForEach iterates over the children of an array or object node, calling
// fn for each child. Iteration stops if fn returns false. A scalar node
// invokes the callback once with Sequence{Index: -1} (Sonic semantics).
func (n *Node) ForEach(fn Scanner) error {
	if err := n.Check(); err != nil {
		return err
	}
	if err := n.ensureLoaded(); err != nil {
		return err
	}
	switch n.typ {
	case V_ARRAY:
		for i := range n.arr {
			seq := Sequence{Index: i}
			if !fn(seq, &n.arr[i]) {
				return nil
			}
		}
	case V_OBJECT:
		for i := range n.obj {
			key := n.obj[i].Key
			seq := Sequence{Index: i, Key: &key}
			if !fn(seq, &n.obj[i].Value) {
				return nil
			}
		}
	default:
		seq := Sequence{Index: -1}
		fn(seq, n)
	}
	return nil
}

// Values returns a ListIterator over an array node's children.
func (n *Node) Values() (ListIterator, error) {
	if n == nil {
		return ListIterator{}, ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return ListIterator{}, err
	}
	if !n.exists {
		return ListIterator{}, ErrNotExist
	}
	if n.typ != V_ARRAY {
		return ListIterator{}, fmt.Errorf("node type %d is not an array", n.typ)
	}
	return ListIterator{Iterator: Iterator{node: n}}, nil
}

// Properties returns an ObjectIterator over an object node's pairs.
func (n *Node) Properties() (ObjectIterator, error) {
	if n == nil {
		return ObjectIterator{}, ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return ObjectIterator{}, err
	}
	if !n.exists {
		return ObjectIterator{}, ErrNotExist
	}
	if n.typ != V_OBJECT {
		return ObjectIterator{}, fmt.Errorf("node type %d is not an object", n.typ)
	}
	return ObjectIterator{Iterator: Iterator{node: n}}, nil
}

// ---------------------------------------------------------------------------
// Interface conversions
// ---------------------------------------------------------------------------

// Interface returns the JSON value of the node as a Go value. Numbers
// are returned as float64.
func (n *Node) Interface() (interface{}, error) {
	return n.interfaceWith(false, false)
}

// InterfaceUseNumber is like Interface but numbers are returned as
// json.Number.
func (n *Node) InterfaceUseNumber() (interface{}, error) {
	return n.interfaceWith(true, false)
}

// InterfaceUseNode is like Interface but container children are returned
// as Node values ([]Node for arrays, map[string]Node for objects).
func (n *Node) InterfaceUseNode() (interface{}, error) {
	if n == nil {
		return nil, ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return nil, err
	}
	if !n.exists {
		return nil, ErrNotExist
	}
	if n.typ == V_ANY {
		return *n, nil
	}
	return n.interfaceWith(false, true)
}

func (n *Node) interfaceWith(useNumber, useNode bool) (interface{}, error) {
	if n == nil {
		return nil, ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return nil, err
	}
	if !n.exists {
		return nil, ErrNotExist
	}
	switch n.typ {
	case V_ANY:
		return n.any, nil
	case V_NULL:
		return nil, nil
	case V_TRUE:
		return true, nil
	case V_FALSE:
		return false, nil
	case V_STRING:
		return n.str, nil
	case V_NUMBER:
		if useNumber {
			return n.num, nil
		}
		f, err := strconv.ParseFloat(string(n.num), 64)
		if err != nil {
			return nil, err
		}
		return f, nil
	case V_ARRAY:
		if useNode {
			out := make([]Node, len(n.arr))
			copy(out, n.arr)
			return out, nil
		}
		out := make([]interface{}, len(n.arr))
		for i := range n.arr {
			v, err := n.arr[i].interfaceWith(useNumber, false)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case V_OBJECT:
		if useNode {
			out := make(map[string]Node, len(n.obj))
			for i := range n.obj {
				out[n.obj[i].Key] = n.obj[i].Value
			}
			return out, nil
		}
		out := make(map[string]interface{}, len(n.obj))
		for i := range n.obj {
			v, err := n.obj[i].Value.interfaceWith(useNumber, false)
			if err != nil {
				return nil, err
			}
			out[n.obj[i].Key] = v
		}
		return out, nil
	}
	return nil, fmt.Errorf("cannot convert node type %d to interface", n.typ)
}

// Array returns the array node's values as []interface{}.
func (n *Node) Array() ([]interface{}, error) {
	v, err := n.Interface()
	if err != nil {
		return nil, err
	}
	if arr, ok := v.([]interface{}); ok {
		return arr, nil
	}
	return nil, fmt.Errorf("node is not an array")
}

// ArrayUseNumber returns the array as []interface{} but with numbers as
// json.Number.
func (n *Node) ArrayUseNumber() ([]interface{}, error) {
	v, err := n.InterfaceUseNumber()
	if err != nil {
		return nil, err
	}
	if arr, ok := v.([]interface{}); ok {
		return arr, nil
	}
	return nil, fmt.Errorf("node is not an array")
}

// ArrayUseNode returns the array as []Node.
func (n *Node) ArrayUseNode() ([]Node, error) {
	if n == nil {
		return nil, ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return nil, err
	}
	if !n.exists {
		return nil, ErrNotExist
	}
	if n.typ == V_ANY {
		if array, ok := n.any.([]Node); ok {
			return array, nil
		}
		return nil, ErrUnsupportType
	}
	if n.typ != V_ARRAY {
		return nil, fmt.Errorf("node is not an array")
	}
	out := make([]Node, len(n.arr))
	copy(out, n.arr)
	return out, nil
}

// Map returns the object node's entries as map[string]interface{}.
func (n *Node) Map() (map[string]interface{}, error) {
	v, err := n.Interface()
	if err != nil {
		return nil, err
	}
	if m, ok := v.(map[string]interface{}); ok {
		return m, nil
	}
	return nil, fmt.Errorf("node is not an object")
}

// MapUseNumber returns the object as map[string]interface{} but with
// numbers as json.Number.
func (n *Node) MapUseNumber() (map[string]interface{}, error) {
	v, err := n.InterfaceUseNumber()
	if err != nil {
		return nil, err
	}
	if m, ok := v.(map[string]interface{}); ok {
		return m, nil
	}
	return nil, fmt.Errorf("node is not an object")
}

// MapUseNode returns the object as map[string]Node.
func (n *Node) MapUseNode() (map[string]Node, error) {
	if n == nil {
		return nil, ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return nil, err
	}
	if !n.exists {
		return nil, ErrNotExist
	}
	if n.typ == V_ANY {
		if object, ok := n.any.(map[string]Node); ok {
			return object, nil
		}
		return nil, ErrUnsupportType
	}
	v, err := n.InterfaceUseNode()
	if err != nil {
		return nil, err
	}
	if m, ok := v.(map[string]Node); ok {
		return m, nil
	}
	return nil, fmt.Errorf("node is not an object")
}

// ---------------------------------------------------------------------------
// Mutation
// ---------------------------------------------------------------------------

// Add appends a node to an array node. An absent (V_NONE) or null node
// is promoted to an array first, matching Sonic (including zero-value
// Nodes).
func (n *Node) Add(child Node) error {
	if n == nil {
		return ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return err
	}
	if n.typ == V_NONE || n.typ == V_NULL {
		replacement := NewArray([]Node{child})
		assignLoaded(n, &replacement)
		return nil
	}
	if !n.exists {
		return ErrNotExist
	}
	if n.typ != V_ARRAY {
		return fmt.Errorf("cannot Add to node type %d", n.typ)
	}
	n.arr = append(n.arr, child)
	return nil
}

// AddAny is like Add but builds the node from an arbitrary Go value.
func (n *Node) AddAny(v interface{}) error {
	return n.Add(NewAny(v))
}

// Set sets the value of an object entry by key. If the key does not exist
// it is appended. The returned bool reports whether the key already
// existed (Sonic semantics: false for a newly added key, true when an
// existing entry was overwritten). An absent (V_NONE) or null node is
// promoted to an object first, matching Sonic.
func (n *Node) Set(key string, node Node) (bool, error) {
	if n == nil {
		return false, ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return false, err
	}
	if node.typ == V_ERROR {
		return false, node.Check()
	}
	if n.typ == V_NONE || n.typ == V_NULL {
		replacement := NewObject([]Pair{NewPair(key, node)})
		assignLoaded(n, &replacement)
		return false, nil
	}
	if !n.exists {
		return false, ErrNotExist
	}
	if n.typ != V_OBJECT {
		return false, fmt.Errorf("cannot Set on node type %d", n.typ)
	}
	for i := range n.obj {
		if n.obj[i].Key == key {
			n.obj[i].Value = node
			return true, nil
		}
	}
	n.obj = append(n.obj, Pair{Key: key, Value: node})
	return false, nil
}

// SetAny is like Set but builds the node from an arbitrary Go value.
func (n *Node) SetAny(key string, val interface{}) (bool, error) {
	return n.Set(key, NewAny(val))
}

// SetByIndex sets the i-th child of an array or object node (Sonic
// semantics: the index must address an existing child). The returned
// bool reports whether an existing child was overwritten. An absent
// (V_NONE) or null node is promoted to an array when idx == 0, matching
// Sonic; out-of-range indices return ErrNotExist.
func (n *Node) SetByIndex(idx int, node Node) (bool, error) {
	if n == nil {
		return false, ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return false, err
	}
	if node.typ == V_ERROR {
		return false, node.Check()
	}
	if idx == 0 && (n.typ == V_NONE || n.typ == V_NULL) {
		replacement := NewArray([]Node{node})
		assignLoaded(n, &replacement)
		return false, nil
	}
	if !n.exists {
		return false, ErrNotExist
	}
	switch n.typ {
	case V_ARRAY:
		if idx < 0 || idx >= len(n.arr) {
			return false, ErrNotExist
		}
		n.arr[idx] = node
		return true, nil
	case V_OBJECT:
		if idx < 0 || idx >= len(n.obj) {
			return false, ErrNotExist
		}
		n.obj[idx].Value = node
		return true, nil
	}
	return false, fmt.Errorf("cannot SetByIndex on node type %d", n.typ)
}

// SetAnyByIndex is like SetByIndex but builds the node from an arbitrary
// Go value.
func (n *Node) SetAnyByIndex(idx int, val interface{}) (bool, error) {
	return n.SetByIndex(idx, NewAny(val))
}

// Unset removes the object entry with the given key. The returned bool
// reports whether a key was removed.
func (n *Node) Unset(key string) (bool, error) {
	if n == nil || !n.exists {
		return false, ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return false, err
	}
	if n.typ != V_OBJECT {
		return false, fmt.Errorf("cannot Unset on node type %d", n.typ)
	}
	for i := range n.obj {
		if n.obj[i].Key == key {
			n.obj = append(n.obj[:i], n.obj[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

// UnsetByIndex removes the i-th child. For arrays it removes the array
// element; for objects it removes the pair. The returned bool reports
// whether an element was removed.
func (n *Node) UnsetByIndex(idx int) (bool, error) {
	if n == nil || !n.exists {
		return false, ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return false, err
	}
	switch n.typ {
	case V_ARRAY:
		if idx < 0 || idx >= len(n.arr) {
			return false, ErrNotExist
		}
		n.arr = append(n.arr[:idx], n.arr[idx+1:]...)
		return true, nil
	case V_OBJECT:
		if idx < 0 || idx >= len(n.obj) {
			return false, ErrNotExist
		}
		n.obj = append(n.obj[:idx], n.obj[idx+1:]...)
		return true, nil
	}
	return false, fmt.Errorf("cannot UnsetByIndex on node type %d", n.typ)
}

// Pop removes the last element of an array node, or the last pair of an
// object node (Sonic semantics).
func (n *Node) Pop() error {
	if n == nil || !n.exists {
		return ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return err
	}
	switch n.typ {
	case V_ARRAY:
		if len(n.arr) == 0 {
			return nil
		}
		n.arr = n.arr[:len(n.arr)-1]
		return nil
	case V_OBJECT:
		if len(n.obj) == 0 {
			return nil
		}
		n.obj = n.obj[:len(n.obj)-1]
		return nil
	}
	return fmt.Errorf("cannot Pop on node type %d", n.typ)
}

// Move moves the element at position src to position dst in an array
// node, shifting the elements in between.
func (n *Node) Move(dst, src int) error {
	if n == nil || !n.exists {
		return ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return err
	}
	if n.typ != V_ARRAY {
		return fmt.Errorf("cannot Move on node type %d", n.typ)
	}
	if src < 0 || src >= len(n.arr) {
		return fmt.Errorf("src index %d out of range [0,%d)", src, len(n.arr))
	}
	if dst < 0 || dst >= len(n.arr) {
		return fmt.Errorf("dst index %d out of range [0,%d)", dst, len(n.arr))
	}
	if src == dst {
		return nil
	}
	v := n.arr[src]
	if src < dst {
		copy(n.arr[src:dst+1], n.arr[src+1:dst+1])
	} else {
		copy(n.arr[dst+1:src+1], n.arr[dst:src])
	}
	n.arr[dst] = v
	return nil
}

// SortKeys sorts the keys of an object node. When recurse is true, SortKeys
// also recursively sorts the keys of any nested object children. For an array
// receiver, it always crosses nested arrays to reach object containers; recurse
// controls traversal below those objects.
func (n *Node) SortKeys(recurse bool) error {
	if n == nil || !n.exists {
		return ErrNotExist
	}
	if err := n.ensureLoaded(); err != nil {
		return err
	}
	if n.typ == V_ARRAY {
		return sortKeysArray(n, recurse)
	}
	return sortKeysNode(n, recurse)
}

// sortKeysArray crosses nested array containers regardless of recurse. Once
// it reaches an object, recurse controls traversal below that object.
func sortKeysArray(n *Node, recurse bool) error {
	for i := range n.arr {
		if err := sortKeysContainer(&n.arr[i], recurse); err != nil {
			return err
		}
	}
	return nil
}

func sortKeysContainer(n *Node, recurse bool) error {
	switch n.typ {
	case V_ARRAY, V_OBJECT:
		return n.SortKeys(recurse)
	}
	return nil
}

func sortKeysNode(n *Node, recurse bool) error {
	if n == nil {
		return nil
	}
	switch n.typ {
	case V_OBJECT:
		sort.SliceStable(n.obj, func(i, j int) bool { return n.obj[i].Key < n.obj[j].Key })
		if recurse {
			for i := range n.obj {
				if err := sortKeysContainer(&n.obj[i].Value, recurse); err != nil {
					return err
				}
			}
		}
	case V_ARRAY:
		if recurse {
			return sortKeysArray(n, recurse)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Raw / MarshalJSON
// ---------------------------------------------------------------------------

// Raw returns the compact JSON serialization of the node. For unloaded
// raw nodes it returns the raw text as given (compacted once on demand).
// Error nodes propagate their underlying error (Sonic semantics).
func (n *Node) Raw() (string, error) {
	if n == nil {
		return "", ErrNotExist
	}
	n.lockRead()
	defer n.unlockRead()
	if !n.exists {
		return "", ErrNotExist
	}
	if n.typ == V_ERROR {
		if err := n.checkUnlocked(); err != nil {
			return "", err
		}
	}
	if n.isRawUnlocked() {
		return n.raw, nil
	}
	if n.typ == V_ANY {
		b, err := json.Marshal(n.any)
		return string(b), err
	}
	var b []byte
	var err error
	b, err = appendNodeJSONUnlocked(b, n, false)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// MarshalJSON implements json.Marshaler. Error nodes propagate their
// underlying error (Sonic semantics).
func (n *Node) MarshalJSON() ([]byte, error) {
	if n == nil {
		return nil, ErrNotExist
	}
	n.lockRead()
	defer n.unlockRead()
	if !n.exists {
		return nil, ErrNotExist
	}
	if n.typ == V_ERROR {
		if err := n.checkUnlocked(); err != nil {
			return nil, err
		}
	}
	if n.isRawUnlocked() {
		return []byte(n.raw), nil
	}
	if n.typ == V_ANY {
		return json.Marshal(n.any)
	}
	var b []byte
	var err error
	b, err = appendNodeJSONUnlocked(b, n, false)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// UnmarshalJSON implements json.Unmarshaler. It replaces the node with
// a raw node wrapping the given data; Load is deferred.
func (n *Node) UnmarshalJSON(data []byte) error {
	if n == nil {
		return errors.New("UnmarshalJSON on nil Node")
	}
	replacement := NewRaw(string(data))
	assignLoaded(n, &replacement)
	return nil
}

// ---------------------------------------------------------------------------
// nodeFromInterface
// ---------------------------------------------------------------------------

// nodeFromInterface builds a Node from an arbitrary Go value. Supported
// types are nil, bool, string, json.Number, all signed/unsigned/float
// numeric kinds, []interface{}, []Node, map[string]interface{},
// map[string]Node, []Pair, and existing Node. Other types fall back to
// encoding/json marshal + parse; if that fails ErrUnsupportType is
// returned.
func nodeFromInterface(v interface{}) (Node, error) {
	if v == nil {
		return NewNull(), nil
	}
	switch x := v.(type) {
	case Node:
		return x, nil
	case *Node:
		if x == nil {
			return NewNull(), nil
		}
		return *x, nil
	case bool:
		return NewBool(x), nil
	case string:
		return NewString(x), nil
	case json.Number:
		return NewNumber(string(x)), nil
	case int:
		return NewNumber(strconv.Itoa(x)), nil
	case int8:
		return NewNumber(strconv.FormatInt(int64(x), 10)), nil
	case int16:
		return NewNumber(strconv.FormatInt(int64(x), 10)), nil
	case int32:
		return NewNumber(strconv.FormatInt(int64(x), 10)), nil
	case int64:
		return NewNumber(strconv.FormatInt(x, 10)), nil
	case uint:
		return NewNumber(strconv.FormatUint(uint64(x), 10)), nil
	case uint8:
		return NewNumber(strconv.FormatUint(uint64(x), 10)), nil
	case uint16:
		return NewNumber(strconv.FormatUint(uint64(x), 10)), nil
	case uint32:
		return NewNumber(strconv.FormatUint(uint64(x), 10)), nil
	case uint64:
		return NewNumber(strconv.FormatUint(x, 10)), nil
	case float32:
		return NewNumber(strconv.FormatFloat(float64(x), 'g', -1, 32)), nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return Node{typ: V_ERROR, exists: true, loaded: true, err: ErrUnsupportType}, ErrUnsupportType
		}
		return NewNumber(strconv.FormatFloat(x, 'g', -1, 64)), nil
	case []interface{}:
		arr := make([]Node, 0, len(x))
		for _, e := range x {
			n, err := nodeFromInterface(e)
			if err != nil {
				return Node{}, err
			}
			arr = append(arr, n)
		}
		return NewArray(arr), nil
	case []Node:
		return NewArray(x), nil
	case map[string]interface{}:
		pairs := make([]Pair, 0, len(x))
		for k, e := range x {
			n, err := nodeFromInterface(e)
			if err != nil {
				return Node{}, err
			}
			pairs = append(pairs, NewPair(k, n))
		}
		// Stable order: sort by key so output is deterministic.
		sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].Key < pairs[j].Key })
		return NewObject(pairs), nil
	case map[string]Node:
		pairs := make([]Pair, 0, len(x))
		for k, e := range x {
			pairs = append(pairs, NewPair(k, e))
		}
		sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].Key < pairs[j].Key })
		return NewObject(pairs), nil
	case []Pair:
		return NewObject(x), nil
	}
	// Fallback: try encoding/json marshal + parse.
	b, err := json.Marshal(v)
	if err != nil {
		return Node{typ: V_ERROR, exists: true, loaded: true, err: ErrUnsupportType}, ErrUnsupportType
	}
	return NewRaw(string(b)), nil
}

// ---------------------------------------------------------------------------
// JSON serialization helpers
// ---------------------------------------------------------------------------

func appendNodeJSON(dst []byte, n *Node, escapeHTML bool) ([]byte, error) {
	if n == nil {
		return append(dst, "null"...), nil
	}
	n.lockRead()
	defer n.unlockRead()
	return appendNodeJSONUnlocked(dst, n, escapeHTML)
}

func appendNodeJSONUnlocked(dst []byte, n *Node, escapeHTML bool) ([]byte, error) {
	if !n.exists {
		return append(dst, "null"...), nil
	}
	if n.isRawUnlocked() {
		return append(dst, n.raw...), nil
	}
	switch n.typ {
	case V_NULL:
		return append(dst, "null"...), nil
	case V_TRUE:
		return append(dst, "true"...), nil
	case V_FALSE:
		return append(dst, "false"...), nil
	case V_STRING:
		return appendStringJSON(dst, n.str, escapeHTML), nil
	case V_NUMBER:
		return append(dst, string(n.num)...), nil
	case V_ARRAY:
		dst = append(dst, '[')
		for i := range n.arr {
			if i > 0 {
				dst = append(dst, ',')
			}
			var err error
			dst, err = appendNodeJSON(dst, &n.arr[i], escapeHTML)
			if err != nil {
				return dst, err
			}
		}
		return append(dst, ']'), nil
	case V_OBJECT:
		dst = append(dst, '{')
		for i := range n.obj {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendStringJSON(dst, n.obj[i].Key, escapeHTML)
			dst = append(dst, ':')
			var err error
			dst, err = appendNodeJSON(dst, &n.obj[i].Value, escapeHTML)
			if err != nil {
				return dst, err
			}
		}
		return append(dst, '}'), nil
	case V_ANY:
		b, err := json.Marshal(n.any)
		if err != nil {
			return dst, err
		}
		return append(dst, b...), nil
	case V_ERROR, V_NONE:
		// Should not happen if Load was called; emit null defensively.
		return append(dst, "null"...), nil
	}
	return append(dst, "null"...), nil
}

func appendStringJSON(dst []byte, s string, escapeHTML bool) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); {
		b := s[i]
		if b < 0x20 {
			switch b {
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				dst = append(dst, '\\', 'u', '0', '0', hexDigit(b>>4), hexDigit(b&0x0f))
			}
			i++
			continue
		}
		if escapeHTML && (b == '<' || b == '>' || b == '&') {
			dst = append(dst, '\\', 'u', '0', '0', hexDigit(b>>4), hexDigit(b&0x0f))
			i++
			continue
		}
		if b == '\\' || b == '"' {
			dst = append(dst, '\\', b)
			i++
			continue
		}
		// ASCII fast path.
		if b < 0x80 {
			dst = append(dst, b)
			i++
			continue
		}
		// Multi-byte UTF-8: copy whole rune verbatim.
		r, size := decodeRune(s[i:])
		if r == 0xFFFD && size == 1 {
			// Invalid UTF-8 byte; emit replacement as \ufffd.
			dst = append(dst, '\\', 'u', 'f', 'f', 'f', 'd')
			i++
			continue
		}
		// Escape U+2028/U+2029 like Sonic: the raw runes are valid JSON
		// but break JavaScript parsers that treat them as line
		// terminators.
		if r == 0x2028 || r == 0x2029 {
			dst = append(dst, '\\', 'u', '2', '0', '2', hexDigit(byte(r&0xF)))
			i += size
			continue
		}
		dst = append(dst, s[i:i+size]...)
		i += size
	}
	return append(dst, '"')
}

func hexDigit(b byte) byte {
	if b < 10 {
		return '0' + b
	}
	return 'a' + (b - 10)
}

// decodeRune is a tiny utf8.DecodeRuneInString clone to avoid importing
// unicode/utf8 just for two call sites. Continuation bytes are validated
// (s[k]&0xC0 == 0x80); a well-formed prefix with a bad continuation byte
// must decode as a single invalid byte so following JSON delimiters
// (quotes, backslashes) are never swallowed.
func decodeRune(s string) (r rune, size int) {
	if len(s) == 0 {
		return 0, 0
	}
	c := s[0]
	if c < 0x80 {
		return rune(c), 1
	}
	if c&0xE0 == 0xC0 && len(s) >= 2 && s[1]&0xC0 == 0x80 {
		return rune(c&0x1F)<<6 | rune(s[1]&0x3F), 2
	}
	if c&0xF0 == 0xE0 && len(s) >= 3 && s[1]&0xC0 == 0x80 && s[2]&0xC0 == 0x80 {
		return rune(c&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	}
	if c&0xF8 == 0xF0 && len(s) >= 4 && s[1]&0xC0 == 0x80 && s[2]&0xC0 == 0x80 && s[3]&0xC0 == 0x80 {
		return rune(c&0x07)<<18 | rune(s[1]&0x3F)<<12 | rune(s[2]&0x3F)<<6 | rune(s[3]&0x3F), 4
	}
	return 0xFFFD, 1
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

func intFromInt64(v int64) (int, bool) {
	i := int(v)
	return i, int64(i) == v
}

// newMissing returns a pointer to a fresh non-existent node.
func newMissing() *Node {
	return &Node{typ: V_NONE, exists: false, err: ErrNotExist}
}

// newErrorNode returns a pointer to a fresh error node wrapping err.
func newErrorNode(err error) *Node {
	return &Node{typ: V_ERROR, exists: true, loaded: true, err: err}
}

func (n *Node) ensureLoaded() error {
	if n == nil {
		return ErrNotExist
	}
	if n.mu == nil {
		if n.isRawUnlocked() {
			return n.LoadAll()
		}
		return nil
	}
	n.mu.RLock()
	isRaw := n.isRawUnlocked()
	n.mu.RUnlock()
	if isRaw {
		return n.LoadAll()
	}
	return nil
}

func (n *Node) isRawUnlocked() bool {
	return !n.loaded && n.raw != ""
}

func (n *Node) lockRead() {
	if n.mu != nil {
		n.mu.RLock()
	}
}

func (n *Node) unlockRead() {
	if n.mu != nil {
		n.mu.RUnlock()
	}
}

// itoa is a small strconv.Itoa alias kept here to avoid an extra import
// in types.go for SyntaxError.Error.
func itoa(n int) string { return strconv.Itoa(n) }

// quoteString returns a JSON-quoted version of s for error messages.
func quoteString(s string) string {
	var b []byte
	b = appendStringJSON(b, s, false)
	return string(b)
}
