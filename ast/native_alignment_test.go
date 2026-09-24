package ast

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	nativetypes "github.com/bytedance/sonic/internal/native/types"
)

func TestNativeChildPointersSurviveSiblingMutations(t *testing.T) {
	array := NewArray([]Node{NewNumber("1")})
	first := array.Index(0)
	for i := 0; i < 64; i++ {
		if err := array.Add(NewNumber("2")); err != nil {
			t.Fatal(err)
		}
	}
	*first = NewNumber("3")
	if got, _ := array.Index(0).Int64(); got != 3 {
		t.Fatalf("retained array child update lost: %d", got)
	}

	object := NewObject([]Pair{NewPair("a", NewNumber("1")), NewPair("b", NewNumber("2")), NewPair("c", NewNumber("3"))})
	removed, retained := object.Get("a"), object.Get("b")
	for i := 0; i < 64; i++ {
		if _, err := object.Set(fmt.Sprint(i), NewNull()); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := object.Unset("a"); err != nil || !ok {
		t.Fatalf("Unset: %v, %v", ok, err)
	}
	if removed.Exists() {
		t.Fatal("removed pointer still refers to a value")
	}
	if got, _ := retained.Int64(); got != 2 {
		t.Fatalf("sibling removal changed retained child: %d", got)
	}
	*retained = NewNumber("4")
	if got, _ := object.Get("b").Int64(); got != 4 {
		t.Fatalf("retained object child update lost: %d", got)
	}
}

func TestNativeAbsentContainerChildren(t *testing.T) {
	array := NewArray([]Node{{}, NewNumber("1"), {}})
	assertNodeRaw(t, &array, "[1]")
	values, err := array.Array()
	if err != nil || !reflect.DeepEqual(values, []interface{}{float64(1)}) {
		t.Fatalf("Array: %#v, %v", values, err)
	}
	var positions []int
	if err := array.ForEach(func(path Sequence, _ *Node) bool { positions = append(positions, path.Index); return true }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(positions, []int{1}) {
		t.Fatalf("ForEach positions: %v", positions)
	}
	it, err := array.Values()
	if err != nil {
		t.Fatal(err)
	}
	var value Node
	if !it.Next(&value) || it.Pos() != 2 || it.Next(&value) {
		t.Fatal("iterator did not skip absent children")
	}

	object := NewObject([]Pair{NewPair("missing", Node{}), NewPair("present", NewNull())})
	assertNodeRaw(t, &object, `{"present":null}`)
	m, err := object.Map()
	if err != nil || len(m) != 1 {
		t.Fatalf("Map: %#v, %v", m, err)
	}
}

func TestNativeUnsupportedTypeSentinel(t *testing.T) {
	n := NewNumber("1")
	for _, test := range []struct {
		name string
		run  func() error
	}{
		{"Set", func() error { _, e := n.Set("x", NewNull()); return e }},
		{"Add", func() error { return n.Add(NewNull()) }},
		{"Values", func() error { _, e := n.Values(); return e }},
		{"Properties", func() error { _, e := n.Properties(); return e }},
		{"StrictString", func() error { _, e := n.StrictString(); return e }},
		{"StrictBool", func() error { _, e := n.StrictBool(); return e }},
		{"Array", func() error { _, e := n.Array(); return e }},
		{"Map", func() error { _, e := n.Map(); return e }},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); !errors.Is(err, ErrUnsupportType) {
				t.Fatalf("error = %v, want ErrUnsupportType", err)
			}
		})
	}
}

func TestNativeParserErrorOffsetsAndSource(t *testing.T) {
	for _, test := range []struct {
		src  string
		pos  int
		code nativetypes.ParsingError
	}{
		{`   truX`, 6, nativetypes.ERR_INVALID_CHAR},
		{`"abc\q"`, 7, nativetypes.ERR_INVALID_ESCAPE},
		{`"x\uZZZZ"`, 9, nativetypes.ERR_INVALID_CHAR},
		{`1.`, 1, nativetypes.ERR_INVALID_CHAR},
		{strings.Repeat(" ", 70) + `truX`, 73, nativetypes.ERR_INVALID_CHAR},
	} {
		p := NewParser(test.src)
		n, code := p.Parse()
		if code != test.code || p.Pos() != test.pos || n.Type() != V_NONE {
			t.Fatalf("Parse(%q) = code %v, pos %d, type %d", test.src, code, p.Pos(), n.Type())
		}
		e := p.ExportError(code).(*SyntaxError)
		if e.Src != test.src || e.Pos != test.pos {
			t.Fatalf("ExportError lost source or offset: %#v", e)
		}
	}
}

func TestNativeLazyChildrenAndLoadAliases(t *testing.T) {
	n := NewRaw(`{"good":1,"\q":2}`)
	if got, err := n.Len(); err != nil || got != 0 {
		t.Fatalf("unloaded Len: %d, %v", got, err)
	}
	if got, err := n.Get("good").Int64(); err != nil || got != 1 {
		t.Fatalf("selected value: %d, %v", got, err)
	}
	if got, err := n.Len(); err != nil || got != 1 {
		t.Fatalf("partially loaded Len: %d, %v", got, err)
	}

	for _, all := range []bool{false, true} {
		n := NewRaw(`{"a":[ 1, 2 ]}`)
		var err error
		if all {
			err = n.LoadAll()
		} else {
			err = n.Load()
		}
		if err != nil {
			t.Fatal(err)
		}
		child := n.Get("a")
		if !child.IsRaw() {
			t.Fatalf("LoadAll=%v unexpectedly parsed container child", all)
		}
		assertNodeRaw(t, child, `[ 1, 2 ]`)
	}
}

func TestNativeForEachStopsBeforeUnvisitedMalformedKey(t *testing.T) {
	n := NewRaw(`{"good":1,"\q":2}`)
	calls := 0
	if err := n.ForEach(func(_ Sequence, _ *Node) bool { calls++; return false }); err != nil || calls != 1 {
		t.Fatalf("ForEach: calls=%d, err=%v", calls, err)
	}
}

func TestLoadMakesPartiallyReadTreeConcurrent(t *testing.T) {
	n := NewRaw(concurrentReadTarget)
	if got, err := n.Get("nested").Get("integer").Int64(); err != nil || got != 1 {
		t.Fatalf("partial read: %d,%v", got, err)
	}
	if err := n.Load(); err != nil {
		t.Fatal(err)
	}
	runConcurrentReads(t, &n)
}

func TestNativeParserErrorCaretAtEndOfSource(t *testing.T) {
	p := NewParser(`"abc\q"`)
	_, code := p.Parse()
	got := p.ExportError(code).(*SyntaxError).Description()
	if !strings.Contains(got, "invalid escape char") || !strings.Contains(got, "\t.......^\n") {
		t.Fatalf("EOF-position description = %q", got)
	}
}

func TestNativeNumberContinuationBoundaries(t *testing.T) {
	for _, src := range []string{"1+00", "1-2", "1.2.3", "1e2e3", "1e2+3", "0.1+2", "0e2+3"} {
		if _, err := NewSearcher(src).GetByPath(); err == nil {
			t.Fatalf("Searcher accepted %q", src)
		}
		if n := NewRaw(src); n.Type() != V_ERROR {
			t.Fatalf("NewRaw accepted %q", src)
		}
		if err := Preorder(src, &recordingVisitor{}, nil); err == nil {
			t.Fatalf("Preorder accepted %q", src)
		}
	}
	for _, src := range []string{"0+1", "0-1", "01+2"} {
		n := NewRaw(src)
		assertNodeRaw(t, &n, "0")
	}
}

func TestNativeZeroNodeUnsupportedOperations(t *testing.T) {
	var n Node
	for _, test := range []struct {
		name string
		run  func() error
	}{
		{"Int64", func() error { _, e := n.Int64(); return e }},
		{"Bool", func() error { _, e := n.Bool(); return e }},
		{"String", func() error { _, e := n.String(); return e }},
		{"Interface", func() error { _, e := n.Interface(); return e }},
		{"Values", func() error { _, e := n.Values(); return e }},
		{"Properties", func() error { _, e := n.Properties(); return e }},
		{"Unset", func() error { _, e := n.Unset("x"); return e }},
		{"UnsetByIndex", func() error { _, e := n.UnsetByIndex(0); return e }},
		{"Pop", n.Pop},
		{"Move", func() error { return n.Move(0, 0) }},
		{"Array", func() error { _, e := n.Array(); return e }},
		{"Map", func() error { _, e := n.Map(); return e }},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); !errors.Is(err, ErrUnsupportType) {
				t.Fatalf("error=%v, want ErrUnsupportType", err)
			}
		})
	}
	if err := n.SortKeys(false); err != nil {
		t.Fatalf("zero SortKeys: %v", err)
	}
}

func TestNativeInterfaceUseNodeClonesScalars(t *testing.T) {
	for _, n := range []Node{{}, NewNull(), NewBool(true), NewString("text"), NewNumber("1"), NewAny(1)} {
		value, err := n.InterfaceUseNode()
		clone, ok := value.(Node)
		if err != nil || !ok || clone.Type() != n.Type() {
			t.Fatalf("InterfaceUseNode(type=%d) = %T, %v", n.Type(), value, err)
		}
	}
}
