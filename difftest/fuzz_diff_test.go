package difftest

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
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

func pathFromSeed(seed string) []pathPart {
	candidates := []pathPart{
		{Kind: "key", Key: "x"},
		{Kind: "key", Key: "users"},
		{Kind: "key", Key: "a"},
		{Kind: "index", Index: 0},
		{Kind: "index", Index: 1},
		{Kind: "key", Key: "seed"},
	}
	if len(seed) == 0 {
		return nil
	}
	pathLen := int(seed[0] % 4)
	path := make([]pathPart, 0, pathLen)
	for i := 0; i < pathLen; i++ {
		b := seed[(i+1)%len(seed)]
		path = append(path, candidates[int(b)%len(candidates)])
	}
	return path
}

func runHelper(t *testing.T, dir string, req request) result {
	t.Helper()

	reqBytes, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	cmdPath := "./cmd/sonicupstream"
	args := []string{"run", cmdPath}
	if filepath.Base(dir) == "local" {
		cmdPath = "./cmd/soniclocal"
		args = []string{"run", cmdPath}
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(reqBytes)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("run helper %s: %v\nstderr:\n%s", dir, err, stderr.String())
	}

	var res result
	if err := json.Unmarshal(stdout, &res); err != nil {
		t.Fatalf("unmarshal helper %s stdout: %v\nstdout:\n%s\nstderr:\n%s", dir, err, string(stdout), stderr.String())
	}
	return res
}

func FuzzUpstreamSonicParity(f *testing.F) {
	for _, seed := range []string{
		"null",
		`{"x":1}`,
		`{"users":[{"id":1},{"id":2}]}`,
		`[1,true,"x"]`,
		`{"bad":`,
		`{"a":01}`,
		`{"a":1.}`,
		`{"a":1e}`,
		`{"a":+1}`,
		`7xxx`,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data string) {
		req := request{
			Data: base64.StdEncoding.EncodeToString([]byte(data)),
			Path: pathFromSeed(data),
		}

		local := runHelper(t, filepath.Join("local"), req)
		upstream := runHelper(t, filepath.Join("upstream"), req)
		if !reflect.DeepEqual(local, upstream) {
			t.Fatalf("sonic parity mismatch\ndata: %q\npath: %+v\nlocal: %+v\nupstream: %+v", data, req.Path, local, upstream)
		}
	})
}

func TestMalformedNumberParity(t *testing.T) {
	for _, data := range []string{
		`{"a":01}`,
		`{"a":1.}`,
		`{"a":1e}`,
		`{"a":+1}`,
		string([]byte{'{', '"', 'a', '"', ':', '"', 0x11, 'a', '"', ',', '"', 'b', '"', ':', '0', '1', '}'}),
		string([]byte{'{', '"', 'a', '"', ':', '"', 0x11, 'a', '"', ',', '"', 'b', '"', ':', '1', 'e', '}'}),
	} {
		req := request{
			Data: base64.StdEncoding.EncodeToString([]byte(data)),
			Path: []pathPart{{Kind: "key", Key: "a"}},
		}
		local := runHelper(t, filepath.Join("local"), req)
		upstream := runHelper(t, filepath.Join("upstream"), req)
		if !reflect.DeepEqual(local, upstream) {
			t.Fatalf("sonic parity mismatch\ndata: %q\npath: %+v\nlocal: %+v\nupstream: %+v", data, req.Path, local, upstream)
		}
	}
}

func TestMalformedNumberContainerPathParity(t *testing.T) {
	for _, data := range []string{
		`{"a":{"b":1e}}`,
		`{"a":{"b":01}}`,
		`{"x":"\n","a":{"b":1e}}`,
		`{"x":"\n","a":{"b":01}}`,
		`{"\u0061":{"b":1e}}`,
		`{"\u0061":{"b":01}}`,
		`{"é":1,"a":{"b":1e}}`,
		`{"é":1,"a":{"b":01}}`,
	} {
		req := request{
			Data: base64.StdEncoding.EncodeToString([]byte(data)),
			Path: []pathPart{{Kind: "key", Key: "a"}},
		}
		local := runHelper(t, filepath.Join("local"), req)
		upstream := runHelper(t, filepath.Join("upstream"), req)
		if !reflect.DeepEqual(local, upstream) {
			t.Fatalf("sonic parity mismatch\ndata: %q\npath: %+v\nlocal: %+v\nupstream: %+v", data, req.Path, local, upstream)
		}
	}
}

func TestEscapedObjectKeyParity(t *testing.T) {
	for _, tt := range []struct {
		data string
		key  string
	}{
		{data: `{"\u0061":1}`, key: "a"},
		{data: `{"a\/b":1}`, key: "a/b"},
		{data: `{"a\"b":1}`, key: `a"b`},
	} {
		req := request{
			Data: base64.StdEncoding.EncodeToString([]byte(tt.data)),
			Path: []pathPart{{Kind: "key", Key: tt.key}},
		}
		local := runHelper(t, filepath.Join("local"), req)
		upstream := runHelper(t, filepath.Join("upstream"), req)
		if !reflect.DeepEqual(local, upstream) {
			t.Fatalf("sonic parity mismatch\ndata: %q\npath: %+v\nlocal: %+v\nupstream: %+v", tt.data, req.Path, local, upstream)
		}
	}
}

func TestInvalidEscapeRawParity(t *testing.T) {
	for _, tt := range []struct {
		data string
		path []pathPart
	}{
		{data: `{"a":"\q","b":1}`, path: []pathPart{{Kind: "key", Key: "a"}}},
		{data: `{"a":"\uZZZZ","b":1}`, path: []pathPart{{Kind: "key", Key: "a"}}},
		{data: `{"a":{"b":"\q"}}`, path: []pathPart{{Kind: "key", Key: "a"}}},
		{data: `{"a":{"b":"\uZZZZ"}}`, path: []pathPart{{Kind: "key", Key: "a"}}},
	} {
		req := request{Data: base64.StdEncoding.EncodeToString([]byte(tt.data)), Path: tt.path}
		local := runHelper(t, filepath.Join("local"), req)
		upstream := runHelper(t, filepath.Join("upstream"), req)
		if !reflect.DeepEqual(local, upstream) {
			t.Fatalf("sonic parity mismatch\ndata: %q\npath: %+v\nlocal: %+v\nupstream: %+v", tt.data, req.Path, local, upstream)
		}
	}
}
