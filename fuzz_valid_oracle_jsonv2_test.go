//go:build sonic_jsonv2 && !sonic_stdjson && goexperiment.jsonv2

package sonic_test

import "encoding/json"

func validParityOracle(data []byte) (bool, string) {
	return len(data) > 0 && json.Valid(data), "jsonv2"
}
