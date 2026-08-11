package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/ast"
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
	Valid           bool   `json:"valid"`
	UnmarshalOK     bool   `json:"unmarshal_ok"`
	MarshalOK       bool   `json:"marshal_ok"`
	Normalized      string `json:"normalized,omitempty"`
	GetRootOK       bool   `json:"get_root_ok"`
	GetRootRaw      string `json:"get_root_raw,omitempty"`
	GetPathOK       bool   `json:"get_path_ok"`
	GetPathRaw      string `json:"get_path_raw,omitempty"`
	SearcherPathOK  bool   `json:"searcher_path_ok"`
	SearcherPathRaw string `json:"searcher_path_raw,omitempty"`
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

	return res
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
