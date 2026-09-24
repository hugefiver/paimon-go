package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/encoder"
)

const maxRequestBytes = 1 << 20

var errRequestTooLarge = errors.New("helper request exceeds 1 MiB")

type pathPart struct {
	Kind  string `json:"kind"`
	Key   string `json:"key,omitempty"`
	Index int    `json:"index,omitempty"`
}

type request struct {
	Data string     `json:"data"`
	Path []pathPart `json:"path"`
}

type result struct {
	Valid              bool   `json:"valid"`
	EncoderValid       bool   `json:"encoder_valid"`
	EncoderValidStart  int    `json:"encoder_valid_start"`
	UnmarshalOK        bool   `json:"unmarshal_ok"`
	MarshalOK          bool   `json:"marshal_ok"`
	Normalized         string `json:"normalized,omitempty"`
	GetRootOK          bool   `json:"get_root_ok"`
	GetRootRaw         string `json:"get_root_raw,omitempty"`
	GetPathOK          bool   `json:"get_path_ok"`
	GetPathRaw         string `json:"get_path_raw,omitempty"`
	SearcherPathOK     bool   `json:"searcher_path_ok"`
	SearcherPathRaw    string `json:"searcher_path_raw,omitempty"`
	PreorderOnlyNumber string `json:"preorder_only_number,omitempty"`
	NewRawType         int    `json:"new_raw_type,omitempty"`
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--info" {
		_ = json.NewEncoder(os.Stdout).Encode(struct {
			GoVersion string `json:"go_version"`
			APIKind   int    `json:"api_kind"`
		}{runtime.Version(), sonic.APIKind})
		return
	}
	res := run()
	if err := json.NewEncoder(os.Stdout).Encode(res); err != nil {
		fmt.Fprintf(os.Stderr, "encode result: %v\n", err)
		os.Exit(1)
	}
}

func run() result {
	req, err := readRequest(os.Stdin)
	if err != nil {
		return result{}
	}
	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		return result{}
	}

	ok, start := encoder.Valid(data)
	res := result{Valid: sonic.Valid(data), EncoderValid: ok, EncoderValidStart: start}

	var v interface{}
	if err := sonic.Unmarshal(data, &v); err == nil {
		res.UnmarshalOK = true
		if normalized, err := sonic.Marshal(v); err == nil {
			res.MarshalOK = true
			res.Normalized = canonicalJSON(normalized)
		}
	}

	if node, err := sonic.Get(data); err == nil {
		if raw, err := node.Raw(); err == nil {
			res.GetRootOK = true
			res.GetRootRaw = raw
		}
	}

	path := convertPath(req.Path)
	if node, err := sonic.Get(data, path...); err == nil {
		if raw, err := node.Raw(); err == nil {
			res.GetPathOK = true
			res.GetPathRaw = raw
		}
	}
	if node, err := ast.NewSearcher(string(data)).GetByPath(path...); err == nil {
		if raw, err := node.Raw(); err == nil {
			res.SearcherPathOK = true
			res.SearcherPathRaw = raw
		}
	}
	res.PreorderOnlyNumber = recordPreorderOnlyNumber(string(data))
	res.NewRawType = int(ast.NewRaw(string(data)).Type())

	return res
}

// Compare JSON values independently of permitted map ordering and escaping
// differences. Invalid marshal output stays invalid and is rejected by the driver.
func canonicalJSON(data []byte) string {
	if !json.Valid(data) {
		return "invalid marshaled JSON: " + string(data)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return "invalid marshaled JSON: " + string(data)
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return "invalid marshaled JSON: " + string(data)
	}
	return string(bytes.TrimSuffix(out.Bytes(), []byte{'\n'}))
}

func readRequest(r io.Reader) (request, error) {
	input, err := io.ReadAll(io.LimitReader(r, maxRequestBytes+1))
	if err != nil {
		return request{}, err
	}
	if len(input) > maxRequestBytes {
		return request{}, errRequestTooLarge
	}
	var req request
	if err := json.Unmarshal(input, &req); err != nil {
		return request{}, err
	}
	return req, nil
}

type recordingVisitor struct {
	events []string
}

func (v *recordingVisitor) OnNull() error            { return nil }
func (v *recordingVisitor) OnBool(bool) error        { return nil }
func (v *recordingVisitor) OnString(string) error    { return nil }
func (v *recordingVisitor) OnObjectBegin(int) error  { return nil }
func (v *recordingVisitor) OnObjectKey(string) error { return nil }
func (v *recordingVisitor) OnObjectEnd() error       { return nil }
func (v *recordingVisitor) OnArrayBegin(int) error   { return nil }
func (v *recordingVisitor) OnArrayEnd() error        { return nil }
func (v *recordingVisitor) OnInt64(value int64, raw json.Number) error {
	v.events = append(v.events, "int64:"+strconv.FormatInt(value, 10)+":"+string(raw))
	return nil
}
func (v *recordingVisitor) OnFloat64(value float64, raw json.Number) error {
	v.events = append(v.events, "float64:"+strconv.FormatFloat(value, 'g', -1, 64)+":"+string(raw))
	return nil
}

func recordPreorderOnlyNumber(data string) string {
	visitor := recordingVisitor{}
	err := ast.Preorder(data, &visitor, &ast.VisitorOptions{OnlyNumber: true})
	if err != nil {
		return `["error"]`
	}
	encoded, err := json.Marshal(visitor.events)
	if err != nil {
		return "[\"error\"]"
	}
	return string(encoded)
}

func convertPath(parts []pathPart) []interface{} {
	path := make([]interface{}, 0, len(parts))
	for _, part := range parts {
		switch part.Kind {
		case "key":
			path = append(path, part.Key)
		case "index":
			path = append(path, part.Index)
		}
	}
	return path
}
