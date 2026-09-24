# Performance measurements

Measured on 2026-09-22 against baseline commit `86f963e`, using the same workload source and Go 1.27.1 linux/amd64 toolchain. CPU: Intel Core i9-13950HX; both processes pinned to logical CPU 2; `GOMAXPROCS=1`; `-benchtime=200ms -count=5 -benchmem`. Values below are medians. These are local measurements, not a statistical significance claim or a promise across machines.

The baseline directory was extracted from `git archive 86f963e`; the added `workloads_bench_test.go` was copied into its benchmark module unchanged. The large-string Unmarshal baseline was recorded in an additional run with the same flags; the raw baseline log contains both runs. No upstream JIT timings are mixed into this before/after comparison.

| Workload | Before ns/op | After ns/op | Time change | Before B / allocs | After B / allocs |
|---|---:|---:|---:|---:|---:|
| Marshal small struct | 696.5 | 660.2 | -5.2% | 496 / 5 | 336 / 3 |
| Unmarshal medium map | 11412 | 5351 | -53.1% | 4872 / 147 | 4368 / 112 |
| Unmarshal 64 KiB string map | 119602 | 64376 | -46.2% | 65947 / 8 | 65907 / 6 |
| Valid medium | 659.4 | 545.1 | -17.3% | 0 / 0 | 0 / 0 |
| Get nested scalar | 199.5 | 203.8 | +2.2% | 164 / 2 | 164 / 2 |
| Searcher 1000 objects | 703282 | 36377 | -94.8% | 2.27786e+06 / 8012 | 0 / 0 |
| GetFromString, 64 KiB tail | 4570 | 39.56 | -99.1% | 73730 / 2 | 0 / 0 |
| GetCopyFromString, 64 KiB tail | 8678 | 45.51 | -99.5% | 147458 / 3 | 2 / 1 |
| Stream Encode | 278.7 | 159.6 | -42.7% | 224 / 3 | 0 / 0 |
| EncodeInto, reused destination | 290.4 | 167 | -42.5% | 224 / 3 | 0 / 0 |
| Default decode, six numbers | 1635 | 649.7 | -60.3% | 648 / 23 | 536 / 13 |
| UseInt64, six numbers | 3105 | 570.6 | -81.6% | 1592 / 44 | 488 / 7 |
| Decoder, 100 consecutive values | 42144 | 24102 | -42.8% | 48002 / 600 | 2168 / 108 |
| Preorder, depth 1024 | 769822 | 6336 | -99.2% | 0 / 0 | 0 / 0 |
| ValidString, 64 KiB | 20106 | 920.6 | -95.4% | 73728 / 1 | 0 / 0 |

Raw outputs: [before](../bench/results/2026-09-22-default-before.txt), [after](../bench/results/2026-09-22-default-after.txt), [summary JSON](../bench/results/2026-09-22-default-summary.json). Workloads: [original four](../bench/rootbench/root_bench_test.go), [additional workloads](../bench/rootbench/workloads_bench_test.go).

The default decoder now uses the validated token cursor for ordinary dynamic targets (`*any` and nil/empty `*map[string]any`); typed values, prefilled pointers and nonempty maps keep the standard-library path. This removes reflection overhead on the medium-map workload without broadening method dispatch. Raw validation shares a byte/string scanner and uses optimized quote search for long strings instead of checking string contents that native Sonic intentionally leaves unchecked. The third-party JSON engine dependency is no longer needed. A word-at-a-time control-byte check avoids the normalization state machine for compact inputs without raw controls.

Other substantial improvements come from removing subtree construction during Searcher validation, repeated Preorder capacity scans, whole-document string copies, repeated encoders/decoders, and number postprocessing. Smaller timing changes remain sensitive to machine load; the figures do not establish universal throughput ratios.

`LoadAll` appears in the raw output but is deliberately excluded from the comparison table. Restoring native Sonic's per-level lazy loading changes how much work this call performs, so its large reduction cannot be presented as doing the same eager parse faster. Node size increased from 152 to 160 bytes to retain parsing state and stable storage; the measured scalar Get allocation size remains unchanged.

`GetFromString` now retains a substring of the original input, matching native Sonic. A long-lived result can retain the original document; `GetCopyFromString` copies only the selected value and avoids that retention. The large-string benchmark looks up an early field without scanning the unrelated tail.

The JSON-v2 backend separately eliminates per-number v1 decoders and repeated option construction. In the six-number workload, preliminary same-toolchain measurements reduced `UseNumber` from 4472 B / 60 allocations to 648 B / 17 allocations, and `UseInt64` from 4720 B / 75 allocations to 552 B / 11 allocations. Its streaming encoder reuses buffers. Reproduce this backend independently; those preliminary counts are not mixed into the default table.

## Reproduce

From `bench/` with cached dependencies:

```sh
GOPROXY=off GOMAXPROCS=1 taskset -c 2 go test -mod=readonly -modfile=go.local.mod -run='^$' -bench=. -benchmem -benchtime=200ms -count=5 ./rootbench
GOEXPERIMENT=jsonv2 GOMAXPROCS=1 go test -mod=readonly -modfile=go.local.mod -tags=sonic_jsonv2 -run='^$' -bench=. -benchmem -benchtime=200ms -count=5 ./rootbench
```

Choose an available CPU for `taskset`, or omit it on other platforms. Longer runs and more repetitions with benchstat are appropriate before publishing hardware-independent claims. The existing PowerShell runner compares local modes with native upstream using distinct toolchains; that is a different experiment.

Compatibility checks and remaining limitations are documented in [compatibility.md](compatibility.md).
