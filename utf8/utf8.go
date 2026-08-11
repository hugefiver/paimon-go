// Package utf8 mirrors the public surface of Sonic's v1.15.2 utf8
// package.
//
// Validate and ValidateString delegate to the standard library's
// unicode/utf8 package so behavior matches the Go runtime's UTF-8
// validity rules byte for byte. CorrectWith scans a source byte slice,
// copies valid runes unchanged, and replaces each invalid byte with the
// caller-supplied replacement string.
package utf8

import stdutf8 "unicode/utf8"

// Validate reports whether src consists entirely of valid UTF-8-encoded
// runes. A nil slice is considered valid.
func Validate(src []byte) bool {
	if src == nil {
		return true
	}
	return stdutf8.Valid(src)
}

// ValidateString reports whether src consists entirely of valid
// UTF-8-encoded runes. An empty string is considered valid.
func ValidateString(src string) bool {
	if src == "" {
		return true
	}
	return stdutf8.ValidString(src)
}

// CorrectWith appends src to dst with each invalid UTF-8 byte replaced
// by repl. Valid runes are appended unchanged. When stdutf8.DecodeRune
// returns both RuneError and a size greater than 1, the (well-formed)
// RuneError encoding is preserved as-is rather than being treated as an
// error.
func CorrectWith(dst []byte, src []byte, repl string) []byte {
	for i := 0; i < len(src); {
		r, size := stdutf8.DecodeRune(src[i:])
		if r == stdutf8.RuneError && size == 1 {
			dst = append(dst, repl...)
			i++
			continue
		}
		dst = append(dst, src[i:i+size]...)
		i += size
	}
	return dst
}
