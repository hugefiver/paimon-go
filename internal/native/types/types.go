// Package types mirrors the minimal subset of Sonic's
// internal/native/types package needed by the public compatibility surface.
//
// ParsingError codes and their message strings are kept stable so that
// callers depending on Sonic's v1.15.2 error wording keep compiling and
// behaving the same against this replacement module. Only the codes that
// appear in exported signatures are reproduced here; the full Sonic JIT
// parser machinery is intentionally not implemented.
package types

// ParsingError is a numeric parsing error code used by the AST/parser
// layers. The zero value denotes "no error".
type ParsingError uint

const (
	ERR_EOF ParsingError = iota + 1
	ERR_INVALID_CHAR
	ERR_INVALID_ESCAPE
	ERR_INVALID_UNICODE
	ERR_INTEGER_OVERFLOW
	ERR_INVALID_NUMBER_FMT
	ERR_RECURSE_EXCEED_MAX
	ERR_FLOAT_INFINITY
	ERR_MISMATCH
	ERR_INVALID_UTF8
)

// The following codes are intentionally assigned values that match the
// upstream Sonic numeric codes so existing switch statements over
// ParsingError keep working.
const (
	ERR_NOT_FOUND      ParsingError = 33
	ERR_UNSUPPORT_TYPE ParsingError = 34
)

// Error implements the error interface. It returns the same text as
// Message so callers can use ParsingError values interchangeably as
// errors. The zero value returns an empty string, matching Sonic's
// "no error" sentinel behavior.
func (e ParsingError) Error() string { return e.Message() }

// Message returns a stable, human-readable description of the error
// code. The zero value returns "" to indicate no error. Unknown codes
// return "unknown parsing error".
func (e ParsingError) Message() string {
	switch e {
	case 0:
		return ""
	case ERR_EOF:
		return "eof"
	case ERR_INVALID_CHAR:
		return "invalid char"
	case ERR_INVALID_ESCAPE:
		return "invalid escape"
	case ERR_INVALID_UNICODE:
		return "invalid unicode"
	case ERR_INTEGER_OVERFLOW:
		return "integer overflow"
	case ERR_INVALID_NUMBER_FMT:
		return "invalid number format"
	case ERR_RECURSE_EXCEED_MAX:
		return "recursion exceeds max depth"
	case ERR_FLOAT_INFINITY:
		return "float infinity"
	case ERR_MISMATCH:
		return "mismatch"
	case ERR_INVALID_UTF8:
		return "invalid utf8"
	case ERR_NOT_FOUND:
		return "not found"
	case ERR_UNSUPPORT_TYPE:
		return "unsupported type"
	default:
		return "unknown parsing error"
	}
}

// ValueType identifies the JSON value category produced by a parser.
// It mirrors Sonic v1.15.2: ValueType is an int64 alias and the V_*
// constants start at 1 (V_EOF) through V_INTEGER = 9.
type ValueType = int64

const (
	V_EOF     ValueType = 1
	V_NULL    ValueType = 2
	V_TRUE    ValueType = 3
	V_FALSE   ValueType = 4
	V_ARRAY   ValueType = 5
	V_OBJECT  ValueType = 6
	V_STRING  ValueType = 7
	V_DOUBLE  ValueType = 8
	V_INTEGER ValueType = 9
)
