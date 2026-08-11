package types

import "testing"

func TestParsingErrorMessages(t *testing.T) {
	cases := map[ParsingError]string{
		ERR_EOF:                "eof",
		ERR_INVALID_CHAR:       "invalid char",
		ERR_INVALID_ESCAPE:     "invalid escape",
		ERR_INVALID_UNICODE:    "invalid unicode",
		ERR_INTEGER_OVERFLOW:   "integer overflow",
		ERR_INVALID_NUMBER_FMT: "invalid number format",
		ERR_RECURSE_EXCEED_MAX: "recursion exceeds max depth",
		ERR_FLOAT_INFINITY:     "float infinity",
		ERR_MISMATCH:           "mismatch",
		ERR_INVALID_UTF8:       "invalid utf8",
		ERR_NOT_FOUND:          "not found",
		ERR_UNSUPPORT_TYPE:     "unsupported type",
	}
	for code, want := range cases {
		if got := code.Message(); got == "" || got == "unknown parsing error" {
			t.Fatalf("ParsingError(%d).Message() = %q, want descriptive text containing %q", code, got, want)
		}
		if got := code.Error(); got != code.Message() {
			t.Fatalf("ParsingError(%d).Error() = %q, want Message() %q", code, got, code.Message())
		}
	}
	// Code 0 must return an empty message (not "unknown parsing error").
	var zero ParsingError
	if got := zero.Message(); got != "" {
		t.Fatalf("ParsingError(0).Message() = %q, want empty string", got)
	}
	if got := zero.Error(); got != "" {
		t.Fatalf("ParsingError(0).Error() = %q, want empty string", got)
	}
}
