package difftest

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestNativeConsumerContract(t *testing.T) {
	source, err := filepath.Abs(filepath.Join("testdata", "contract", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	expected := runContract(t, source, "upstream", "")
	for _, tags := range []string{"", "sonic_stdjson", "sonic_jsonv2"} {
		name := tags
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			got := runContract(t, source, "local", tags)
			for key, want := range expected {
				if !reflect.DeepEqual(got[key], want) {
					t.Errorf("%s: local=%s upstream=%s", key, got[key], want)
				}
			}
			for key := range got {
				if _, ok := expected[key]; !ok {
					t.Errorf("unexpected result %s", key)
				}
			}
		})
	}
}

func runContract(t *testing.T, source, dir, tags string) map[string]json.RawMessage {
	t.Helper()
	binary := filepath.Join(t.TempDir(), helperExecutableName("contract"))
	args := []string{"build", "-mod=readonly", "-o", binary}
	if tags != "" {
		args = append(args, "-tags="+tags)
	}
	args = append(args, source)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	if dir == "upstream" {
		chain := os.Getenv("SONIC_DIFFTEST_UPSTREAM_TOOLCHAIN")
		if chain == "" {
			chain = "go1.26.7"
		}
		cmd.Env = helperEnvironment(chain)
	}
	if tags == "sonic_jsonv2" {
		cmd.Env = append(helperEnvironment("auto"), "GOEXPERIMENT=jsonv2")
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s %s contract: %v\n%s", dir, tags, err, output)
	}
	ctx2, cancel2 := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel2()
	output, err := exec.CommandContext(ctx2, binary).CombinedOutput()
	if err != nil {
		t.Fatalf("run %s %s contract: %v\n%s", dir, tags, err, output)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(output), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
