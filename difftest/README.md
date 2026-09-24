# Native Sonic differential tests

This independent module compares the local replacement with upstream Sonic v1.15.2. The local and upstream implementations share a module path, so they are built as separate helper executables using `go build -mod=readonly`.

The local helper uses the repository's Go 1.27 toolchain. The upstream helper uses **Go 1.26.7** by default; Go 1.27 would silently select upstream's standard-library fallback. Each helper exposes its Go version and `APIKind`, and the harness rejects the fallback. `SONIC_DIFFTEST_UPSTREAM_TOOLCHAIN` can select another supported native toolchain. Builds may download missing modules/toolchains unless `GOPROXY=off` is set.

The helpers are built once in an owned temporary directory and then invoked directly. Requests contain base64 JSON input, avoiding command-line encoding issues. Each invocation has a 30-second deadline and a bounded `Cmd.WaitDelay`; candidate inputs are limited to 64 KiB and requests/responses to 1 MiB. Fuzz workers share the built helpers. Invalid protocol requests return an empty result.

Raw tests compare validity, decode/encode success, independently validated/canonicalized re-encoding, root/path lookup, Searcher, actual Preorder number callbacks and raw node type. The helper exercises default Sonic marshaling, then independently validates and canonicalizes its JSON output with the standard library. This preserves number tokens while ignoring permitted map ordering and escape-spelling differences (including U+2028/U+2029). Exact representations for supported ordinary cases are checked by the shared consumer. Raw-control inputs additionally use an independent normalization/token oracle; native Sonic accepts them. Invalid UTF-8 in protocol result strings is normalized by the standard JSON transport, so these fields do not assert byte-for-byte invalid-UTF-8 preservation.

`testdata/contract/main.go` is compiled unchanged against upstream and each local backend. This catches missing methods and compares ordinary representation, number modes and panic timing, custom callbacks, retained state/cycles, streams, quoting/unquoting, and AST mutation behavior. Intentional strict raw-input differences of the two opt-in backends are tested in the root module instead.

From the repository root:

```sh
go -C difftest test -mod=readonly ./... -count=1
go -C difftest test -mod=readonly -run='^$' -fuzz='^FuzzUpstreamSonicParity$' -fuzztime=60s -parallel=1
GOTOOLCHAIN=go1.26.7 go -C difftest/upstream test -mod=readonly ./... -count=1
```

The new native harness found `1+00` accepted as a prefix number by the old local scanner; its regression corpus is committed under `testdata/fuzz`. Behavioral boundaries that are not claimed as identical are documented in [compatibility notes](../docs/compatibility.md).
