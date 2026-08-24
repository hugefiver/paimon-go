//go:build !sonic_jsonv2

package sonic_test

import (
	"encoding/json"

	"github.com/bytedance/sonic/internal/compatmode"
	"github.com/bytedance/sonic/internal/fastjsoncompat"
)

func validParityOracle(data []byte) (bool, string) {
	if compatmode.StdJSON {
		return len(data) > 0 && json.Valid(data), "stdjson"
	}

	return fastjsoncompat.Valid(data), "sonic-compatible"
}
