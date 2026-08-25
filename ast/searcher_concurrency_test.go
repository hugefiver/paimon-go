package ast

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"unsafe"
)

const concurrentReadTarget = `{"nested":{"integer":1,"float":1.5,"number":2,"bool":true,"string":"sonic","array":[1],"map":{"key":1},"interface":{"key":1}}}`

const concurrentReadFixture = `{"target":` + concurrentReadTarget + `}`

func TestSearcherConcurrentReadLazyLoad(t *testing.T) {
	searcher := NewSearcher(concurrentReadFixture)
	searcher.ConcurrentRead = true
	node, err := searcher.GetByPath("target")
	if err != nil {
		t.Fatalf("GetByPath(target) error = %v", err)
	}

	runConcurrentReads(t, &node)
}

func runConcurrentReads(t *testing.T, nodes ...*Node) {
	t.Helper()

	start := make(chan struct{})
	errs := make(chan error, 1)
	var once sync.Once
	report := func(err error) {
		once.Do(func() { errs <- err })
	}

	var wg sync.WaitGroup
	for worker := range 32 {
		wg.Add(1)
		go func(node *Node) {
			defer wg.Done()
			<-start
			for range 128 {
				if got := node.TypeSafe(); got != V_OBJECT {
					report(fmt.Errorf("TypeSafe() = %d, want %d", got, V_OBJECT))
					return
				}
				integer := node.GetByPath("nested", "integer")
				if integer == nil {
					report(fmt.Errorf("GetByPath(nested, integer) = nil"))
					return
				}
				if got, err := integer.Int64(); err != nil || got != 1 {
					report(fmt.Errorf("Int64() = %d, %v; want 1, nil", got, err))
					return
				}
				if got, err := node.GetByPath("nested", "float").Float64(); err != nil || got != 1.5 {
					report(fmt.Errorf("Float64() = %v, %v; want 1.5, nil", got, err))
					return
				}
				if got, err := node.GetByPath("nested", "number").Number(); err != nil || got != "2" {
					report(fmt.Errorf("Number() = %q, %v; want 2, nil", got, err))
					return
				}
				if got, err := node.GetByPath("nested", "bool").Bool(); err != nil || !got {
					report(fmt.Errorf("Bool() = %v, %v; want true, nil", got, err))
					return
				}
				if got, err := node.GetByPath("nested", "string").String(); err != nil || got != "sonic" {
					report(fmt.Errorf("String() = %q, %v; want sonic, nil", got, err))
					return
				}
				if got, err := node.GetByPath("nested", "array").Array(); err != nil || len(got) != 1 {
					report(fmt.Errorf("Array() = %#v, %v; want one item, nil", got, err))
					return
				}
				if got, err := node.GetByPath("nested", "map").Map(); err != nil || got["key"] != float64(1) {
					report(fmt.Errorf("Map() = %#v, %v; want key 1, nil", got, err))
					return
				}
				if got, err := node.GetByPath("nested", "interface").Interface(); err != nil || got.(map[string]interface{})["key"] != float64(1) {
					report(fmt.Errorf("Interface() = %#v, %v; want key 1, nil", got, err))
					return
				}
				if got, err := node.Raw(); err != nil || got == "" {
					report(fmt.Errorf("Raw() = %q, %v; want non-empty raw, nil", got, err))
					return
				}
				if got, err := node.MarshalJSON(); err != nil || len(got) == 0 {
					report(fmt.Errorf("MarshalJSON() = %q, %v; want non-empty JSON, nil", got, err))
					return
				}
			}
		}(nodes[worker%len(nodes)])
	}
	close(start)
	wg.Wait()
	select {
	case err := <-errs:
		t.Error(err)
	default:
	}
}

func TestConcurrentReadNodeConstruction(t *testing.T) {
	ordinary := NewRaw(`{"key":1}`)
	if ordinary.mu != nil {
		t.Fatal("NewRaw allocated a mutex")
	}
	invalid := NewRawConcurrentRead(`}`)
	if invalid.mu != nil {
		t.Fatal("invalid NewRawConcurrentRead allocated a mutex")
	}

	concurrent := NewRawConcurrentRead(concurrentReadTarget)
	if concurrent.mu == nil {
		t.Fatal("NewRawConcurrentRead did not allocate a mutex")
	}
	copied := concurrent
	if copied.mu != concurrent.mu {
		t.Fatal("copied concurrent nodes do not share a mutex")
	}
	runConcurrentReads(t, &concurrent, &copied)

	defaultSearcher := NewSearcher(`{"target":1}`)
	defaultNode, err := defaultSearcher.GetByPath("target")
	if err != nil {
		t.Fatalf("default GetByPath error = %v", err)
	}
	if defaultNode.mu != nil {
		t.Fatal("default Searcher result allocated a mutex")
	}

	looseSearcher := NewSearcher(`{"target":{garbage}}`)
	looseSearcher.ValidateJSON = false
	looseSearcher.ConcurrentRead = true
	loose, err := looseSearcher.GetByPath("target")
	if err != nil {
		t.Fatalf("loose concurrent GetByPath error = %v", err)
	}
	if loose.mu == nil {
		t.Fatal("loose concurrent Searcher result did not allocate a mutex")
	}
	if got, err := loose.Raw(); err != nil || got != `{garbage}` {
		t.Fatalf("loose concurrent Raw() = %q, %v; want {garbage}, nil", got, err)
	}

	copySearcher := NewSearcher(`{"target":1}`)
	copySearcher.ConcurrentRead = true
	if _, err := copySearcher.GetByPathCopy("target"); err != nil {
		t.Fatalf("GetByPathCopy error = %v", err)
	}
	if !copySearcher.CopyReturn {
		t.Fatal("GetByPathCopy did not persist CopyReturn")
	}

	if runtime.GOARCH == "amd64" {
		size := unsafe.Sizeof(Node{})
		t.Logf("unsafe.Sizeof(Node{}) = %d", size)
		if size > 152 {
			t.Fatalf("unsafe.Sizeof(Node{}) = %d, want <= 152", size)
		}
	}
}
