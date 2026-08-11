# Sonic benchmark subproject

This directory contains a standalone benchmark module for comparing three JSON modes against the same benchmark shapes:

1. local root `github.com/bytedance/sonic` using the repository's default fastjson/stdjson-compatible implementation;
2. local `github.com/bytedance/sonic/stdjsonv2` with `GOEXPERIMENT=jsonv2`;
3. upstream `github.com/bytedance/sonic` v1.15.2.

The root repository already uses the module path `github.com/bytedance/sonic`, so the benchmark module cannot import the local root package and upstream Sonic through one module file at the same time. The switch is handled with two module files:

- `go.mod` has no `replace`, so `github.com/bytedance/sonic v1.15.2` resolves to upstream Sonic.
- `go.local.mod` keeps the same requirement but adds `replace github.com/bytedance/sonic => ..`, so imports resolve to the repository root.

The benchmarks are intended to provide reproducible side-by-side numbers only. This project does not promise that the local replacement is performance-equivalent to upstream Sonic.

## Runner

Run all three modes from this directory:

```powershell
pwsh -File .\run.ps1
```

The runner prints three labeled blocks:

- `=== local root fastjson/default ===`
- `=== local stdjsonv2 ===`
- `=== upstream sonic v1.15.2 ===`

## Manual commands

Local root default mode:

```powershell
go test "-modfile=go.local.mod" ./rootbench -bench=. -benchmem -run='^$'
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

`stdjsonv2` requires both `GOEXPERIMENT=jsonv2` and a Go toolchain that provides `encoding/json/v2` plus `encoding/json/jsontext`. If the current toolchain does not include that experiment, only the root/default and upstream modes can run.

The first run may create or update `go.sum` / `go.local.sum` files for the selected module file.
