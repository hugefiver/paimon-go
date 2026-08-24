# Sonic benchmark subproject

This directory contains a standalone benchmark module for comparing four JSON modes against the same benchmark shapes:

1. local root `github.com/bytedance/sonic` using the repository's default Sonic-compatible raw JSON implementation;
2. local root `github.com/bytedance/sonic` with `-tags sonic_stdjson` enabled for strict standard raw JSON behavior;
3. local `github.com/bytedance/sonic/stdjsonv2` with `GOEXPERIMENT=jsonv2`;
4. upstream `github.com/bytedance/sonic` v1.15.2.

The root repository already uses the module path `github.com/bytedance/sonic`, so the benchmark module cannot import the local root package and upstream Sonic through one module file at the same time. The switch is handled with two module files:

- `go.mod` has no `replace`, so `github.com/bytedance/sonic v1.15.2` resolves to upstream Sonic.
- `go.local.mod` keeps the same requirement but adds `replace github.com/bytedance/sonic => ..`, so imports resolve to the repository root.

The benchmarks are intended to provide reproducible side-by-side numbers only. This project does not promise that the local replacement is performance-equivalent to upstream Sonic.

## Runner

Run all four modes from this directory:

```powershell
pwsh -File .\run.ps1
```

The runner prints four labeled blocks:

- `=== local root sonic/default ===`
- `=== local root stdjson tag ===`
- `=== local stdjsonv2 ===`
- `=== upstream sonic v1.15.2 ===`

## Manual commands

Local root default Sonic-compatible mode:

```powershell
go test "-modfile=go.local.mod" ./rootbench -bench=. -benchmem -run='^$'
```

Local root strict standard-JSON mode:

```powershell
go test "-modfile=go.local.mod" -tags sonic_stdjson ./rootbench -bench=. -benchmem -run='^$'
```

Local `stdjsonv2` mode:

```powershell
$oldExperiment = $env:GOEXPERIMENT
$env:GOEXPERIMENT = "jsonv2"
go test "-modfile=go.local.mod" ./localv2bench -bench=. -benchmem -run='^$'
$env:GOEXPERIMENT = $oldExperiment
```

Upstream Sonic v1.15.2 mode:

```powershell
Remove-Item Env:GOEXPERIMENT -ErrorAction SilentlyContinue
go test ./rootbench -bench=. -benchmem -run='^$'
```

The benchmark runner and manual command set `GOEXPERIMENT=jsonv2` explicitly
for reproducibility. On the required Go 1.27 baseline, the default experiment
set also selects the real backend. The toolchain must expose
`encoding/json/v2` and `encoding/json/jsontext`. Explicit `GOEXPERIMENT=none`
selects the stub and cannot run `localv2bench` as the real backend.

The first run may create or update `go.sum` / `go.local.sum` files for the selected module file.
