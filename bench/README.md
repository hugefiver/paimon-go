# Sonic 基准测试子项目

该目录是独立 Go module，用相同 payload 对比以下四种模式：

1. local root `github.com/bytedance/sonic` 默认 Sonic-compatible 模式；
2. local root 启用 `sonic_stdjson` 的严格 JSON 模式；
3. local root 启用 `sonic_jsonv2` 的 JSON v2 模式；
4. upstream `github.com/bytedance/sonic` v1.15.2。

前三种 local 模式使用同一份 `rootbench/root_bench_test.go`，只改变 build flags，
并使用项目要求的 Go 1.27。upstream Sonic v1.15.2 单独使用
Go 1.26.7，以进入其支持的原生/JIT 路径。该口径保持硬件和 benchmark payload
一致，但属于跨 Go 工具链对比。

## Module 文件

根仓库和 upstream 都使用 module path `github.com/bytedance/sonic`，因此不能在
一个 module graph 中同时导入两者。本目录通过两个 module 文件切换来源：

- `go.mod` 没有 `replace`，解析到 upstream Sonic v1.15.2；
- `go.local.mod` 包含 `replace github.com/bytedance/sonic => ..`，解析到本地仓库。

所有 runner 命令都使用 `-mod=readonly`，不会修改 `go.mod`、`go.sum` 或
`go.local.mod`。runner 对每个 block 强制 `GOPROXY=off`：所需 modules 和
upstream toolchain 必须已经缓存，cache miss 会失败而不会下载。

## 一键运行

从本目录执行：

```powershell
pwsh -NoProfile -File .\run.ps1
```

runner 会串行执行四个 block，每项 benchmark 运行 3 次并输出内存分配：

- `=== local root sonic/default ===`
- `=== local root stdjson tag ===`
- `=== local root jsonv2 tag ===`
- `=== upstream sonic v1.15.2 / Go 1.26.7 ===`

runner 在 local JSON v2 block 临时设置 `GOEXPERIMENT=jsonv2`，在 upstream block
临时设置 `GOTOOLCHAIN=go1.26.7` 并移除 `GOEXPERIMENT`。所有四个 block 均在
`GOPROXY=off` 下执行；每块在启动 benchmark 前验证 `$env:GOPROXY`、`go env GOPROXY`
精确为 `off`，并验证完整 `go version` 包含 local 所需的 `go1.27` 或 upstream 所需的
`go1.26.7`。验证成功后，日志会打印稳定的
`PROOF: label=<block>; GOPROXY=off; go version=<完整 go version>` 行；任一命令失败或
值不符都会立即终止，因而不会执行该块 benchmark。缺少缓存的 module 或 toolchain 也会
失败而不会下载。无论成功还是异常，runner 都会精确恢复调用者原有的 `GOPROXY`、
`GOEXPERIMENT` 和 `GOTOOLCHAIN` 的存在性和值。

## 手动命令

下列命令应在离线环境运行；可在包住单个命令的 `try`/`finally` 中临时设置
`$env:GOPROXY = 'off'` 并恢复调用者原值。使用 runner 可自动完成该恢复。

### Local root/default

```powershell
go test -mod=readonly '-modfile=go.local.mod' -run '^$' -bench '.' -benchmem -count=3 ./rootbench
```

### Local root/strict

```powershell
go test -mod=readonly '-modfile=go.local.mod' -tags sonic_stdjson -run '^$' -bench '.' -benchmem -count=3 ./rootbench
```

### Local root/JSON v2

```powershell
$hadExperiment = Test-Path Env:GOEXPERIMENT
$oldExperiment = $env:GOEXPERIMENT
try {
    $env:GOEXPERIMENT = 'jsonv2'
    go test -mod=readonly '-modfile=go.local.mod' -tags sonic_jsonv2 -run '^$' -bench '.' -benchmem -count=3 ./rootbench
}
finally {
    if ($hadExperiment) {
        $env:GOEXPERIMENT = $oldExperiment
    }
    else {
        Remove-Item Env:GOEXPERIMENT -ErrorAction SilentlyContinue
    }
}
```

### Upstream Sonic v1.15.2 / Go 1.26.7

```powershell
$hadToolchain = Test-Path Env:GOTOOLCHAIN
$oldToolchain = $env:GOTOOLCHAIN
$hadExperiment = Test-Path Env:GOEXPERIMENT
$oldExperiment = $env:GOEXPERIMENT
try {
    $env:GOTOOLCHAIN = 'go1.26.7'
    Remove-Item Env:GOEXPERIMENT -ErrorAction SilentlyContinue
    go test -mod=readonly -run '^$' -bench '.' -benchmem -count=3 ./rootbench
}
finally {
    if ($hadToolchain) {
        $env:GOTOOLCHAIN = $oldToolchain
    }
    else {
        Remove-Item Env:GOTOOLCHAIN -ErrorAction SilentlyContinue
    }
    if ($hadExperiment) {
        $env:GOEXPERIMENT = $oldExperiment
    }
    else {
        Remove-Item Env:GOEXPERIMENT -ErrorAction SilentlyContinue
    }
}
```

## 结果解释

- 基准仅用于同机横向参考，不构成性能承诺。
- local 与 upstream 使用不同 Go 版本，结果同时包含实现和工具链差异。
- 笔记本电源策略、温度、后台负载和 Go patch 版本都会影响结果。
- 发布级性能判断应增加运行次数、固定 CPU 条件，并使用 `benchstat` 进行统计分析。
