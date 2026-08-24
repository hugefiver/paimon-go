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
	if filepath.Base(dir) == "local" {
		cmdPath = "./cmd/soniclocal"
	}
	args := []string{"run", "-mod=readonly", cmdPath}
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

// hasRawControlInStringToken identifies the documented local-only acceptance
// of unescaped ASCII control bytes in otherwise valid JSON string tokens.
func hasRawControlInStringToken(data string) bool {
	normalized := make([]byte, 0, len(data))
	inString := false
	escaped := false
	hasRawControl := false
	const hex = "0123456789abcdef"

	for i := 0; i < len(data); i++ {
		b := data[i]
		if !inString {
			if b <= 0x1f && b != '\t' && b != '\n' && b != '\r' {
				return false
			}
			normalized = append(normalized, b)
			if b == '"' {
				inString = true
			}
			continue
		}

		if escaped {
			normalized = append(normalized, b)
			escaped = false
			continue
		}
		switch b {
		case '\\':
			normalized = append(normalized, b)
			escaped = true
		case '"':
			normalized = append(normalized, b)
			inString = false
		default:
			if b > 0x1f {
				normalized = append(normalized, b)
				continue
			}
			hasRawControl = true
			normalized = append(normalized, '\\', 'u', '0', '0', hex[b>>4], hex[b&0x0f])
		}
	}

	// Invalid JSON remains in the ordinary strict parity path. Only documents
	// made valid by escaping raw controls in string tokens are excepted.
	return hasRawControl && !inString && !escaped && json.Valid(normalized)
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
		if hasRawControlInStringToken(data) {
			return
		}
		if !reflect.DeepEqual(local, upstream) {
			t.Fatalf("sonic parity mismatch\ndata: %q\npath: %+v\nlocal: %+v\nupstream: %+v", data, req.Path, local, upstream)
		}
	})
}

func TestHasRawControlInStringToken(t *testing.T) {
	for _, tt := range []struct {
		name string
		data string
		want bool
	}{
		{
			name: "control in object key",
			data: string([]byte{'{', '"', 0x11, 'x', '"', ':', '1', '}'}),
			want: true,
		},
		{
			name: "control in string value",
			data: string([]byte{'{', '"', 'x', '"', ':', '"', 0x00, '"', '}'}),
			want: true,
		},
		{
			name: "legal whitespace outside with string control",
			data: string([]byte{' ', '\t', '\n', '\r', '{', '"', 'x', '"', ':', '"', 0x11, '"', '}', '\t', '\n', '\r'}),
			want: true,
		},
		{
			name: "control outside string",
			data: string([]byte{'{', 0x11, '"', 'x', '"', ':', '1', '}'}),
		},
		{
			name: "escaped control",
			data: `{"\u0011x":1}`,
		},
		{
			name: "leading zero",
			data: string([]byte{'{', '"', 0x11, 'x', '"', ':', '0', '1', '}'}),
		},
		{
			name: "invalid escape",
			data: string([]byte{'{', '"', 0x11, 'x', '"', ':', '"', '\\', 'q', '"', '}'}),
		},
		{
			name: "ordinary valid JSON",
			data: `{"x":[true,null,1]}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasRawControlInStringToken(tt.data); got != tt.want {
				t.Fatalf("hasRawControlInStringToken(%q) = %t, want %t", tt.data, got, tt.want)
			}
		})
	}

	for b := byte(0); b <= 0x1f; b++ {
		if b == '\t' || b == '\n' || b == '\r' {
			continue
		}
		t.Run("outside_control_"+string([]byte{b}), func(t *testing.T) {
			data := string([]byte{'{', b, '"', 'x', '"', ':', '"', 0x11, '"', '}'})
			if got := hasRawControlInStringToken(data); got {
				t.Fatalf("hasRawControlInStringToken(%q) = true, want false", data)
			}
		})
	}
}

func TestFinalReviewSonicParity(t *testing.T) {
	for _, tt := range []struct {
		name      string
		data      string
		path      []pathPart
		rootValue string
		pathValue string
	}{
		{name: "root bare exponent followed by space", data: "1e ", rootValue: "1e", pathValue: "1e"},
		{name: "root bare exponent followed by comma", data: "1e,", rootValue: "1e", pathValue: "1e"},
		{name: "matched b before malformed sibling", data: `{"b":2,"a":{`, path: []pathPart{{Kind: "key", Key: "b"}}, pathValue: "2"},
		{name: "matched a before malformed sibling", data: `{"a":1,"b":}`, path: []pathPart{{Kind: "key", Key: "a"}}, pathValue: "1"},
		{name: "selected malformed value remains rejected", data: `{"a":{`, path: []pathPart{{Kind: "key", Key: "a"}}},
		{name: "malformed member before target remains rejected", data: `{"broken":{,"a":1}`, path: []pathPart{{Kind: "key", Key: "a"}}},
		{name: "object skips balanced malformed container before target", data: `{"broken":{garbage},"a":1}`, path: []pathPart{{Kind: "key", Key: "a"}}, pathValue: "1"},
		{name: "array skips balanced malformed container before target", data: `[{garbage},1]`, path: []pathPart{{Kind: "index", Index: 1}}, pathValue: "1"},
		{name: "selected malformed container remains rejected", data: `{"a":{garbage}}`, path: []pathPart{{Kind: "key", Key: "a"}}},
		{name: "prior malformed scalar remains rejected", data: `{"broken":garbage,"a":1}`, path: []pathPart{{Kind: "key", Key: "a"}}},
		{name: "prior unclosed container remains rejected", data: `{"broken":{garbage,"a":1}`, path: []pathPart{{Kind: "key", Key: "a"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := request{
				Data: base64.StdEncoding.EncodeToString([]byte(tt.data)),
				Path: tt.path,
			}
			local := runHelper(t, filepath.Join("local"), req)
			upstream := runHelper(t, filepath.Join("upstream"), req)
			if !reflect.DeepEqual(local, upstream) {
				t.Fatalf("final review sonic parity mismatch\ndata: %q\npath: %+v\nlocal: %+v\nupstream: %+v", tt.data, req.Path, local, upstream)
			}

			if tt.rootValue != "" {
				if !upstream.GetRootOK || upstream.GetRootRaw != tt.rootValue {
					t.Fatalf("upstream sonic.Get(%q) = ok:%t raw:%q, want ok:true raw:%q", tt.data, upstream.GetRootOK, upstream.GetRootRaw, tt.rootValue)
				}
				if !upstream.GetPathOK || upstream.GetPathRaw != tt.pathValue {
					t.Fatalf("upstream sonic.Get(%q, root) = ok:%t raw:%q, want ok:true raw:%q", tt.data, upstream.GetPathOK, upstream.GetPathRaw, tt.pathValue)
				}
				if !upstream.SearcherPathOK || upstream.SearcherPathRaw != tt.pathValue || upstream.NewRawType != 33 {
					t.Fatalf("upstream root AST result = %+v, want Searcher raw %q and NewRawType 33", upstream, tt.pathValue)
				}
				return
			}

			if tt.pathValue != "" {
				if !upstream.SearcherPathOK || upstream.SearcherPathRaw != tt.pathValue {
					t.Fatalf("upstream Searcher(%q, %+v) = ok:%t raw:%q, want ok:true raw:%q", tt.data, tt.path, upstream.SearcherPathOK, upstream.SearcherPathRaw, tt.pathValue)
				}
				return
			}
			if upstream.SearcherPathOK {
				t.Fatalf("upstream Searcher(%q, %+v) unexpectedly succeeded with raw %q", tt.data, tt.path, upstream.SearcherPathRaw)
			}
		})
	}
}

func TestSonicCompatibilityProbeParity(t *testing.T) {
	for _, data := range []string{
		`1`,
		`1.5`,
		`{"a":1}x`,
		`tru`,
		`1e`,
		`{"a":1]`,
		`"\q"`,
	} {
		req := request{Data: base64.StdEncoding.EncodeToString([]byte(data))}
		local := runHelper(t, filepath.Join("local"), req)
		upstream := runHelper(t, filepath.Join("upstream"), req)
		if !reflect.DeepEqual(local, upstream) {
			t.Fatalf("sonic compatibility probe mismatch\ndata: %q\nlocal: %+v\nupstream: %+v", data, local, upstream)
		}

		if data == "1" && (local.PreorderOnlyNumber == "" || !local.GetRootOK || local.GetRootRaw != "1") {
			t.Fatalf("sonic compatibility probe protocol unavailable\ndata: %q\nresult: %+v", data, local)
		}
	}
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
