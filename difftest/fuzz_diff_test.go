package difftest

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const (
	helperTimeout             = 30 * time.Second
	helperWaitDelay           = time.Second
	helperCleanupTimeout      = 5 * time.Second
	helperCleanupRetryDelay   = 50 * time.Millisecond
	maxDifferentialInputBytes = 64 << 10
	maxHelperRequestBytes     = 1 << 20
	maxHelperOutputBytes      = 1 << 20
)

const (
	helperTempDirEnv          = "SONIC_DIFFTEST_HELPER_TEMP_DIR"
	helperHangChildEnv        = "SONIC_DIFFTEST_HELPER_HANG_CHILD"
	helperCleanupHangChildEnv = "SONIC_DIFFTEST_HELPER_CLEANUP_HANG_CHILD"
)

var (
	errHelperOutputTooLarge = errors.New("helper output exceeds 1 MiB")
	helperExecutables       helperExecutablePaths
)

type helperExecutablePaths struct {
	local    string
	upstream string
}

func TestMain(m *testing.M) {
	if os.Getenv(helperHangChildEnv) == "1" || os.Getenv(helperCleanupHangChildEnv) != "" {
		os.Exit(m.Run())
	}

	if tempDir := os.Getenv(helperTempDirEnv); tempDir != "" {
		helperExecutables = helperExecutablesFor(tempDir)
		os.Exit(m.Run())
	}

	tempDir, err := os.MkdirTemp("", "sonic-difftest-helpers-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create helper temp directory: %v\n", err)
		os.Exit(1)
	}
	helperExecutables = helperExecutablesFor(tempDir)
	if err := buildHelperExecutables(helperExecutables); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		if cleanupErr := removeAllWithRetry(tempDir); cleanupErr != nil {
			fmt.Fprintf(os.Stderr, "remove helper temp directory %q: %v\n", tempDir, cleanupErr)
		}
		os.Exit(1)
	}
	if err := os.Setenv(helperTempDirEnv, tempDir); err != nil {
		fmt.Fprintf(os.Stderr, "set helper temp directory environment: %v\n", err)
		if cleanupErr := removeAllWithRetry(tempDir); cleanupErr != nil {
			fmt.Fprintf(os.Stderr, "remove helper temp directory %q: %v\n", tempDir, cleanupErr)
		}
		os.Exit(1)
	}

	exitCode := m.Run()
	if err := os.Unsetenv(helperTempDirEnv); err != nil {
		fmt.Fprintf(os.Stderr, "unset helper temp directory environment: %v\n", err)
		exitCode = 1
	}
	if err := removeAllWithRetry(tempDir); err != nil {
		fmt.Fprintf(os.Stderr, "remove helper temp directory %q: %v\n", tempDir, err)
		exitCode = 1
	}
	os.Exit(exitCode)
}

func helperExecutablesFor(tempDir string) helperExecutablePaths {
	return helperExecutablePaths{
		local:    filepath.Join(tempDir, helperExecutableName("soniclocal")),
		upstream: filepath.Join(tempDir, helperExecutableName("sonicupstream")),
	}
}

func helperExecutableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func buildHelperExecutables(paths helperExecutablePaths) error {
	for _, helper := range []struct {
		name        string
		dir         string
		packagePath string
		output      string
	}{
		{name: "local", dir: "local", packagePath: "./cmd/soniclocal", output: paths.local},
		{name: "upstream", dir: "upstream", packagePath: "./cmd/sonicupstream", output: paths.upstream},
	} {
		cmd := exec.Command("go", "build", "-mod=readonly", "-o", helper.output, helper.packagePath)
		cmd.Dir = helper.dir
		if helper.name == "upstream" {
			toolchain := os.Getenv("SONIC_DIFFTEST_UPSTREAM_TOOLCHAIN")
			if toolchain == "" {
				toolchain = "go1.26.7"
			}
			cmd.Env = helperEnvironment(toolchain)
		}
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("build %s helper: %w\n%s", helper.name, err, output)
		}
		if err := verifyNativeHelper(helper.output); err != nil {
			return fmt.Errorf("verify %s helper: %w", helper.name, err)
		}
	}
	return nil
}

func helperEnvironment(toolchain string) []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "GOTOOLCHAIN=") && !strings.HasPrefix(item, "GOEXPERIMENT=") {
			env = append(env, item)
		}
	}
	return append(env, "GOTOOLCHAIN="+toolchain, "GOEXPERIMENT=")
}

func verifyNativeHelper(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), helperTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, "--info").CombinedOutput()
	if err != nil {
		return fmt.Errorf("read helper identity: %w: %s", err, output)
	}
	var info struct {
		GoVersion string `json:"go_version"`
		APIKind   int    `json:"api_kind"`
	}
	if err := json.Unmarshal(output, &info); err != nil {
		return err
	}
	if info.APIKind != 1 || info.GoVersion == "" {
		return fmt.Errorf("expected native Sonic (APIKind=1), got %s", output)
	}
	return nil
}

func removeAllWithRetry(path string) error {
	deadline := time.Now().Add(helperCleanupTimeout)
	for {
		err := os.RemoveAll(path)
		if err == nil {
			return nil
		}
		retryDelay := helperCleanupRetryDelay
		if remaining := time.Until(deadline); remaining <= 0 {
			return err
		} else if remaining < retryDelay {
			retryDelay = remaining
		}
		time.Sleep(retryDelay)
	}
}

type pathPart struct {
	Kind  string `json:"kind"`
	Key   string `json:"key,omitempty"`
	Index int    `json:"index,omitempty"`
}

type boundedOutputBuffer struct {
	bytes.Buffer
	err error
}

func (b *boundedOutputBuffer) Write(p []byte) (int, error) {
	if len(p) > maxHelperOutputBytes-b.Len() {
		b.err = errHelperOutputTooLarge
		return 0, errHelperOutputTooLarge
	}
	return b.Buffer.Write(p)
}

type request struct {
	Data string     `json:"data"`
	Path []pathPart `json:"path"`
}

type result struct {
	Valid              bool   `json:"valid"`
	EncoderValid       bool   `json:"encoder_valid"`
	EncoderValidStart  int    `json:"encoder_valid_start"`
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
	if len(reqBytes) > maxHelperRequestBytes {
		t.Fatalf("encoded helper request exceeds %d bytes: %d", maxHelperRequestBytes, len(reqBytes))
	}

	cmdPath := helperExecutables.upstream
	if filepath.Base(dir) == "local" {
		cmdPath = helperExecutables.local
	}
	if cmdPath == "" {
		t.Fatalf("helper executable for %s was not initialized", dir)
	}
	ctx, cancel := newHelperContext(t)
	defer cancel()
	stdout, stderr, outputErr, err := runHelperProcess(ctx, cmdPath, reqBytes)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			t.Fatalf("helper %s timed out after %s\nstderr:\n%s", dir, helperTimeout, stderr)
		}
		if errors.Is(outputErr, errHelperOutputTooLarge) {
			t.Fatalf("helper %s response exceeds %d bytes\nstderr:\n%s", dir, maxHelperOutputBytes, stderr)
		}
		t.Fatalf("run helper %s: %v\nstderr:\n%s", dir, err, stderr)
	}
	if errors.Is(outputErr, errHelperOutputTooLarge) {
		t.Fatalf("helper %s response exceeds %d bytes\nstderr:\n%s", dir, maxHelperOutputBytes, stderr)
	}

	var res result
	if err := json.Unmarshal(stdout, &res); err != nil {
		t.Fatalf("unmarshal helper %s stdout: %v\nstdout:\n%s\nstderr:\n%s", dir, err, string(stdout), stderr)
	}
	if res.MarshalOK && !json.Valid([]byte(res.Normalized)) {
		t.Fatalf("helper %s produced invalid JSON: %q", dir, res.Normalized)
	}
	return res
}

func runHelperProcess(ctx context.Context, executable string, input []byte, args ...string) ([]byte, string, error, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.WaitDelay = helperWaitDelay
	cmd.Stdin = bytes.NewReader(input)
	var stdout boundedOutputBuffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.String(), stdout.err, err
}

func newHelperContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(t.Context(), helperTimeout)
}

func assertRawControlParity(t *testing.T, data string, local, upstream result) {
	t.Helper()
	if err := rawControlParityError(t, data, local, upstream); err != nil {
		t.Fatal(err)
	}
}

// rawControlParityError is kept separate from the testing wrapper so the
// raw oracle can be regression-tested against shared corruption.
func rawControlParityError(t *testing.T, data string, local, upstream result) error {
	if local.EncoderValid != upstream.EncoderValid || local.EncoderValidStart != upstream.EncoderValidStart {
		return fmt.Errorf("encoder.Valid(%q) = local (%t,%d), upstream (%t,%d)", data, local.EncoderValid, local.EncoderValidStart, upstream.EncoderValid, upstream.EncoderValidStart)
	}
	wantNormalized := canonicalRawControlNormalization(t, data)
	wantRaw, err := independentRawControlRoot(data)
	if err != nil {
		return fmt.Errorf("derive independent raw-control root: %w", err)
	}
	if !local.Valid || !local.UnmarshalOK || !local.MarshalOK || local.Normalized != wantNormalized {
		return fmt.Errorf("local raw-control result = %+v, want accepted and normalized to %q", local, wantNormalized)
	}
	if !upstream.Valid || !upstream.UnmarshalOK || !upstream.MarshalOK || upstream.Normalized != wantNormalized {
		return fmt.Errorf("upstream raw-control result = %+v, want accepted and normalized to %q", upstream, wantNormalized)
	}
	for _, operation := range []struct {
		name        string
		localOK     bool
		localRaw    string
		upstreamOK  bool
		upstreamRaw string
	}{
		{name: "Get root", localOK: local.GetRootOK, localRaw: local.GetRootRaw, upstreamOK: upstream.GetRootOK, upstreamRaw: upstream.GetRootRaw},
		{name: "Get path", localOK: local.GetPathOK, localRaw: local.GetPathRaw, upstreamOK: upstream.GetPathOK, upstreamRaw: upstream.GetPathRaw},
		{name: "Searcher path", localOK: local.SearcherPathOK, localRaw: local.SearcherPathRaw, upstreamOK: upstream.SearcherPathOK, upstreamRaw: upstream.SearcherPathRaw},
	} {
		if !operation.localOK || !operation.upstreamOK {
			return fmt.Errorf("%s = local ok:%t raw:%q, upstream ok:%t raw:%q; want both successful", operation.name, operation.localOK, operation.localRaw, operation.upstreamOK, operation.upstreamRaw)
		}
		if operation.localRaw != wantRaw || operation.upstreamRaw != wantRaw {
			return fmt.Errorf("%s raw = local %q, upstream %q; want independent raw %q", operation.name, operation.localRaw, operation.upstreamRaw, wantRaw)
		}
	}
	wantNumbers := canonicalPreorderNumbers(t, data)
	if local.PreorderOnlyNumber != wantNumbers || upstream.PreorderOnlyNumber != wantNumbers {
		return fmt.Errorf("Preorder only number = local %q upstream %q, want both %q", local.PreorderOnlyNumber, upstream.PreorderOnlyNumber, wantNumbers)
	}
	if local.NewRawType != upstream.NewRawType {
		return fmt.Errorf("NewRawType mismatch: local %d, upstream %d", local.NewRawType, upstream.NewRawType)
	}
	return nil
}

// independentRawControlRoot derives the first root raw without trusting either
// helper. A same-length copy that replaces raw C0 bytes only in string tokens
// lets encoding/json locate the original root boundary. Invalid UTF-8 in the
// sliced original root then follows encoding/json's U+FFFD replacement rule.
func independentRawControlRoot(data string) (string, error) {
	sanitized := sanitizeRawControlsInStringTokens([]byte(data))
	decoder := json.NewDecoder(bytes.NewReader(sanitized))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return "", err
	}
	start := firstJSONValueOffset([]byte(data))
	end := start + len(raw)
	if start == len(data) || end > len(data) {
		return "", fmt.Errorf("invalid raw range [%d:%d) for %d-byte input", start, end, len(data))
	}
	return replaceInvalidUTF8InStringTokens(data[start:end]), nil
}

func replaceInvalidUTF8InStringTokens(root string) string {
	normalized := make([]byte, 0, len(root))
	inString := false
	escaped := false
	for i := 0; i < len(root); {
		b := root[i]
		if !inString {
			normalized = append(normalized, b)
			i++
			if b == '"' {
				inString = true
			}
			continue
		}
		if escaped {
			normalized = append(normalized, b)
			i++
			escaped = false
			continue
		}
		switch b {
		case '\\':
			normalized = append(normalized, b)
			i++
			escaped = true
		case '"':
			normalized = append(normalized, b)
			i++
			inString = false
		default:
			if b < utf8.RuneSelf {
				normalized = append(normalized, b)
				i++
				continue
			}
			r, size := utf8.DecodeRuneInString(root[i:])
			if r == utf8.RuneError && size == 1 {
				normalized = append(normalized, "�"...)
				i++
				continue
			}
			normalized = append(normalized, root[i:i+size]...)
			i += size
		}
	}
	return string(normalized)
}

func sanitizeRawControlsInStringTokens(data []byte) []byte {
	sanitized := append([]byte(nil), data...)
	inString := false
	escaped := false
	for i, b := range sanitized {
		if !inString {
			if b == '"' {
				inString = true
			}
			continue
		}
		if escaped {
			escaped = false
			continue
		}
		switch b {
		case '\\':
			escaped = true
		case '"':
			inString = false
		default:
			if b <= 0x1f {
				sanitized[i] = 'x'
			}
		}
	}
	return sanitized
}

func firstJSONValueOffset(data []byte) int {
	for i, b := range data {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			return i
		}
	}
	return len(data)
}

// Derive the numeric callback trace using an independent standard decoder.
// Do not hide raw-control Preorder results behind a synthetic error marker.
func canonicalPreorderNumbers(t *testing.T, data string) string {
	t.Helper()
	d := json.NewDecoder(strings.NewReader(escapeRawControlsInStringTokens(data)))
	d.UseNumber()
	var events []string
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if n, ok := token.(json.Number); ok {
			events = append(events, "float64:0:"+string(n))
		}
	}
	out, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func canonicalRawControlNormalization(t *testing.T, data string) string {
	t.Helper()
	var value interface{}
	if err := json.Unmarshal([]byte(escapeRawControlsInStringTokens(data)), &value); err != nil {
		t.Fatalf("unmarshal escaped raw-control input: %v", err)
	}
	var normalized bytes.Buffer
	encoder := json.NewEncoder(&normalized)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		t.Fatalf("encode escaped raw-control input: %v", err)
	}
	return normalized.String()[:normalized.Len()-1]
}

func escapeRawControlsInStringTokens(data string) string {
	normalized := make([]byte, 0, len(data))
	inString := false
	escaped := false
	const hex = "0123456789abcdef"

	for i := 0; i < len(data); i++ {
		b := data[i]
		if !inString {
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
			normalized = append(normalized, '\\', 'u', '0', '0', hex[b>>4], hex[b&0x0f])
		}
	}
	return string(normalized)
}

// hasRawControlInStringToken selects raw-control cases for an independent
// normalization oracle in addition to the native Sonic comparison.
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
		`truX`,
		`nan`,
		`1..2`,
		`[truX]`,
		`{"a":1..2}`,
		`"\u2028"`,
		`"\u2029"`,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data string) {
		if len([]byte(data)) > maxDifferentialInputBytes {
			return
		}
		isRawControl := hasRawControlInStringToken(data)
		req := request{
			Data: base64.StdEncoding.EncodeToString([]byte(data)),
			Path: pathFromSeed(data),
		}
		if isRawControl {
			req.Path = nil
		}

		local := runHelper(t, filepath.Join("local"), req)
		upstream := runHelper(t, filepath.Join("upstream"), req)
		if isRawControl {
			assertRawControlParity(t, data, local, upstream)
			return
		}
		if !reflect.DeepEqual(local, upstream) {
			t.Fatalf("sonic parity mismatch\ndata: %q\npath: %+v\nlocal: %+v\nupstream: %+v", data, req.Path, local, upstream)
		}
	})
}

func TestEncoderValidCursorParity(t *testing.T) {
	ctx, cancel := newHelperContext(t)
	defer cancel()
	info, stderr, _, err := runHelperProcess(ctx, helperExecutables.upstream, nil, "--info")
	if err != nil {
		t.Fatalf("upstream helper identity: %v: %s", err, stderr)
	}
	var native struct {
		GoVersion string `json:"go_version"`
		APIKind   int    `json:"api_kind"`
	}
	if err := json.Unmarshal(info, &native); err != nil || native.GoVersion != "go1.26.7" || native.APIKind != 1 {
		t.Fatalf("expected native Sonic on Go 1.26.7 (APIKind=1), got %s: %v", info, err)
	}
	for _, data := range []string{
		"", " ", "\n  ", "true", " false ", "  null \t", "0", "-123.45e+6", "[]", "{}", " [true] ",
		"truX", "trX", "falX", "nulX", "nan", "fax", "t", "tru", "n", "nul", "falsee",
		"1..2", "1.x", "1.", "1e", "1e+", "1e+x", "-", "-x", "01", "1x", "1 2", "true false",
		"[truX]", "[falX]", "[nan]", "[1..2]", "[1e+]", "[1,]", "[1 x]", "[x]", "[true, false,]",
		`{"a":}`, `{"a":truX}`, `{"a":nan}`, `{"a":1..2}`, `{"a":1e+}`, `{"a" 1}`, `{"a":1,}`, `{"a":1 x}`,
		" \n [\ttruX]", " [0, {\"x\": 1..2}]", "\x00", "[\x00]", "\"unterminated", "{", "[",
		"tXxx", "txxx", "trux", "trueX", "naxx", "nulx", "fXxxx", "faXxx", "falXx", "falseX",
		"tX", "tx", "nx", "fx", "fa", "fal", "fals", "nux", "nullx", "\t nan ", "\t fax ",
		"1.x2", "1.2.3", "1e+x", "1e-x", "1e1.2", "1e1e2", "1e1+2", "1e2-3", "1.2e+3.4", "1.2e-", "1e-", "1e- ",
		"-.", "-1..2", "-1.2.3", "-1e+", "-1e+x", "1e+e", "1e++", "1e+1e", "1e+1+", "1.e1", "1. e", "12.3.4", "12e2e", "12e+e",
		"[ 1..2 ]", "[ -1..2 ]", "{\"x\": 1e+2.3}", "[1,2..3]", "{\"x\": \t nux}",
		"4E8.9918E915..37", "88e14877E206.+9E35e+057506E1++.9-474e-..+76-82-+8E+28.e21",
		`{"x":[0,268121.2+E+29.+.]}`,
	} {
		req := request{Data: base64.StdEncoding.EncodeToString([]byte(data))}
		local := runHelper(t, "local", req)
		upstream := runHelper(t, "upstream", req)
		if local.EncoderValid != upstream.EncoderValid || local.EncoderValidStart != upstream.EncoderValidStart {
			t.Errorf("encoder.Valid(%q) = local (%t,%d), upstream (%t,%d)", data, local.EncoderValid, local.EncoderValidStart, upstream.EncoderValid, upstream.EncoderValidStart)
		}
	}
}

func TestEncoderValidReviewRegression(t *testing.T) {
	for _, tt := range []struct {
		name string
		data string
		want int
	}{
		{name: "missing object colon", data: `{"a"}`},
		{name: "nesting 4095", data: strings.Repeat("[", 4095) + "1..2" + strings.Repeat("]", 4095)},
		{name: "nesting 4096", data: strings.Repeat("[", 4096) + "1..2" + strings.Repeat("]", 4096)},
		{name: "nesting 4097", data: strings.Repeat("[", 4097) + "1..2" + strings.Repeat("]", 4097)},
		{name: "array depth limit nested array", data: strings.Repeat("[", 4096) + "[]" + strings.Repeat("]", 4096), want: 4096},
		{name: "object depth limit scalar", data: strings.Repeat(`{"a":`, 4096) + "0" + strings.Repeat("}", 4096), want: 20478},
		{name: "object depth limit invalid number", data: strings.Repeat(`{"a":`, 4096) + "1..2" + strings.Repeat("}", 4096), want: 20478},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := request{Data: base64.StdEncoding.EncodeToString([]byte(tt.data))}
			local := runHelper(t, "local", req)
			upstream := runHelper(t, "upstream", req)
			if local.EncoderValid != upstream.EncoderValid || local.EncoderValidStart != upstream.EncoderValidStart {
				t.Errorf("encoder.Valid(%s, bytes=%d) local=(%t,%d) upstream=(%t,%d)", tt.name, len(tt.data), local.EncoderValid, local.EncoderValidStart, upstream.EncoderValid, upstream.EncoderValidStart)
			}
			if tt.want != 0 && (upstream.EncoderValid || upstream.EncoderValidStart != tt.want) {
				t.Errorf("native encoder.Valid(%s) = (%t,%d), want (false,%d)", tt.name, upstream.EncoderValid, upstream.EncoderValidStart, tt.want)
			}
		})
	}
	data := "1ee" + strings.Repeat("1", 13) + ".." + strings.Repeat("1", 14)
	for _, tt := range []struct {
		mode string
		want int
	}{{"auto", 16}, {"noavx", 1}, {"noavx2", 1}} {
		t.Run(tt.mode, func(t *testing.T) {
			t.Setenv("SONIC_MODE", tt.mode)
			req := request{Data: base64.StdEncoding.EncodeToString([]byte(data))}
			local := runHelper(t, "local", req)
			upstream := runHelper(t, "upstream", req)
			if local.EncoderValid != upstream.EncoderValid || local.EncoderValidStart != upstream.EncoderValidStart {
				t.Errorf("SONIC_MODE=%s encoder.Valid(%q) local=(%t,%d) upstream=(%t,%d)", tt.mode, data, local.EncoderValid, local.EncoderValidStart, upstream.EncoderValid, upstream.EncoderValidStart)
			}
			if upstream.EncoderValid || upstream.EncoderValidStart != tt.want {
				t.Errorf("SONIC_MODE=%s native encoder.Valid=(%t,%d), want (false,%d)", tt.mode, upstream.EncoderValid, upstream.EncoderValidStart, tt.want)
			}
		})
	}
}

func TestEncoderValidRandomCursorParity(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	const alphabet = "0123456789-.eE+tnrufals[]{}:, \t\r\n\"\\\x00\xff"
	const numberAlphabet = "0123456789.eE+-"
	for i := 0; i < 510; i++ {
		chars := alphabet
		size := rng.Intn(40) + 1
		if i%3 != 0 {
			chars = numberAlphabet
			size = rng.Intn(15) + 2
		}
		if i >= 450 {
			chars = numberAlphabet
			size = rng.Intn(48) + 32
		}
		data := make([]byte, size)
		for j := range data {
			data[j] = chars[rng.Intn(len(chars))]
		}
		if i%3 == 2 {
			data = append([]byte(`{"x":[0,`), data...)
			data = append(data, ']', '}')
		}
		req := request{Data: base64.StdEncoding.EncodeToString(data)}
		local := runHelper(t, "local", req)
		upstream := runHelper(t, "upstream", req)
		if local.EncoderValid != upstream.EncoderValid || local.EncoderValidStart != upstream.EncoderValidStart {
			t.Errorf("encoder.Valid(%q) = local (%t,%d), upstream (%t,%d)", data, local.EncoderValid, local.EncoderValidStart, upstream.EncoderValid, upstream.EncoderValidStart)
		}
	}
}

func TestRawControlParity(t *testing.T) {
	data := string([]byte{'{', '"', 0x11, 'x', '"', ':', '1', '}'})
	req := request{Data: base64.StdEncoding.EncodeToString([]byte(data))}
	local := runHelper(t, filepath.Join("local"), req)
	upstream := runHelper(t, filepath.Join("upstream"), req)

	assertRawControlParity(t, data, local, upstream)
	if local.Normalized != `{"\u0011x":1}` {
		t.Fatalf("local normalization = %q, want %q", local.Normalized, `{"\u0011x":1}`)
	}
	for _, helper := range []struct {
		name string
		res  result
	}{
		{name: "local", res: local},
		{name: "upstream", res: upstream},
	} {
		if helper.res.GetRootRaw != data || helper.res.GetPathRaw != data || helper.res.SearcherPathRaw != data {
			t.Fatalf("%s raw results = root:%q path:%q searcher:%q, want all %q", helper.name, helper.res.GetRootRaw, helper.res.GetPathRaw, helper.res.SearcherPathRaw, data)
		}
		if helper.res.NewRawType != 6 {
			t.Fatalf("%s NewRawType = %d, want 6", helper.name, helper.res.NewRawType)
		}
	}
}

func TestRawControlParityRejectsSharedCorruption(t *testing.T) {
	data := string([]byte{'{', '"', 0x11, 'x', '"', ':', '1', '}'})
	corrupted := `{"\u0011x":1}`
	wantNormalized := canonicalRawControlNormalization(t, data)
	local := result{
		Valid:              true,
		UnmarshalOK:        true,
		MarshalOK:          true,
		Normalized:         wantNormalized,
		GetRootOK:          true,
		GetRootRaw:         corrupted,
		GetPathOK:          true,
		GetPathRaw:         corrupted,
		SearcherPathOK:     true,
		SearcherPathRaw:    corrupted,
		PreorderOnlyNumber: canonicalPreorderNumbers(t, data),
		NewRawType:         6,
	}
	upstream := local

	if err := rawControlParityError(t, data, local, upstream); err == nil {
		t.Fatal("raw-control validator accepted identical corrupted raw values")
	}
}

func TestRawControlParityGeneralizes(t *testing.T) {
	for _, tt := range []struct {
		name           string
		data           string
		wantNormalized string
		wantRaw        string
		wantNewRawType int
	}{
		{
			name:           "string root",
			data:           string([]byte{'"', 0x11, 'x', '"'}),
			wantNormalized: `"\u0011x"`,
			wantNewRawType: 7,
		},
		{
			name:           "array string value",
			data:           string([]byte{'[', '"', 0x00, '"', ',', '1', ']'}),
			wantNormalized: `["\u0000",1]`,
			wantNewRawType: 5,
		},
		{
			name:           "object value with outer whitespace",
			data:           string([]byte{' ', '\t', '\n', '{', '"', 'x', '"', ':', '"', 0x11, '"', '}', ' ', '\r'}),
			wantNormalized: `{"x":"\u0011"}`,
		},
		{
			name:           "object key with raw control and invalid UTF-8",
			data:           string([]byte{'{', '"', 0x11, 0xff, '"', ':', '1', '}'}),
			wantNormalized: "{\"\\u0011�\":1}",
			wantRaw:        "{\"\x11�\":1}",
			wantNewRawType: 6,
		},
		{
			name:           "object key with raw control and HTML punctuation",
			data:           string([]byte{'{', '"', 0x11, '<', '"', ':', '"', '&', '>', '"', '}'}),
			wantNormalized: `{"\u0011<":"&>"}`,
			wantRaw:        string([]byte{'{', '"', 0x11, '<', '"', ':', '"', '&', '>', '"', '}'}),
			wantNewRawType: 6,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := request{Data: base64.StdEncoding.EncodeToString([]byte(tt.data))}
			local := runHelper(t, filepath.Join("local"), req)
			upstream := runHelper(t, filepath.Join("upstream"), req)

			assertRawControlParity(t, tt.data, local, upstream)
			if local.Normalized != tt.wantNormalized {
				t.Fatalf("local normalization = %q, want %q", local.Normalized, tt.wantNormalized)
			}
			if tt.wantRaw != "" {
				for _, helper := range []struct {
					name string
					res  result
				}{
					{name: "local", res: local},
					{name: "upstream", res: upstream},
				} {
					if helper.res.GetRootRaw != tt.wantRaw || helper.res.GetPathRaw != tt.wantRaw || helper.res.SearcherPathRaw != tt.wantRaw {
						t.Fatalf("%s raw results = root:%q path:%q searcher:%q, want all %q", helper.name, helper.res.GetRootRaw, helper.res.GetPathRaw, helper.res.SearcherPathRaw, tt.wantRaw)
					}
				}
			}
			if tt.wantNewRawType != 0 && local.NewRawType != tt.wantNewRawType {
				t.Fatalf("local NewRawType = %d, want %d", local.NewRawType, tt.wantNewRawType)
			}
		})
	}
}

func TestHelperContextDeadline(t *testing.T) {
	before := time.Now()
	ctx, cancel := newHelperContext(t)
	defer cancel()
	after := time.Now()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("helper context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining > helperTimeout || deadline.After(after.Add(helperTimeout)) {
		t.Fatalf("helper context deadline exceeds timeout: remaining=%s timeout=%s", remaining, helperTimeout)
	}
	if deadline.Before(before.Add(helperTimeout - time.Second)) {
		t.Fatalf("helper context deadline is not close to timeout: remaining=%s timeout=%s", remaining, helperTimeout)
	}
}

func TestHelperTimeoutKillsHangingDirectProcess(t *testing.T) {
	if os.Getenv(helperHangChildEnv) == "1" {
		for {
			runtime.Gosched()
		}
	}

	t.Setenv(helperHangChildEnv, "1")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	stdout, stderr, outputErr, err := runHelperProcess(ctx, os.Args[0], nil, "-test.run=^TestHelperTimeoutKillsHangingDirectProcess$")
	elapsed := time.Since(started)
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("hanging direct process context = %v, want DeadlineExceeded; process error = %v; output error = %v; stdout = %q; stderr = %q", ctx.Err(), err, outputErr, stdout, stderr)
	}
	if err == nil {
		t.Fatal("hanging direct process exited without an error")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("hanging direct process took %s, want bounded termination within 2s", elapsed)
	}
}

func TestRemoveAllWithRetryWaitsForLockedExecutable(t *testing.T) {
	if readyFile := os.Getenv(helperCleanupHangChildEnv); readyFile != "" {
		if err := os.WriteFile(readyFile, nil, 0o600); err != nil {
			t.Fatalf("signal cleanup child readiness: %v", err)
		}
		for {
			runtime.Gosched()
		}
	}
	if runtime.GOOS != "windows" {
		t.Skip("Windows keeps a running executable delete-locked")
	}

	tempDir := t.TempDir()
	lockedExecutable := filepath.Join(tempDir, "cleanup-lock.exe")
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatalf("find current test executable: %v", err)
	}
	contents, err := os.ReadFile(testExecutable)
	if err != nil {
		t.Fatalf("read current test executable: %v", err)
	}
	if err := os.WriteFile(lockedExecutable, contents, 0o755); err != nil {
		t.Fatalf("copy current test executable: %v", err)
	}

	readyFile := filepath.Join(tempDir, "cleanup-child-ready")
	child := exec.Command(lockedExecutable, "-test.run=^TestRemoveAllWithRetryWaitsForLockedExecutable$")
	child.Env = append(os.Environ(), helperCleanupHangChildEnv+"="+readyFile)
	if err := child.Start(); err != nil {
		t.Fatalf("start cleanup child: %v", err)
	}
	childRunning := true
	t.Cleanup(func() {
		if childRunning {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
		_ = os.RemoveAll(tempDir)
	})

	readyDeadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(readyFile); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatalf("check cleanup child readiness: %v", err)
		}
		if time.Now().After(readyDeadline) {
			t.Fatal("cleanup child did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cleanupDone := make(chan error, 1)
	go func() {
		cleanupDone <- removeAllWithRetry(tempDir)
	}()
	select {
	case err := <-cleanupDone:
		t.Fatalf("cleanup returned before the locked executable exited: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	if err := child.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("terminate cleanup child: %v", err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("cleanup child exited without termination")
	}
	childRunning = false

	select {
	case err := <-cleanupDone:
		if err != nil {
			t.Fatalf("cleanup after child exit: %v", err)
		}
	case <-time.After(helperCleanupTimeout + time.Second):
		t.Fatalf("cleanup did not finish within %s after child exit", helperCleanupTimeout+time.Second)
	}
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Fatalf("helper temp directory still exists after cleanup: %v", err)
	}
}

func TestBoundedOutput(t *testing.T) {
	var output boundedOutputBuffer
	accepted := make([]byte, maxHelperOutputBytes)
	if n, err := output.Write(accepted); err != nil || n != len(accepted) {
		t.Fatalf("bounded output exact limit Write = (%d, %v), want (%d, nil)", n, err, len(accepted))
	}
	if n, err := output.Write([]byte{'x'}); n != 0 || !errors.Is(err, errHelperOutputTooLarge) {
		t.Fatalf("bounded output overflow Write = (%d, %v), want (0, errHelperOutputTooLarge)", n, err)
	}
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
		{name: "root bare exponent followed by space", data: "1e "},
		{name: "root bare exponent followed by comma", data: "1e,"},
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
