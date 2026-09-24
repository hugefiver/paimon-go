package ast

import (
	"sync"
	"testing"
)

func TestPartiallyLoadedNodeCopiesKeepIndependentCursors(t *testing.T) {
	for _, test := range []struct {
		name string
		copy func(*testing.T, *Node) Node
	}{
		{"assignment", func(_ *testing.T, n *Node) Node { return *n }},
		{"array_constructor", func(_ *testing.T, n *Node) Node {
			container := NewArray([]Node{*n})
			return *container.Index(0)
		}},
		{"object_constructor", func(_ *testing.T, n *Node) Node {
			container := NewObject([]Pair{NewPair("child", *n)})
			return *container.Get("child")
		}},
		{"array_use_node", func(t *testing.T, n *Node) Node {
			container := NewArray([]Node{*n})
			values, err := container.ArrayUseNode()
			if err != nil {
				t.Fatal(err)
			}
			return values[0]
		}},
		{"map_use_node", func(t *testing.T, n *Node) Node {
			container := NewObject([]Pair{NewPair("child", *n)})
			values, err := container.MapUseNode()
			if err != nil {
				t.Fatal(err)
			}
			return values["child"]
		}},
		{"array_iterator", func(t *testing.T, n *Node) Node {
			container := NewArray([]Node{*n})
			iterator, err := container.Values()
			if err != nil {
				t.Fatal(err)
			}
			var value Node
			if !iterator.Next(&value) {
				t.Fatal("missing iterator value")
			}
			return value
		}},
		{"object_iterator", func(t *testing.T, n *Node) Node {
			container := NewObject([]Pair{NewPair("child", *n)})
			iterator, err := container.Properties()
			if err != nil {
				t.Fatal(err)
			}
			var value Pair
			if !iterator.Next(&value) {
				t.Fatal("missing iterator value")
			}
			return value.Value
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := NewRaw(`{"a":1,"b":2,"c":3}`)
			if got, err := original.Get("a").Int64(); err != nil || got != 1 {
				t.Fatalf("first child = %d, %v", got, err)
			}
			copied := test.copy(t, &original)
			if got, err := original.Get("b").Int64(); err != nil || got != 2 {
				t.Fatalf("original second child = %d, %v", got, err)
			}
			if got, err := copied.Get("b").Int64(); err != nil || got != 2 {
				t.Fatalf("copied second child = %d, %v", got, err)
			}
			assertNodeRaw(t, &copied, `{"a":1,"b":2,"c":3}`)
			assertNodeRaw(t, &original, `{"a":1,"b":2,"c":3}`)
		})
	}
}

func TestLoadProtectsAlreadyExposedDeepDescendants(t *testing.T) {
	for range 32 {
		n := NewRaw(`{"a":{"b":{"x":1,"y":2,"untouched":[1,2,3]}}}`)
		b := n.Get("a").Get("b")
		if !b.Exists() {
			t.Fatal("missing nested container")
		}
		if err := n.Load(); err != nil {
			t.Fatal(err)
		}
		if !b.IsRaw() {
			t.Fatal("Load eagerly parsed an untouched grandchild")
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for range 16 {
					if got, err := n.GetByPath("a", "b", "x").Int64(); err != nil || got != 1 {
						t.Errorf("concurrent grandchild read = %d, %v", got, err)
						return
					}
				}
			}()
		}
		close(start)
		wg.Wait()
	}
}

func TestLazyCopiesKeepPointersInSharedIndexStorage(t *testing.T) {
	object := NewRaw(`{"a":1,"b":2,"c":3,"d":4,"e":5}`)
	_ = object.Get("c")
	copyObject := object
	p := object.Get("d")
	_ = copyObject.Get("d")
	*p = NewNumber("42")
	assertNodeRaw(t, &object, `{"a":1,"b":2,"c":3,"d":42,"e":5}`)
	assertNodeRaw(t, &copyObject, `{"a":1,"b":2,"c":3,"d":42,"e":5}`)

	array := NewRaw(`[1,2,3,4,5]`)
	_ = array.Index(2)
	copyArray := array
	p = array.Index(3)
	_ = copyArray.Index(3)
	*p = NewNumber("42")
	assertNodeRaw(t, &array, `[1,2,3,42,5]`)
	assertNodeRaw(t, &copyArray, `[1,2,3,42,5]`)
}

func TestConcurrentReadFiniteSnapshotUsesSharedMutex(t *testing.T) {
	for _, preload := range []bool{false, true} {
		for _, test := range []struct {
			src, want string
			add       func(*Node) error
		}{
			{`[1]`, `[1,[1]]`, func(n *Node) error { return n.Add(*n) }},
			{`{"a":1}`, `{"a":1,"snapshot":{"a":1}}`, func(n *Node) error {
				_, err := n.Set("snapshot", *n)
				return err
			}},
		} {
			n := NewRawConcurrentRead(test.src)
			if preload {
				if err := n.Load(); err != nil {
					t.Fatal(err)
				}
			}
			if err := test.add(&n); err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			for range 8 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for range 32 {
						if _, err := n.Interface(); err != nil {
							t.Error(err)
							return
						}
						if got, err := n.Raw(); err != nil || got != test.want {
							t.Errorf("Raw = %q, %v", got, err)
							return
						}
						if got, err := n.MarshalJSON(); err != nil || string(got) != test.want {
							t.Errorf("MarshalJSON = %q, %v", got, err)
							return
						}
					}
				}()
			}
			wg.Wait()
		}
	}
}

func TestConcurrentReadersCopyLazyChildren(t *testing.T) {
	for range 64 {
		array := NewRawConcurrentRead(`[{"x":1,"y":2,"z":3}]`)
		object := NewRawConcurrentRead(`{"child":{"x":1,"y":2,"z":3}}`)
		arrayChild, objectChild := array.Index(0), object.Get("child")
		check := func(value Node) {
			if got, err := value.Get("x").Int64(); err != nil || got != 1 {
				t.Errorf("copied child = %d, %v", got, err)
			}
		}
		readers := []func(){
			func() {
				for _, child := range []*Node{arrayChild, objectChild} {
					if got, err := child.Get("x").Int64(); err != nil || got != 1 {
						t.Errorf("original child = %d, %v", got, err)
					}
				}
			},
			func() {
				iterator, err := array.Values()
				if err != nil {
					t.Error(err)
					return
				}
				var value Node
				if !iterator.Next(&value) {
					t.Error("missing array child")
					return
				}
				check(value)
			},
			func() {
				iterator, err := object.Properties()
				if err != nil {
					t.Error(err)
					return
				}
				var value Pair
				if !iterator.Next(&value) {
					t.Error("missing object child")
					return
				}
				check(value.Value)
			},
			func() {
				values, err := array.ArrayUseNode()
				if err != nil || len(values) != 1 {
					t.Errorf("ArrayUseNode = %v, %v", values, err)
					return
				}
				check(values[0])
			},
			func() {
				values, err := object.MapUseNode()
				if err != nil || len(values) != 1 {
					t.Errorf("MapUseNode = %v, %v", values, err)
					return
				}
				check(values["child"])
			},
			func() {
				values, err := array.InterfaceUseNode()
				if err != nil {
					t.Error(err)
					return
				}
				check(values.([]Node)[0])
			},
			func() {
				values, err := object.InterfaceUseNode()
				if err != nil {
					t.Error(err)
					return
				}
				check(values.(map[string]Node)["child"])
			},
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		for _, read := range readers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				read()
			}()
		}
		close(start)
		wg.Wait()
	}
}
