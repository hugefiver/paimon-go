package rootbench

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/decoder"
	"github.com/bytedance/sonic/encoder"
)

var sinkNode ast.Node
var workloadValue = struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}{42, "paimon", true}

func containerFixture(n int) string {
	return `{"items":[` + strings.TrimSuffix(strings.Repeat(`{"id":42,"name":"paimon","ok":true},`, n), ",") + `]}`
}

func stringGetFixture(n int) string {
	return `{"id":42,"padding":"` + strings.Repeat("x", n) + `"}`
}

func TestWorkloadFixtures(t *testing.T) {
	for _, n := range []int{10, 1000} {
		src := []byte(containerFixture(n))
		var got struct{ Items []struct{ ID int } }
		if err := json.Unmarshal(src, &got); err != nil || len(got.Items) != n || got.Items[0].ID != 42 || got.Items[n-1].ID != 42 {
			t.Fatalf("container %d: items=%d err=%v", n, len(got.Items), err)
		}
		t.Logf("FIXTURE_SHA256 container%d=%x bytes=%d", n, sha256.Sum256(src), len(src))
	}
	for _, n := range []int{16, 65536} {
		src := []byte(stringGetFixture(n))
		var got struct {
			ID      int
			Padding string
		}
		if err := json.Unmarshal(src, &got); err != nil || got.ID != 42 || len(got.Padding) != n {
			t.Fatalf("string get %d: got=%d padding=%d err=%v", n, got.ID, len(got.Padding), err)
		}
		t.Logf("FIXTURE_SHA256 string%d=%x bytes=%d", n, sha256.Sum256(src), len(src))
	}
	for _, n := range []int{64, 256, 1024} {
		src := []byte(strings.Repeat("[", n) + "0" + strings.Repeat("]", n))
		if !json.Valid(src) {
			t.Fatalf("invalid preorder depth %d", n)
		}
		t.Logf("FIXTURE_SHA256 depth%d=%x bytes=%d", n, sha256.Sum256(src), len(src))
	}
	src := []byte(`{"payload":"` + strings.Repeat("x", 65536) + `"}`)
	if !json.Valid(src) {
		t.Fatal("invalid large string fixture")
	}
	t.Logf("FIXTURE_SHA256 largeString=%x bytes=%d", sha256.Sum256(src), len(src))
	src = []byte(strings.TrimSpace(strings.Repeat(`{"id":42} `, 100)))
	dec := json.NewDecoder(bytes.NewReader(src))
	for i := 0; i < 100; i++ {
		var got struct{ ID int }
		if err := dec.Decode(&got); err != nil || got.ID != 42 {
			t.Fatalf("stream item %d: %+v %v", i, got, err)
		}
	}
	t.Logf("FIXTURE_SHA256 stream=%x bytes=%d", sha256.Sum256(src), len(src))
}

func checkItems(b *testing.B, v ast.Node, want int) {
	b.Helper()
	// Searcher returns a lazy raw container: Len only materializes its header.
	// Load all children in the untimed correctness check before counting them.
	if err := v.LoadAll(); err != nil {
		b.Fatal(err)
	}
	n, err := v.Len()
	if err != nil || n != want {
		b.Fatalf("items length=%d err=%v, want %d", n, err, want)
	}
}

func checkID(b *testing.B, v ast.Node) {
	b.Helper()
	id, err := v.Int64()
	if err != nil || id != 42 {
		b.Fatalf("id=%d err=%v, want 42", id, err)
	}
}

func BenchmarkContainer(b *testing.B) {
	for _, n := range []int{10, 1000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			src := containerFixture(n)
			data := []byte(src)
			b.Run("Get", func(b *testing.B) {
				v, err := sonic.Get(data, "items")
				if err != nil {
					b.Fatal(err)
				}
				checkItems(b, v, n)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					v, err := sonic.Get(data, "items")
					if err != nil {
						b.Fatal(err)
					}
					sinkNode = v
				}
			})
			b.Run("SearcherDefault", func(b *testing.B) {
				s := ast.NewSearcher(src)
				v, err := s.GetByPath("items")
				if err != nil {
					b.Fatal(err)
				}
				checkItems(b, v, n)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					v, err := s.GetByPath("items")
					if err != nil {
						b.Fatal(err)
					}
					sinkNode = v
				}
			})
			b.Run("SearcherNoValidate", func(b *testing.B) {
				s := ast.NewSearcher(src)
				s.ValidateJSON = false
				v, err := s.GetByPath("items")
				if err != nil {
					b.Fatal(err)
				}
				checkItems(b, v, n)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					v, err := s.GetByPath("items")
					if err != nil {
						b.Fatal(err)
					}
					sinkNode = v
				}
			})
			b.Run("LoadAll", func(b *testing.B) {
				warm := ast.NewRaw(src)
				if err := warm.LoadAll(); err != nil {
					b.Fatal(err)
				}
				checkItems(b, *warm.GetByPath("items"), n)
				b.ReportAllocs()
				b.SetBytes(int64(len(data)))
				b.ResetTimer()
				for b.Loop() {
					v := ast.NewRaw(src)
					if err := v.LoadAll(); err != nil {
						b.Fatal(err)
					}
					sinkNode = v
				}
			})
		})
	}
}
func BenchmarkStringGet(b *testing.B) {
	for _, n := range []int{16, 65536} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			src := stringGetFixture(n)
			data := []byte(src)
			b.Run("Bytes", func(b *testing.B) {
				v, err := sonic.Get(data, "id")
				if err != nil {
					b.Fatal(err)
				}
				checkID(b, v)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					v, err := sonic.Get(data, "id")
					if err != nil {
						b.Fatal(err)
					}
					sinkNode = v
				}
			})
			b.Run("String", func(b *testing.B) {
				v, err := sonic.GetFromString(src, "id")
				if err != nil {
					b.Fatal(err)
				}
				checkID(b, v)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					v, err := sonic.GetFromString(src, "id")
					if err != nil {
						b.Fatal(err)
					}
					sinkNode = v
				}
			})
			b.Run("Searcher", func(b *testing.B) {
				v, err := ast.NewSearcher(src).GetByPath("id")
				if err != nil {
					b.Fatal(err)
				}
				checkID(b, v)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					v, err := ast.NewSearcher(src).GetByPath("id")
					if err != nil {
						b.Fatal(err)
					}
					sinkNode = v
				}
			})
			b.Run("CopyString", func(b *testing.B) {
				v, err := sonic.GetCopyFromString(src, "id")
				if err != nil {
					b.Fatal(err)
				}
				checkID(b, v)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					v, err := sonic.GetCopyFromString(src, "id")
					if err != nil {
						b.Fatal(err)
					}
					sinkNode = v
				}
			})
		})
	}
}
func BenchmarkEncoder(b *testing.B) {
	encoded := mustFixtureJSON(&workloadValue)
	b.Run("RootStream", func(b *testing.B) {
		e := sonic.ConfigDefault.NewEncoder(io.Discard)
		if err := e.Encode(&workloadValue); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.SetBytes(int64(len(encoded) + 1))
		b.ResetTimer()
		for b.Loop() {
			if err := e.Encode(&workloadValue); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("StdStream", func(b *testing.B) {
		e := json.NewEncoder(io.Discard)
		e.SetEscapeHTML(false)
		if err := e.Encode(&workloadValue); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.SetBytes(int64(len(encoded) + 1))
		b.ResetTimer()
		for b.Loop() {
			if err := e.Encode(&workloadValue); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("RootMarshal", func(b *testing.B) {
		if data, err := sonic.Marshal(&workloadValue); err != nil || !bytes.Equal(data, encoded) {
			b.Fatalf("marshal warmup: %s %v", data, err)
		}
		b.ReportAllocs()
		b.SetBytes(int64(len(encoded)))
		b.ResetTimer()
		for b.Loop() {
			var err error
			sinkBytes, err = sonic.Marshal(&workloadValue)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("EncodeIntoReuse", func(b *testing.B) {
		out := make([]byte, 0, 1024)
		if err := encoder.EncodeInto(&out, &workloadValue, 0); err != nil || !bytes.Equal(out, encoded) {
			b.Fatalf("EncodeInto warmup: %s %v", out, err)
		}
		b.ReportAllocs()
		b.SetBytes(int64(len(encoded)))
		b.ResetTimer()
		for b.Loop() {
			out = out[:0]
			if err := encoder.EncodeInto(&out, &workloadValue, 0); err != nil {
				b.Fatal(err)
			}
			sinkBytes = out
		}
	})
}
func BenchmarkDecodeNumbers(b *testing.B) {
	src := []byte(`{"a":1,"b":2,"c":3,"d":[4,5,6]}`)
	for _, mode := range []string{"Default", "UseNumber", "UseInt64"} {
		b.Run(mode, func(b *testing.B) {
			api := sonic.Config{UseNumber: mode == "UseNumber", UseInt64: mode == "UseInt64"}.Froze()
			var warm map[string]any
			if err := api.Unmarshal(src, &warm); err != nil {
				b.Fatal(err)
			}
			want := any(float64(1))
			if mode == "UseNumber" {
				want = json.Number("1")
			}
			if mode == "UseInt64" {
				want = int64(1)
			}
			if warm["a"] != want {
				b.Fatalf("%s decoded a=%T %v, want %T %v", mode, warm["a"], warm["a"], want, want)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(src)))
			b.ResetTimer()
			for b.Loop() {
				var v any
				if err := api.Unmarshal(src, &v); err != nil {
					b.Fatal(err)
				}
				sinkAny = v
			}
		})
	}
}
func BenchmarkStringDecoder(b *testing.B) {
	src := strings.TrimSpace(strings.Repeat(`{"id":42} `, 100))
	b.Run("PackageDecoder", func(b *testing.B) {
		warm := decoder.NewDecoder(src)
		for i := 0; i < 100; i++ {
			var v struct{ ID int }
			if err := warm.Decode(&v); err != nil || v.ID != 42 {
				b.Fatalf("package decoder warmup: %+v %v", v, err)
			}
		}
		b.ReportAllocs()
		b.SetBytes(int64(len(src)))
		b.ResetTimer()
		for b.Loop() {
			d := decoder.NewDecoder(src)
			for i := 0; i < 100; i++ {
				var v struct{ ID int }
				if err := d.Decode(&v); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
	b.Run("StdDecoder", func(b *testing.B) {
		warm := json.NewDecoder(strings.NewReader(src))
		for i := 0; i < 100; i++ {
			var v struct{ ID int }
			if err := warm.Decode(&v); err != nil || v.ID != 42 {
				b.Fatalf("stdlib decoder warmup: %+v %v", v, err)
			}
		}
		b.ReportAllocs()
		b.SetBytes(int64(len(src)))
		b.ResetTimer()
		for b.Loop() {
			d := json.NewDecoder(strings.NewReader(src))
			for i := 0; i < 100; i++ {
				var v struct{ ID int }
				if err := d.Decode(&v); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}
func TestBasicEquivalence(t *testing.T) {
	var a, b bytes.Buffer
	e := sonic.ConfigDefault.NewEncoder(&a)
	if err := e.Encode(&workloadValue); err != nil {
		t.Fatal(err)
	}
	s := json.NewEncoder(&b)
	s.SetEscapeHTML(false)
	if err := s.Encode(&workloadValue); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Fatal(a.String(), b.String())
	}
}

type noopVisitor struct{}

func (noopVisitor) OnNull() error                        { return nil }
func (noopVisitor) OnBool(bool) error                    { return nil }
func (noopVisitor) OnString(string) error                { return nil }
func (noopVisitor) OnInt64(int64, json.Number) error     { return nil }
func (noopVisitor) OnFloat64(float64, json.Number) error { return nil }
func (noopVisitor) OnObjectBegin(int) error              { return nil }
func (noopVisitor) OnObjectKey(string) error             { return nil }
func (noopVisitor) OnObjectEnd() error                   { return nil }
func (noopVisitor) OnArrayBegin(int) error               { return nil }
func (noopVisitor) OnArrayEnd() error                    { return nil }
func BenchmarkPreorderDepth(b *testing.B) {
	for _, n := range []int{64, 256, 1024} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			src := strings.Repeat("[", n) + "0" + strings.Repeat("]", n)
			if err := ast.Preorder(src, noopVisitor{}, nil); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(src)))
			b.ResetTimer()
			for b.Loop() {
				if err := ast.Preorder(src, noopVisitor{}, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkValidStringLarge(b *testing.B) {
	src := `{"payload":"` + strings.Repeat("x", 65536) + `"}`
	if !sonic.ValidString(src) {
		b.Fatal("valid string fixture rejected")
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for b.Loop() {
		sinkBool = sonic.ValidString(src)
	}
}

func BenchmarkUnmarshalLargeString(b *testing.B) {
	data := []byte(`{"payload":"` + strings.Repeat("x", 65536) + `"}`)
	var warm map[string]any
	if err := sonic.Unmarshal(data, &warm); err != nil || len(warm["payload"].(string)) != 65536 {
		b.Fatalf("large decode warmup: %v", err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for b.Loop() {
		var out map[string]any
		if err := sonic.Unmarshal(data, &out); err != nil {
			b.Fatal(err)
		}
		sinkAny = out
	}
}
