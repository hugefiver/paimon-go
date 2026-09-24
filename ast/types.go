// Package ast mirrors the public surface of Sonic's v1.15.2 ast package.
//
// The mutable JSON tree, parser, searcher, visitor, and iterator types
// provided here are source-compatible with github.com/bytedance/sonic/ast
// for the Sonic 1.15.* line. Raw containers expose stable child slots lazily;
// parsing and validation do not borrow memory from reusable parser arenas.
package ast

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/bytedance/sonic/internal/errorcontext"
	nativetypes "github.com/bytedance/sonic/internal/native/types"
)

// Node type tags. These mirror the Sonic v1.15.2 V_* numeric tags used
// by Node.Type() and TypeSafe().
const (
	V_NONE   = 0
	V_ERROR  = 1
	V_NULL   = 2
	V_TRUE   = 3
	V_FALSE  = 4
	V_ARRAY  = 5
	V_OBJECT = 6
	V_STRING = 7
	V_NUMBER = 33
	V_ANY    = 34
)

// ErrNotExist is returned by accessor methods that target a value which
// does not exist in the parsed JSON.
var ErrNotExist = errors.New("value not exists")

// ErrUnsupportType is returned when a conversion or constructor receives
// a Go value that cannot be mapped to a JSON node.
var ErrUnsupportType = errors.New("unsupported type")

// VisitOPSkip is returned from a Visitor callback (for example
// OnObjectBegin / OnArrayBegin) to signal that the visitor wants the
// walker to skip the children of the current container without treating
// it as a fatal error.
var VisitOPSkip = errors.New("skip children")

// Node is a mutable JSON value. The zero value is an absent node.
//
// All exported accessors are safe to call on a zero-value Node. Unsupported
// conversions return ErrUnsupportType; Raw and MarshalJSON return ErrNotExist.
type Node struct {
	typ    int
	exists bool
	// loaded reports whether raw JSON (if any) has been parsed and the
	// scalar/container fields populated. A NewRaw node has loaded=false
	// until Load / LoadAll is called.
	loaded bool
	boolv  bool
	// mu is enabled by concurrent-read constructors or an explicit Load.
	// It remains stable during reads and protects lazy materialization.
	mu  *sync.RWMutex
	raw string
	str string
	num json.Number
	arr []*Node
	obj []*Pair
	any interface{}
	err error
	// lazyPos is owned by each Node copy; raw holds the immutable source.
	lazyPos int
}

// Pair is an object key/value pair. Object nodes store their entries as
// a pointer slice so insertion order and child addresses survive mutations.
type Pair struct {
	Key   string
	Value Node
}

// Sequence represents one step of a path used by ForEach callbacks.
// For array elements Index is set and Key is nil. For object entries
// Key points at the entry key and Index is the entry position.
type Sequence struct {
	Index int
	Key   *string
}

// String returns the Sonic-compatible textual representation of a path step.
func (s Sequence) String() string {
	key := ""
	if s.Key != nil {
		key = *s.Key
	}
	return fmt.Sprintf("Sequence(%d, %q)", s.Index, key)
}

// Scanner is the callback signature used by Node.ForEach.
//
// The callback returns true to continue iteration and false to stop.
type Scanner func(path Sequence, node *Node) bool

// SearchOptions tunes Searcher behavior. The fields match the Sonic
// v1.15.2 public shape.
type SearchOptions struct {
	ValidateJSON   bool
	CopyReturn     bool
	ConcurrentRead bool
}

// Searcher parses and walks raw JSON on demand for GetByPath lookups.
type Searcher struct {
	SearchOptions
	src string
}

// Parser parses a single JSON document into a Node tree.
type Parser struct {
	src string
	pos int
}

// SyntaxError describes a JSON parse error at a concrete source position.
// It implements the error interface.
type SyntaxError struct {
	Pos  int
	Src  string
	Code nativetypes.ParsingError
	Msg  string
}

// Error returns a human-readable description of the syntax error.
func (e SyntaxError) Error() string {
	return fmt.Sprintf("%q", e.Description())
}

func (e SyntaxError) Description() string {
	return "Syntax error " + e.description()
}

func (e SyntaxError) Message() string {
	if e.Msg != "" {
		return e.Msg
	}
	return e.Code.Message()
}

func (e SyntaxError) description() string {
	if e.Src == "" {
		return fmt.Sprintf("no sources available, the input json is empty: %#v", e)
	}
	return errorcontext.ASTSourceDescription(e.Src, e.Pos, e.Message())
}

// VisitorOptions tunes Preorder traversal behavior.
type VisitorOptions struct {
	OnlyNumber bool
}

// Visitor is the callback interface used by Preorder to walk a JSON
// document. Each scalar value produces exactly one typed callback;
// containers produce begin/end callbacks around their children.
type Visitor interface {
	OnNull() error
	OnBool(bool) error
	OnString(string) error
	OnInt64(int64, json.Number) error
	OnFloat64(float64, json.Number) error
	OnObjectBegin(capacity int) error
	OnObjectKey(key string) error
	OnObjectEnd() error
	OnArrayBegin(capacity int) error
	OnArrayEnd() error
}
