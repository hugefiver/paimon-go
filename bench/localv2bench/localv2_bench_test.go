//go:build goexperiment.jsonv2

package localv2bench

import (
	"testing"

	"github.com/bytedance/sonic/stdjsonv2"
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

func BenchmarkMarshalSmallStruct(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		out, err := stdjsonv2.Marshal(smallValue)
		if err != nil {
			b.Fatal(err)
		}
		sinkBytes = out
	}
}

func BenchmarkUnmarshalMediumMap(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var out map[string]interface{}
		if err := stdjsonv2.Unmarshal(mediumJSON, &out); err != nil {
			b.Fatal(err)
		}
		sinkAny = out
	}
}

func BenchmarkValidMedium(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkBool = stdjsonv2.Valid(mediumJSON)
	}
}

func BenchmarkGetPath(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		node, err := stdjsonv2.Get(pathJSON, "users", 1, "id")
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
