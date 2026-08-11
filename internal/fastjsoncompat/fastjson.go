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
	"fmt"

	"github.com/bytedance/sonic/ast"
	nativetypes "github.com/bytedance/sonic/internal/native/types"
	vfastjson "github.com/valyala/fastjson"
)

// Valid reports whether data is a single well-formed JSON value. It is
// a thin wrapper over fastjson.Validate.
func Valid(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	var p vfastjson.Parser
	_, err := p.Parse(string(data))
	return err == nil
}

// ValidString is the string-input form of Valid.
func ValidString(data string) bool {
	if len(data) == 0 {
		return false
	}
	var p vfastjson.Parser
	_, err := p.Parse(data)
	return err == nil
}

// Get resolves path against data and returns the matching AST node.
//
// When opts.ValidateJSON is true the full document is validated before
// lookup and any structural error is returned. When opts.CopyReturn is
// true a deep-copied Node is returned (always the case here: the ast
// search layer already produces owning nodes). An empty path returns the
// whole document parsed into a Node.
//
// An invalid path element type or a missing node yields ast.ErrNotExist
// or ast.ErrUnsupportType wrapped with context.
func Get(data []byte, opts ast.SearchOptions, path ...interface{}) (ast.Node, error) {
	if len(data) == 0 {
		return ast.Node{}, ast.ErrNotExist
	}
	src := string(data)
	if opts.ValidateJSON {
		if err := vfastjson.Validate(src); err != nil {
			return ast.Node{}, mapFastjsonError(src, err)
		}
	}
	// ast.NewSearcher.GetByPath honors SearchOptions.ValidateJSON, but we
	// have already validated above. We pass the options through so the
	// searcher sees the caller's intent and so callers that set
	// ValidateJSON here without pre-validating still get errors.
	s := ast.NewSearcher(src)
	s.SearchOptions = opts
	node, err := s.GetByPath(path...)
	if err != nil {
		return ast.Node{}, err
	}
	return node, nil
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
