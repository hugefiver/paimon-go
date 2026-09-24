# Compatibility Notes

The reference is Sonic **v1.15.2's native implementation**, tested on Go 1.26.7/amd64. This repository runs on Go 1.27. Sonic v1.15.2 selects its standard-library fallback on Go 1.27; comparing both modules under that toolchain does not test native compatibility. The differential harness therefore builds upstream separately and requires `APIKind == UseSonicJSON`.

The public root, encoder, decoder, AST, option, unquote and utf8 APIs are implemented in pure Go. Native/JIT performance, loader internals and complete behavioral equivalence for every advanced configuration are not promised. The supported contracts and remaining boundaries are listed below.

## Number modes and ordinary values

`UseNumber` preserves JSON number tokens as `json.Number`. `UseInt64` produces `int64` for representable integer tokens, otherwise `float64`; fractional/exponent forms use `float64`, and non-finite overflow is an error. Conversion happens during decoding rather than walking the destination afterward. Omitted fields, `json:"-"`, pre-existing reference cycles outside the input, and values produced by custom unmarshalers are not rewritten.

A configuration can be frozen and used for encoding with both number flags, but `Unmarshal`, `UnmarshalFromString`, and `NewDecoder` reject the conflicting modes by panicking, matching native Sonic. Changing a decoder to `UseNumber` clears its `UseInt64` mode.

The JSON-v2 backend uses legacy representation and merge options for ordinary Go values: zero `omitempty` fields, numeric byte arrays, `null` in scalar fields, duplicate object values, field matching, and pointer-method dispatch follow the native compatibility witnesses. Its number callbacks consume validated tokens directly and preserve non-nil concrete pointers and custom unmarshaler results.

## Configuration support

| Configuration | default / `sonic_stdjson` | `sonic_jsonv2` |
|---|---|---|
| `EscapeHTML` | Supported; standard encoder also escapes U+2028/U+2029 | Supported |
| `SortMapKeys` | Map keys are always sorted | Selects deterministic ordering |
| `UseNumber`, `UseInt64` | Supported | Supported |
| `DisallowUnknownFields` | Supported; error/partial-result details may follow stdlib outside the `UseInt64` path | Supported; stops at the error |
| `NoEncoderNewline` | Supported | Supported |
| `NoNullSliceOrMap` | Accepted but has no effect | Supported |
| `CaseSensitive` | Accepted but has no effect | Supported |
| `CopyString` | Strings are owned regardless of the flag | Strings are owned regardless of the flag |
| `CompactMarshaler` | Custom JSON is always compacted | Custom JSON is always compacted |
| `NoQuoteTextMarshaler`, `UseUnicodeErrors`, `ValidateString`, `NoValidateJSONMarshaler`, `NoValidateJSONSkip`, `EncodeNullForInfOrNan` | Accepted; exact native option semantics are not implemented | Accepted; exact native option semantics are not implemented |

These limitations also apply to the corresponding encoder/decoder option bits where applicable. `Pretouch` and `PretouchMany` are no-ops. `APIKind` identifies this replacement API, not a native/JIT implementation.

## Raw lookup and concurrent reads

In Sonic-compatible mode, Searcher validation applies only to the selected value. `CopyReturn` clones only that selected raw substring. String lookup avoids copying the entire document. `GetCopyFromString` validates the selected value, like native Sonic, and owns its raw bytes.

`ConcurrentRead` protects only the pointer-receiver read APIs `TypeSafe`, `Exists`, `Valid`, `Check`, `Bool`, `StrictBool`, `Int64`, `StrictInt64`, `Float64`, `StrictFloat64`, `Number`, `StrictNumber`, `String`, `StrictString`, `Len`, `Cap`, `Get`, `Index`, `GetByPath`, `IndexOrGet`, `IndexOrGetWithIdx`, `IndexPair`, `ForEach`, `Values`, `Properties`, `Interface`, `InterfaceUseNumber`, `InterfaceUseNode`, `Array`, `ArrayUseNumber`, `ArrayUseNode`, `Map`, `MapUseNumber`, `MapUseNode`, `Raw`, and `MarshalJSON` during first lazy materialization without concurrent mutation. `Type`, `IsRaw`, and `Error` are value-receiver APIs that are not concurrent safe. `ConcurrentRead` does not make `Load`, `LoadAll`, `UnmarshalJSON`, or any mutation safe.

Default `Valid` accepts raw controls and unknown escape sequences inside structurally closed string tokens, as native Sonic's validator does. `Unmarshal` still rejects unknown escapes, and normalizes raw controls before standard-library decoding. Empty-path `Get` returns the first value and ignores trailing input. A leading zero terminates a selected numeric value (`Get({"a":01}, "a")` returns `0`), but a container containing that malformed member fails validation. Incomplete exponents are rejected.

`sonic_stdjson` and `sonic_jsonv2` intentionally validate complete documents for their root raw lookup/validation APIs and reject raw string controls and trailing data. AST APIs keep their Sonic behavior independently of the root backend.

## AST, quoting and streaming

Child storage has stable addresses across adding members and removing siblings. Deleted/absent `V_NONE` members are skipped by serialization and iteration. Parser failures retain the complete source and absolute cursor. `Preorder` supplies the native capacity hint of 16 without scanning each subtree in advance.

`Load` and `LoadAll` are aliases, as in native v1.15.2: they load the current container's members while retaining raw container children. Ordinary `Get`/`Index` load only as far as necessary. Node value copies retain independent parsing cursors; already materialized child values remain shallowly shared.

`VisitOPSkip` is consumed only when returned directly from an object/array begin callback. Wrapped sentinels and other callback errors propagate.

`encoder.Quote` emits JSON escapes and follows native byte-preserving string behavior. `unquote` accepts raw control/UTF-8 bytes and replaces unpaired Unicode surrogates. `unquote.IntoBytes` requires capacity for the source length and does not grow the destination.

`decoder.StreamDecoder` exposes the embedded Decoder's public methods. Streaming encode buffers complete values, retries short writes, and reuses its buffer; encoding failures do not publish partial output. Streaming decode remembers errors and does not resume into the next value after a failure.

A nil `NoCopyRawMessage` marshals as `null`; a nil `*NoCopyRawMessage` passed to `UnmarshalJSON` returns `sonic.NoCopyRawMessage: UnmarshalJSON on nil pointer`; a non-nil value retains input bytes without copying. Streaming decoders detach buffers for destinations that may contain this type so later reads cannot overwrite earlier values. Interface-containing destinations conservatively take this ownership path, trading some decoding throughput for correct retained data.

## Remaining behavioral boundaries

- Codec errors may be concrete `encoding/json` or `strconv` errors rather than Sonic's native error types. Malformed JSON error wording and some error-position categories may still differ. Returning an error does not guarantee identical partially populated destinations for every backend.
- JSON-v2 retains its fail-fast handling for ordinary type mismatches; native Sonic can continue populating later fields. Its `UnmarshalJSON` combined with `,string`, invalid JSON tag names, and `null` through interfaces holding multiple pointer levels can differ from native Sonic. These are not covered by the ordinary-value equivalence claim.
- The reflection fallback retains its existing dispatch for prefilled nonempty maps: a custom pointer stored in a `map[string]any` can be replaced where native Sonic would call it. Invalid UTF-8 follows the standard-library replacement rules, including possible key collisions after replacement.
- Allocation of anonymous pointers to unexported embedded types follows the backend's safer reflection restrictions.
- Custom `MarshalJSON` whitespace is compacted even when native Sonic would preserve it; validation-skipping and other advanced encoder options remain limited as shown above.
- Malformed `Node.UnmarshalJSON` leaves an error node rather than reproducing an upstream panic. Constructed AST strings with invalid UTF-8 serialize as valid JSON. Some malformed parser containers defer errors to loading.
- `unquote.IntoBytes` with a nil destination pointer returns a safe error instead of reproducing the native panic.
- Multi-interface pointer cycles return an error where native Sonic itself can overflow its stack. Concurrent mutation is unsupported.
- `int64` and `json.Number` path elements use extended, non-panicking handling; unsupported values return an error.

## Subpackages and validation

`github.com/bytedance/sonic/fastjson` keeps exported aliases for root types. Package-level encode, decode, and validation helpers use the separately assignable `fastjson.ConfigDefault`; `Get*` and `Pretouch*` forward to the root package. `github.com/bytedance/sonic/loader` is not implemented.

The differential module compiles the same consumer against native upstream and all three local modes, then compares typed results, number modes, string/byte representations, callback behavior, streaming and AST mutations. Its raw fuzz harness compares actual parser outputs; it does not substitute synthetic visitor errors. CI runs this separate module explicitly, in addition to root tests and the race detector. See [difftest/README.md](../difftest/README.md) and [performance results](performance.md).
