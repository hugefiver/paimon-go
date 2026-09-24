package jsonconv

import (
	"strconv"
	"strings"
)

// ParseNumber implements Sonic's UseInt64 mode for a validated JSON number.
// Integer literals that fit in int64 keep their integer type. Fractions,
// exponents, and larger integers use float64; floating-point overflow is an
// error, just as it is when decoding into a float64 destination.
func ParseNumber(s string) (any, error) {
	if !strings.ContainsAny(s, ".eE") {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n, nil
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// ParseNumberBytes avoids allocating a temporary string for the common integer
// case. The input must already be a valid JSON number, as for ParseNumber.
func ParseNumberBytes(raw []byte) (any, error) {
	i := 0
	negative := raw[0] == '-'
	limit := uint64(1<<63 - 1)
	if negative {
		i++
		limit++
	}
	var value uint64
	for ; i < len(raw); i++ {
		c := raw[i]
		if c < '0' || c > '9' {
			return ParseNumber(string(raw))
		}
		digit := uint64(c - '0')
		if value > limit/10 || value == limit/10 && digit > limit%10 {
			return ParseNumber(string(raw))
		}
		value = value*10 + digit
	}
	if negative {
		if value == 1<<63 {
			return int64(-1 << 63), nil
		}
		return -int64(value), nil
	}
	return int64(value), nil
}
