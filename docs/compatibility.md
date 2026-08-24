# Compatibility Notes

This module is a source-compatible replacement for
`github.com/bytedance/sonic` targeting Sonic `v1.15.2`. Consumers should keep
their existing Sonic imports and redirect the module with a `go.mod` replace:

```go
require github.com/bytedance/sonic v1.15.2

replace github.com/bytedance/sonic => <local path>
```

After replacing, code importing `github.com/bytedance/sonic` and the covered
subpackages continues to compile against the same public API surface, subject
to the behavioral differences below.

## API coverage and scope

The compatibility target is Sonic `v1.15.2` for these packages and surfaces:

- root package: `Config`, `API`, package-level marshal/unmarshal helpers,
  `Valid`, `Get`, `NoCopyRawMessage`, `Pretouch`, encoders, and decoders;
- `ast`: node wrappers, parser/searcher helpers, visitors, iterator support,
  raw JSON/path behavior, and parse-error types used by the public API;
- `encoder` and `decoder`: compatibility wrappers around the root stream
  types and configuration entry points;
- `option`: compile-option symbols accepted by Sonic APIs such as `Pretouch`;
- `unquote` and `utf8`: exported helpers used by Sonic-compatible callers;
- `fastjson`: explicit thin wrapper subpackage over the root backend;
- `stdjsonv2`: experimental backend subpackage with a real implementation
  selected by the Go 1.27 default experiment set or explicit `jsonv2`, and a
  deterministic disabled stub under explicit `none`.

`github.com/bytedance/sonic/loader` is intentionally out of scope and is not
implemented.

## Backend model

### Root / default backend

The root package chooses raw JSON behavior per feature according to the project
policy: support for standard JSON must not be worse than upstream Sonic; when
upstream Sonic accepts non-standard JSON, default to the faster or better local
strategy and provide a build tag for forcing strict standard behavior when
needed.

- `Valid`, `ValidString`, `Get`, `GetFromString`, `GetCopyFromString`,
  and `GetWithOptions` default to the observed Sonic/fastjson-style raw parser
  behavior because that implementation is faster than the strict standard path
  for the current hot-path benchmarks while still accepting standard JSON.
- Build with `-tags sonic_stdjson` to force strict standard JSON behavior for
  these raw JSON entry points.
- AST parsing and raw JSON nodes remain Sonic-shaped APIs and may still use
  `github.com/valyala/fastjson`-oriented helpers internally.
- `Marshal`, `MarshalString`, `MarshalIndent`, `Unmarshal`,
  `UnmarshalString`, `NewEncoder`, and `NewDecoder` for arbitrary Go values
  use the standard `encoding/json` v1 reflection fallback.

This preserves the most commonly used Sonic API surface without porting
Sonic's native/JIT implementation. It also means struct-level reflection
behavior follows `encoding/json` v1 more closely than upstream Sonic.

### `fastjson` subpackage

`github.com/bytedance/sonic/fastjson` is a thin wrapper around the root
package. Its exported types are aliases of root package types, and its
functions forward to root functions. It exists so source code that explicitly
imports Sonic's `fastjson` path continues to compile and sees the same
behavior as the root/default backend.

### `stdjsonv2` subpackage

`github.com/bytedance/sonic/stdjsonv2` is opt-in and experimental.

- The Go 1.27 default experiment set and explicit
  `$env:GOEXPERIMENT = "jsonv2"` select the real backend, which requires
  `encoding/json/v2` and `encoding/json/jsontext`.
- Explicit `$env:GOEXPERIMENT = "none"` selects the deterministic disabled
  stub: operational APIs return `ErrJSONv2ExperimentDisabled`; `Valid*`
  returns `false`; stream APIs are present but fail/no-op consistently.

## Known behavioral differences

### Raw JSON validation and lookup

- Root `Valid` accepts raw control bytes inside strings by default, root
  `Unmarshal` normalizes those raw control bytes before the `encoding/json`
  fallback, and empty-path root `Get` returns the first complete JSON value while
  ignoring trailing garbage. This matches observed upstream Sonic behavior for
  these hot paths.
- Build with `-tags sonic_stdjson` to force strict `encoding/json`-style
  behavior. Under that tag, root `Valid`, `Unmarshal`, and `Get` reject inputs
  that `encoding/json` rejects, including raw control bytes inside strings and
  trailing garbage after the top-level value.

### AST parsing errors

`ast.Parser.Parse` maps parser failures onto Sonic's public parsing-error
shape and numeric code space on a best-effort basis. The error type and codes
are intended for source compatibility, but exact wording and every edge-case
classification may differ from upstream Sonic's native parser.

`ast.Preorder`, like Sonic, stops after the root value and ignores trailing
data. For every valid JSON number, `VisitorOptions.OnlyNumber` calls
`OnFloat64(0, raw json.Number)`, preserving the raw number and never calling
`OnInt64`. Container callback capacities differ intentionally: this
implementation supplies a content estimate, whereas upstream Sonic uses a
fixed capacity of 16 for non-empty containers; callers must treat capacity as
a preallocation hint.

Unpaired UTF-16 surrogate escapes decode to U+FFFD (Sonic semantics) and
leading-zero number literals such as `0123` round-trip verbatim. Nesting is
accepted up to Sonic's MAX_RECURSE limit (4096 levels).

Unloaded `ast.NewRaw` nodes report the same concrete root `Type()` values as
Sonic. `decoder.Skip` also aligns its negative error start code and diagnostic
cursor with Sonic for malformed input. On the reflection fallback, a root or
`stdjsonv2` API configured with both `UseNumber` and `UseInt64` follows Sonic
v1.15.2's Go 1.27 fallback behavior: `UseNumber` takes precedence, so numbers
decoded into interfaces are `json.Number` values. The `decoder.Decoder`
bitmask API is distinct: `SetOptions` with both option bits panics, matching
the upstream decoder package.

### Stream encoding

Stream encoders retry short writes until the encoded output is fully written.
If a writer makes no progress, the encoder returns `io.ErrShortWrite`.

### `encoding/json` fallback limits

The root/default reflection fallback accepts Sonic configuration fields, but
some flags are only partially implemented or necessarily follow
`encoding/json` v1 behavior. Examples include advanced Sonic-only validation,
case matching, marshaler, string-copying, and native codec behaviors that
depend on Sonic's JIT engine. Unsupported or partially supported flags are
accepted for source compatibility rather than rejected.

These advanced configuration differences remain a configuration gap and were
not expanded in this compatibility pass. This document does not claim complete
Sonic configuration compatibility or bug-free equivalence.

### `stdjsonv2` limits

The real `stdjsonv2` backend requires a toolchain that exposes both
`encoding/json/v2` and `encoding/json/jsontext` and an effective experiment set
that includes jsonv2. Known limitations include:

- `NoEncoderNewline` is honored for arbitrary writers by buffering each value
  and writing it with short-write retry.
- Standard JSON documents with duplicate object names or invalid UTF-8
  (valid per RFC 8259) are accepted, matching the root backend.
- Several Sonic configuration flags are accepted but not fully mapped to
  jsonv2/jsontext behavior, especially flags tied to Sonic's native codec or
  unsupported jsonv2 knobs.
- The disabled stub is an intentional explicit-`GOEXPERIMENT=none` path, not a
  runtime failure of the root package.

### `unquote.IntoBytes` limits

`unquote.IntoBytes` has two known differences from upstream Sonic:

- a nil destination is reported as `ERR_UNSUPPORT_TYPE`;
- Sonic's destination-capacity gate is not implemented, so capacity-sensitive
  edge cases may not match byte-for-byte upstream behavior.

## Performance and native internals

Performance parity is not promised. Upstream Sonic uses native/JIT codecs;
this replacement delegates raw JSON operations to fastjson-oriented helpers and
general Go value reflection to `encoding/json` v1 (or jsonv2 in the explicit
experimental subpackage). The `bench/` module provides reproducible comparison
commands only.

Only the minimal `internal/native/types` symbols needed by exported signatures
are reproduced. Sonic's native parser, decoder, encoder, loader, and assembly
fp-conversion internals are not ported.
