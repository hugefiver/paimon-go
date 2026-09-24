# Performance measurements

Measured on 2026-09-23 on Windows/amd64 (Intel Core i9-12900H). Local modes were built with Go 1.27.1; upstream Sonic v1.15.2 native (`APIKind=1`) was built with Go 1.26.7. The comparison is therefore cross-toolchain: differences below combine implementation and toolchain effects and are not a pure implementation attribution.

All four modes run the same `rootbench` source and fixtures. Binaries are prebuilt and checked offline (`GOPROXY=off`, `GONOPROXY=none`) before any timing. Each mode runs 6 rounds in seeded shuffled order (`seed=20260923`), each round a fresh process at `-benchtime=250ms -count=1`, with `GOMAXPROCS=1`, `GOGC=100`, `GOAMD64=v1` and in-process warmup before the timer. The table below shows the median ns/op of the 6 samples; `B/op · allocs` is per-round output and is stable across rounds except for a ±1 B alignment difference on the largest string workloads.

| Workload | default | `sonic_stdjson` | `sonic_jsonv2` | upstream |
|---|---:|---:|---:|---:|
| Container/10/Get | 1355 ns<br>384 B · 1 | 2357 ns<br>384 B · 1 | 2184.5 ns<br>384 B · 1 | 828.8 ns<br>384 B · 1 |
| Container/10/LoadAll | 1719 ns<br>552 B · 6 | 1885.5 ns<br>552 B · 6 | 1759 ns<br>552 B · 6 | 1297.5 ns<br>1104 B · 4 |
| Container/10/SearcherDefault | 559.25 ns<br>0 B · 0 | 627.7 ns<br>0 B · 0 | 577.45 ns<br>0 B · 0 | 639.5 ns<br>0 B · 0 |
| Container/10/SearcherNoValidate | 683.6 ns<br>0 B · 0 | 724.6 ns<br>0 B · 0 | 664.1 ns<br>0 B · 0 | 43.36 ns<br>0 B · 0 |
| Container/1000/Get | 129906.5 ns<br>40960 B · 1 | 214150 ns<br>40961 B · 1 | 186124 ns<br>40961 B · 1 | 69326 ns<br>40962 B · 1 |
| Container/1000/LoadAll | 123314 ns<br>552 B · 6 | 133873 ns<br>552 B · 6 | 115992 ns<br>552 B · 6 | 65440.5 ns<br>1104 B · 4 |
| Container/1000/SearcherDefault | 56350 ns<br>0 B · 0 | 55210 ns<br>0 B · 0 | 54521 ns<br>0 B · 0 | 54035 ns<br>0 B · 0 |
| Container/1000/SearcherNoValidate | 67626 ns<br>0 B · 0 | 69723 ns<br>0 B · 0 | 63366.5 ns<br>0 B · 0 | 1855.5 ns<br>0 B · 0 |
| DecodeNumbers/Default | 1079.5 ns<br>536 B · 13 | 1064.6 ns<br>536 B · 13 | 3038.5 ns<br>648 B · 23 | 883.65 ns<br>745 B · 13 |
| DecodeNumbers/UseInt64 | 1024.85 ns<br>488 B · 7 | 897.25 ns<br>488 B · 7 | 2330 ns<br>552 B · 11 | 752.85 ns<br>696 B · 7 |
| DecodeNumbers/UseNumber | 3627.5 ns<br>1344 B · 29 | 3453.5 ns<br>1344 B · 29 | 2292.5 ns<br>648 B · 17 | 964.9 ns<br>792 B · 13 |
| Encoder/EncodeIntoReuse | 292.9 ns<br>0 B · 0 | 282.6 ns<br>0 B · 0 | 266.05 ns<br>0 B · 0 | 99.26 ns<br>16 B · 1 |
| Encoder/RootMarshal | 386.8 ns<br>48 B · 1 | 379.5 ns<br>48 B · 1 | 334.45 ns<br>48 B · 1 | 152.3 ns<br>64 B · 2 |
| Encoder/RootStream | 272.05 ns<br>0 B · 0 | 259.1 ns<br>0 B · 0 | 247.55 ns<br>0 B · 0 | 139.05 ns<br>17 B · 2 |
| Encoder/StdStream | 264.55 ns<br>0 B · 0 | 231.95 ns<br>0 B · 0 | 228.6 ns<br>0 B · 0 | 139.65 ns<br>0 B · 0 |
| GetPath | 289.8 ns<br>164 B · 2 | 861.25 ns<br>164 B · 2 | 778.4 ns<br>164 B · 2 | 206.45 ns<br>40 B · 2 |
| MarshalSmallStruct | 1094.5 ns<br>336 B · 3 | 1038 ns<br>336 B · 3 | 1126 ns<br>336 B · 3 | 360.1 ns<br>256 B · 3 |
| PreorderDepth/1024 | 27287 ns<br>0 B · 0 | 25905.5 ns<br>0 B · 0 | 25768.5 ns<br>0 B · 0 | 37639 ns<br>0 B · 0 |
| PreorderDepth/256 | 6096.5 ns<br>0 B · 0 | 5827 ns<br>0 B · 0 | 6593 ns<br>0 B · 0 | 9141.5 ns<br>0 B · 0 |
| PreorderDepth/64 | 1463.5 ns<br>0 B · 0 | 1336.5 ns<br>0 B · 0 | 1515.5 ns<br>0 B · 0 | 2115 ns<br>0 B · 0 |
| StringDecoder/PackageDecoder | 41477.5 ns<br>2168 B · 108 | 39348 ns<br>2168 B · 108 | 41653 ns<br>2168 B · 108 | 9081.5 ns<br>832 B · 101 |
| StringDecoder/StdDecoder | 39792.5 ns<br>2168 B · 108 | 38060.5 ns<br>2168 B · 108 | 40187.5 ns<br>2168 B · 108 | 36081 ns<br>3280 B · 108 |
| StringGet/16/Bytes | 86.58 ns<br>2 B · 1 | 206.45 ns<br>2 B · 1 | 211.15 ns<br>2 B · 1 | 72.86 ns<br>8 B · 1 |
| StringGet/16/CopyString | 86.02 ns<br>2 B · 1 | 275.15 ns<br>50 B · 2 | 272.95 ns<br>50 B · 2 | 68.24 ns<br>8 B · 1 |
| StringGet/16/Searcher | 53.72 ns<br>0 B · 0 | 47.56 ns<br>0 B · 0 | 40.96 ns<br>0 B · 0 | 39.33 ns<br>0 B · 0 |
| StringGet/16/String | 72.8 ns<br>0 B · 0 | 248.75 ns<br>48 B · 1 | 260.5 ns<br>48 B · 1 | 43.1 ns<br>0 B · 0 |
| StringGet/65536/Bytes | 89.48 ns<br>2 B · 1 | 32565.5 ns<br>2 B · 1 | 29442 ns<br>2 B · 1 | 68.53 ns<br>8 B · 1 |
| StringGet/65536/CopyString | 78.88 ns<br>2 B · 1 | 39218 ns<br>73732 B · 2 | 41656 ns<br>73733 B · 2 | 65.17 ns<br>8 B · 1 |
| StringGet/65536/Searcher | 45.79 ns<br>0 B · 0 | 44.32 ns<br>0 B · 0 | 39.65 ns<br>0 B · 0 | 40.43 ns<br>0 B · 0 |
| StringGet/65536/String | 70.44 ns<br>0 B · 0 | 40876 ns<br>73730 B · 1 | 40205 ns<br>73731 B · 1 | 40.18 ns<br>0 B · 0 |
| UnmarshalLargeString | 98474.5 ns<br>65906 B · 6 | 92454.5 ns<br>65906 B · 6 | 48415.5 ns<br>65946 B · 8 | 19348 ns<br>74123 B · 6 |
| UnmarshalMediumMap | 9354.5 ns<br>4368 B · 112 | 7604.5 ns<br>4368 B · 112 | 21033.5 ns<br>4872 B · 147 | 5646.5 ns<br>5360 B · 59 |
| ValidMedium | 898.5 ns<br>0 B · 0 | 955.4 ns<br>0 B · 0 | 920.05 ns<br>0 B · 0 | 736.25 ns<br>0 B · 0 |
| ValidStringLarge | 1586 ns<br>0 B · 0 | 42494.5 ns<br>73730 B · 1 | 47447.5 ns<br>73731 B · 1 | 1671 ns<br>0 B · 0 |

Relative time versus upstream (positive = slower than upstream, negative = faster; local pure Go versus native/JIT plus a toolchain difference, so being slower is expected):

| Workload | default | `sonic_stdjson` | `sonic_jsonv2` |
|---|---:|---:|---:|
| Marshal small struct | +203.9% | **+188.3%** | *+212.7%* |
| Unmarshal medium map | +65.7% | **+34.7%** | *+272.5%* |
| Valid medium | **+22.0%** | *+29.8%* | +25.0% |
| Get nested scalar | **+40.4%** | *+317.2%* | +277.0% |
| Searcher 1000 objects, validate | *+4.3%* | +2.2% | **+0.9%** |
| Searcher 1000 objects, no validate | +3544.6% | *+3657.6%* | **+3315.1%** |
| GetFromString, 64 KiB tail | **+75.3%** | *+101632%* | +99962% |
| Preorder, depth 1024 | *-27.5%* | -31.2% | **-31.5%** |
| EncodeInto, reused buffer | *+195.1%* | +184.7% | **+168.0%** |

**Bold** is the fastest local mode for that row, *italic* the slowest. Raw per-round logs, build proofs, seed and round order: [bench/results/2026-09-23-four-mode](../bench/results/2026-09-23-four-mode).

Notable differences between modes beyond raw time:

- `sonic_stdjson` and `sonic_jsonv2` intentionally validate complete documents for root raw lookup and validation APIs (see [compatibility.md](compatibility.md)). Large-string lookups therefore copy and validate the whole 64 KiB document instead of retaining a substring, which is why `StringGet/65536/*` and `ValidStringLarge` show tens of microseconds and ~73 KB allocations in those modes while the default mode matches upstream's substring-retaining shape.
- The default mode's `Searcher` with `ValidateJSON=false` still walks the container bytes in pure Go; upstream's native scanner skips them with JIT assistance. The 1000-object container shows 67.6 µs local versus 1.9 µs upstream on this workload.
- `GetFromString` in the default mode returns a substring of the original input, matching native Sonic; a long-lived result retains the original document. `GetCopyFromString` copies only the selected value.
- `PreorderDepth` is faster than upstream in all three local modes on this machine; treat the cross-toolchain margin as indicative only.
- The largest string workloads (`UnmarshalLargeString`, `StringGet/65536/CopyString`, `ValidStringLarge` in strict modes) vary by ±1 B/op across rounds due to allocation alignment; allocation counts are otherwise round-stable.

These are single-machine medians, not a statistical significance claim. For publication-grade comparisons, pin machine conditions, inspect the raw logs and use benchstat on repeated runs.

A same-toolchain Linux before/after comparison against baseline `86f963e` (Intel i9-13950HX, Go 1.27.1, 5 × 200 ms medians) was recorded when this branch was authored; its raw logs remain in [bench/results](../bench/results) (`2026-09-22-default-*`). The tables above supersede it as the current reference.

## Reproduce

From `bench/` with cached dependencies and toolchains (Go 1.27.x on PATH, Go 1.26.7 cached):

```powershell
pwsh -NoProfile -File .\run.ps1 -OutputDir ..\bench\results\<new-timestamp-dir>
```

The runner prebuilds and checks all four modes, then times them in seeded shuffled rounds. See [bench/README.md](../bench/README.md) for parameters and the offline guarantees. Compatibility checks and remaining limitations are documented in [compatibility.md](compatibility.md).
