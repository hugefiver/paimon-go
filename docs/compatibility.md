# Compatibility Notes

This document records behavior retained for source compatibility with Sonic `v1.15.2`. Setup, the original import path, and root build-tag selection are documented in the root README.

## JSON-v2 number modes

`UseNumber` and `UseInt64` number modes are implemented in JSON-v2. They preserve `CaseSensitive` matching and unknown-field rejection, replace scalar results, merge duplicate JSON-v2 object values, preserve pre-populated concrete interface values, accept invalid UTF-8 with replacement, call a custom unmarshaler once, and work through streams. When both flags are set, `UseNumber` takes precedence.

## Raw lookup and concurrent reads

In Sonic-compatible mode, Searcher validation applies only to the selected value. `CopyReturn` clones only that selected raw substring. `ConcurrentRead` protects only the pointer-receiver read APIs `TypeSafe`, `Exists`, `Valid`, `Check`, `Bool`, `StrictBool`, `Int64`, `StrictInt64`, `Float64`, `StrictFloat64`, `Number`, `StrictNumber`, `String`, `StrictString`, `Len`, `Cap`, `Get`, `Index`, `GetByPath`, `IndexOrGet`, `IndexOrGetWithIdx`, `IndexPair`, `ForEach`, `Values`, `Properties`, `Interface`, `InterfaceUseNumber`, `InterfaceUseNode`, `Array`, `ArrayUseNumber`, `ArrayUseNode`, `Map`, `MapUseNumber`, `MapUseNode`, `Raw`, and `MarshalJSON` during first lazy materialization without concurrent mutation. `Type`, `IsRaw`, and `Error` are value-receiver APIs that are not concurrent safe. `ConcurrentRead` does not make `Load`, `LoadAll`, `UnmarshalJSON`, or any mutation safe. The default root `Get` path allocates neither a mutex nor a clone.

Default root `Valid` accepts raw control bytes in strings, `Unmarshal` normalizes them before the reflection fallback, and empty-path `Get` returns the first complete value while ignoring trailing data. This local `Valid`/`Unmarshal` acceptance is an intentional divergence from the Go 1.27 upstream Sonic fallback, while both implementations' raw AST entry points agree on the raw value. `sonic_stdjson` instead uses strict standard-JSON validation for these raw entry points.

## AST and decoder behavior

`VisitOPSkip` is consumed only when it is returned directly from an object- or array-begin callback; wrapped sentinels and all other callbacks propagate their errors. Node `V_ANY` construction and mutation, `SortKeys`, object-only `IndexOrGetWithIdx`, and zero-node behavior match the compatibility witnesses.

A nil `NoCopyRawMessage` marshals as `null`; a nil `*NoCopyRawMessage` passed to `UnmarshalJSON` returns `sonic.NoCopyRawMessage: UnmarshalJSON on nil pointer`; a non-nil value retains the input bytes without copying. `decoder.SyntaxError` and `MismatchTypeError` provide Sonic-style source, position, caret, value-kind, and quoted syntax-error descriptions while retaining their public field superset.

## Retained safer differences

- Malformed `Node.UnmarshalJSON` leaves an error node and does not panic when it is later marshaled.
- A constructed string containing invalid UTF-8 serializes as valid JSON.
- A malformed `Parser` container can return its container-shaped node and defer the error to loading.
- `int64` and `json.Number` path elements use extended, non-panicking handling; unsupported values return an error.

## Subpackages and scope

`github.com/bytedance/sonic/fastjson` keeps exported aliases for root types. Package-level encode, decode, and validation helpers use the separately assignable `fastjson.ConfigDefault`; `Get*` and `Pretouch*` forward to the root package.

Scanner rewrites, native/JIT implementation, default-backend changes, and dependency upgrades are out of scope. `github.com/bytedance/sonic/loader` is not implemented.
