package types

import "testing"

func TestParsingErrorMessages(t *testing.T) {
	for code, want := range map[ParsingError]string{
		0: "ok", ERR_EOF: "eof", ERR_INVALID_CHAR: "invalid char", ERR_INVALID_ESCAPE: "invalid escape char", ERR_INVALID_UNICODE: "invalid unicode escape", ERR_INTEGER_OVERFLOW: "integer overflow", ERR_INVALID_NUMBER_FMT: "invalid number format", ERR_RECURSE_EXCEED_MAX: "recursion exceeded max depth", ERR_FLOAT_INFINITY: "float number is infinity", ERR_MISMATCH: "mismatched type with value", ERR_INVALID_UTF8: "invalid UTF8", ERR_NOT_FOUND: "unknown error 33", ERR_UNSUPPORT_TYPE: "unknown error 34",
	} {
		if got := code.Message(); got != want {
			t.Fatalf("%d Message=%q want %q", code, got, want)
		}
		if got := code.Error(); got != "json: error when parsing input: "+want {
			t.Fatalf("%d Error=%q", code, got)
		}
	}
}
