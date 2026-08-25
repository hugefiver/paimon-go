package ast

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"unsafe"
)

func TestSearcherSelectedValueValidation(t *testing.T) {
	for _, tt := range []struct {
		name string
		src  string
		path []interface{}
		want string
	}{
		{name: "balanced container before target", src: `{"before":{garbage},"selected":1}`, path: []interface{}{"selected"}, want: "1"},
		{name: "balanced container after target", src: `{"selected":1,"after":{garbage}}`, path: []interface{}{"selected"}, want: "1"},
		{name: "root ignores trailing", src: `{"selected":1} trailing`, want: `{"selected":1}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			node, err := NewSearcher(tt.src).GetByPath(tt.path...)
			if err != nil {
				t.Fatalf("GetByPath(%q, %v) error = %v", tt.src, tt.path, err)
			}
			assertSearcherRaw(t, node, tt.want)
		})
	}

	for _, tt := range []struct {
		name string
		src  string
		path []interface{}
		want string
	}{
		{name: "selected", src: `{"selected":{garbage}}`, path: []interface{}{"selected"}, want: `{garbage}`},
		{name: "root", src: `{garbage}`, want: `{garbage}`},
		{name: "root nested", src: `{"a":{garbage}}`, want: `{"a":{garbage}}`},
	} {
		t.Run("malformed_"+tt.name, func(t *testing.T) {
			if _, err := NewSearcher(tt.src).GetByPath(tt.path...); err == nil {
				t.Fatal("ValidateJSON=true accepted malformed selected value")
			}

			withoutValidation := NewSearcher(tt.src)
			withoutValidation.ValidateJSON = false
			node, err := withoutValidation.GetByPath(tt.path...)
			if err != nil {
				t.Fatalf("ValidateJSON=false GetByPath error = %v", err)
			}
			assertSearcherRaw(t, node, tt.want)
		})
	}

	for _, validate := range []bool{true, false} {
		t.Run("reject_bad_number_validate_"+strconv.FormatBool(validate), func(t *testing.T) {
			searcher := NewSearcher(`{"selected":1.}`)
			searcher.ValidateJSON = validate
			if _, err := searcher.GetByPath("selected"); err == nil {
				t.Fatal("GetByPath malformed number error = nil, want error")
			}
		})
	}

	for _, tt := range []struct {
		name string
		src  string
		path []interface{}
		want string
	}{
		{name: "root string", src: `"\q"`, want: `"\q"`},
		{name: "root object", src: `{"a":"\q","b":1}`, want: `{"a":"\q","b":1}`},
		{name: "path string", src: `{"a":"\q","b":1}`, path: []interface{}{"a"}, want: `"\q"`},
	} {
		for _, validate := range []bool{true, false} {
			t.Run("invalid_escape_"+tt.name+"_validate_"+strconv.FormatBool(validate), func(t *testing.T) {
				searcher := NewSearcher(tt.src)
				searcher.ValidateJSON = validate
				node, err := searcher.GetByPath(tt.path...)
				if err != nil {
					t.Fatalf("GetByPath(%q, %v) error = %v", tt.src, tt.path, err)
				}
				assertSearcherRaw(t, node, tt.want)
			})
		}
	}

	if _, err := NewSearcher(`{"a":"\q","broken":garbage}`).GetByPath(); err == nil {
		t.Fatal("ValidateJSON=true accepted malformed container after invalid escape")
	}
}

func TestSearcherCopyReturnOwnsExactlySelectedRaw(t *testing.T) {
	newMutableSource := func() ([]byte, string) {
		backing := []byte(`{"selected":"old"}`)
		return backing, unsafe.String(unsafe.SliceData(backing), len(backing))
	}

	backing, src := newMutableSource()
	aliased, err := NewSearcher(src).GetByPath("selected")
	if err != nil {
		t.Fatalf("CopyReturn=false GetByPath error = %v", err)
	}
	idx := bytes.Index(backing, []byte("old"))
	if idx < 0 {
		t.Fatal("mutable source does not contain selected value")
	}
	backing[idx] = 'n'
	assertSearcherRaw(t, aliased, `"nld"`)

	backing, src = newMutableSource()
	copiedSearcher := NewSearcher(src)
	copiedSearcher.CopyReturn = true
	copied, err := copiedSearcher.GetByPath("selected")
	if err != nil {
		t.Fatalf("CopyReturn=true GetByPath error = %v", err)
	}
	idx = bytes.Index(backing, []byte("old"))
	backing[idx] = 'n'
	assertSearcherRaw(t, copied, `"old"`)
}

func TestSearcherExtendedPathElementsRemainSafe(t *testing.T) {
	s := NewSearcher(`{"array":["zero","one"],"key":"value"}`)

	node, err := s.GetByPath("array", int64(1))
	if err != nil {
		t.Fatalf("int64 path error = %v", err)
	}
	assertSearcherRaw(t, node, `"one"`)

	node, err = s.GetByPath("array", json.Number("1"))
	if err != nil {
		t.Fatalf("numeric json.Number path error = %v", err)
	}
	assertSearcherRaw(t, node, `"one"`)

	node, err = s.GetByPath(json.Number("key"))
	if err != nil {
		t.Fatalf("string json.Number path error = %v", err)
	}
	assertSearcherRaw(t, node, `"value"`)

	for _, path := range []interface{}{nil, struct{}{}} {
		if _, err := s.GetByPath(path); !errors.Is(err, ErrUnsupportType) {
			t.Fatalf("GetByPath(%T) error = %v, want ErrUnsupportType", path, err)
		}
	}
}

func assertSearcherRaw(t *testing.T, node Node, want string) {
	t.Helper()
	got, err := node.Raw()
	if err != nil {
		t.Fatalf("Node.Raw() error = %v", err)
	}
	if got != want {
		t.Fatalf("Node.Raw() = %q, want %q", got, want)
	}
}
