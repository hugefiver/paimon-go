package rootbench

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/bytedance/sonic"
)

type smallStruct struct {
	ID      int      `json:"id"`
	Name    string   `json:"name"`
	Active  bool     `json:"active"`
	Score   float64  `json:"score"`
	Tags    []string `json:"tags"`
	Profile profile  `json:"profile"`
}

type profile struct {
	Email string `json:"email"`
	Age   int    `json:"age"`
}

var smallValue = smallStruct{
	ID:     42,
	Name:   "paimon",
	Active: true,
	Score:  98.75,
	Tags:   []string{"json", "benchmark", "sonic"},
	Profile: profile{
		Email: "paimon@example.com",
		Age:   7,
	},
}

var mediumJSON = []byte(`{
	"service":"paimon-go",
	"version":15,
	"enabled":true,
	"limits":{"read":1024,"write":2048,"burst":4096},
	"regions":["iad","sfo","sin","fra"],
	"features":{"marshal":true,"unmarshal":true,"valid":true,"get":true},
	"items":[
		{"id":1,"name":"alpha","scores":[1,2,3],"meta":{"owner":"team-a","hot":true}},
		{"id":2,"name":"beta","scores":[4,5,6],"meta":{"owner":"team-b","hot":false}},
		{"id":3,"name":"gamma","scores":[7,8,9],"meta":{"owner":"team-c","hot":true}}
	],
	"note":"medium payload used for stable benchmark input"
}`)

var pathJSON = []byte(`{
	"users":[
		{"id":1001,"name":"alice","roles":["admin","writer"]},
		{"id":1002,"name":"bob","roles":["reader"]},
		{"id":1003,"name":"carol","roles":["writer","reader"]}
	],
	"count":3
}`)

var sinkBytes []byte
var sinkBool bool
var sinkAny interface{}
var sinkInt int64

func TestBenchmarkEnvironment(t *testing.T) {
	mode := os.Getenv("BENCH_MODE")
	if mode == "" {
		return // Permit direct go test without the matrix runner.
	}
	wantVersion := "go1.27"
	if mode == "upstream-native" {
		wantVersion = "go1.26.7"
		if sonic.APIKind != 1 {
			t.Fatalf("upstream Sonic is not native: APIKind=%d, want 1", sonic.APIKind)
		}
	} else if mode != "local-default" && mode != "local-stdjson" && mode != "local-jsonv2" {
		t.Fatalf("unknown benchmark mode %q", mode)
	}
	if !strings.HasPrefix(runtime.Version(), wantVersion) ||
		(wantVersion == "go1.26.7" && runtime.Version() != wantVersion) {
		t.Fatalf("runtime=%s, want %s", runtime.Version(), wantVersion)
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" || runtime.GOMAXPROCS(0) != 1 || os.Getenv("GOAMD64") != "v1" || os.Getenv("GOGC") != "100" {
		t.Fatalf("unexpected benchmark environment: %s/%s GOMAXPROCS=%d GOAMD64=%q GOGC=%q", runtime.GOOS, runtime.GOARCH, runtime.GOMAXPROCS(0), os.Getenv("GOAMD64"), os.Getenv("GOGC"))
	}
	if experiment := os.Getenv("GOEXPERIMENT"); (mode == "local-jsonv2" && experiment != "jsonv2") || (mode != "local-jsonv2" && experiment != "") {
		t.Fatalf("mode=%s GOEXPERIMENT=%q", mode, experiment)
	}
	t.Logf("PROOF mode=%s runtime=%s APIKind=%d GOMAXPROCS=%d GOAMD64=%s GOGC=%s GOEXPERIMENT=%s", mode, runtime.Version(), sonic.APIKind, runtime.GOMAXPROCS(0), os.Getenv("GOAMD64"), os.Getenv("GOGC"), os.Getenv("GOEXPERIMENT"))
}

func TestBenchmarkFixtures(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"small", mustFixtureJSON(smallValue)}, {"medium", mediumJSON}, {"path", pathJSON},
	} {
		if !json.Valid(tc.data) || !sonic.Valid(tc.data) {
			t.Fatalf("%s fixture rejected", tc.name)
		}
		t.Logf("FIXTURE_SHA256 %s=%x bytes=%d", tc.name, sha256.Sum256(tc.data), len(tc.data))
	}
	var got smallStruct
	if err := sonic.Unmarshal(mustFixtureJSON(smallValue), &got); err != nil || !reflect.DeepEqual(got, smallValue) {
		t.Fatalf("small round trip: got=%+v err=%v", got, err)
	}
	var m map[string]interface{}
	if err := sonic.Unmarshal(mediumJSON, &m); err != nil || m["service"] != "paimon-go" || m["version"] != float64(15) || len(m["items"].([]interface{})) != 3 {
		t.Fatalf("medium map decode: got=%v err=%v", m, err)
	}
	node, err := sonic.Get(pathJSON, "users", 1, "id")
	if err != nil {
		t.Fatal(err)
	}
	id, err := node.Int64()
	if err != nil || id != 1002 {
		t.Fatalf("path lookup: id=%d err=%v, want 1002", id, err)
	}
	if sonic.Valid([]byte(`{"id":`)) {
		t.Fatal("accepted malformed JSON")
	}
}

func mustFixtureJSON(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("invalid benchmark fixture: %v", err))
	}
	return data
}

func BenchmarkMarshalSmallStruct(b *testing.B) {
	if out, err := sonic.Marshal(smallValue); err != nil || !json.Valid(out) {
		b.Fatalf("marshal warmup: %q %v", out, err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(mustFixtureJSON(smallValue))))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := sonic.Marshal(smallValue)
		if err != nil {
			b.Fatal(err)
		}
		sinkBytes = out
	}
}

func BenchmarkUnmarshalMediumMap(b *testing.B) {
	var warm map[string]interface{}
	if err := sonic.Unmarshal(mediumJSON, &warm); err != nil || warm["service"] != "paimon-go" {
		b.Fatalf("unmarshal warmup: %v %v", warm, err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(mediumJSON)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out map[string]interface{}
		if err := sonic.Unmarshal(mediumJSON, &out); err != nil {
			b.Fatal(err)
		}
		sinkAny = out
	}
}

func BenchmarkValidMedium(b *testing.B) {
	if !sonic.Valid(mediumJSON) {
		b.Fatal("valid fixture rejected")
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(mediumJSON)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkBool = sonic.Valid(mediumJSON)
	}
}

func BenchmarkGetPath(b *testing.B) {
	node, err := sonic.Get(pathJSON, "users", 1, "id")
	if err != nil {
		b.Fatal(err)
	}
	if id, err := node.Int64(); err != nil || id != 1002 {
		b.Fatalf("path warmup: %d %v", id, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		node, err := sonic.Get(pathJSON, "users", 1, "id")
		if err != nil {
			b.Fatal(err)
		}
		got, err := node.Int64()
		if err != nil {
			b.Fatal(err)
		}
		sinkInt = got
	}
}
