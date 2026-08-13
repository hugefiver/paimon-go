package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/decoder"
)

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
	SkipStart          int    `json:"skip_start"`
	SkipEnd            int    `json:"skip_end"`
	ConfigBothPanics   bool   `json:"config_both_panics"`
}

func main() {
	res := run()
	if err := json.NewEncoder(os.Stdout).Encode(res); err != nil {
		fmt.Fprintf(os.Stderr, "encode result: %v\n", err)
		os.Exit(1)
	}
}

func run() result {
	var req request
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
		os.Exit(1)
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return result{}
	}
	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		return result{}
	}

	res := result{Valid: sonic.Valid(data)}

	var v interface{}
	if err := sonic.Unmarshal(data, &v); err == nil {
		res.UnmarshalOK = true
		if normalized, err := sonic.Marshal(v); err == nil {
			res.MarshalOK = true
			res.Normalized = string(normalized)
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
	res.SkipStart, res.SkipEnd = decoder.Skip(data)
	res.ConfigBothPanics = panics(func() {
		var value interface{}
		_ = sonic.Config{UseNumber: true, UseInt64: true}.Froze().Unmarshal(data, &value)
	})

	return res
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
	if err != nil || hasRawControlInString(data) {
		return `["error"]`
	}
	encoded, err := json.Marshal(visitor.events)
	if err != nil {
		return "[\"error\"]"
	}
	return string(encoded)
}

func hasRawControlInString(data string) bool {
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		if !inString {
			if data[i] == '"' {
				inString = true
			}
			continue
		}
		if escaped {
			escaped = false
			continue
		}
		switch data[i] {
		case '\\':
			escaped = true
		case '"':
			inString = false
		default:
			if data[i] < 0x20 {
				return true
			}
		}
	}
	return false
}

func panics(fn func()) (panicked bool) {
	defer func() {
		panicked = recover() != nil
	}()
	fn()
	return false
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
