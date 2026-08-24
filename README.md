# Paimon Go：可移植的 Sonic 兼容 JSON 实现

本项目提供 `github.com/bytedance/sonic` 的源码兼容替代实现，兼容目标为
Sonic `v1.15.2` 的公开 API。下游项目可以保留原有 Sonic import，只需在
`go.mod` 中通过 `replace` 指向本仓库。

本项目面向需要 **Go 1.27、跨平台构建和纯 Go 回退实现** 的使用场景；它没有
移植 Sonic 的原生/JIT 编解码器，也不承诺达到上游原生实现的性能。

## 项目特性

- **源码兼容**：覆盖 root package、`ast`、`encoder`、`decoder`、`option`、
  `utf8`、`unquote`、`fastjson` 和实验性的 `stdjsonv2` 等常用公开接口。
- **可移植实现**：任意 Go 值的 `Marshal`、`Unmarshal`、流式 Encoder/Decoder
  基于标准库反射实现，不依赖 Sonic 的汇编、loader 或 JIT。
- **Sonic 风格的 Raw JSON 热路径**：默认 `Valid`、`Get`、AST Searcher 等入口
  保留已验证的 Sonic 兼容行为，包括部分非标准 JSON 边界。
- **可切换严格模式**：使用 `sonic_stdjson` build tag，让 raw JSON 入口遵循
  `encoding/json` 风格的严格校验。
- **完整 AST 能力**：支持 Node、Parser、Searcher、Visitor、Iterator、路径查询和
  lazy raw node；Parser/raw scanner 按兼容约束支持最多 4096 层嵌套。
- **稳健的流式写入**：Encoder 能处理 short write；writer 无进展时返回
  `io.ErrShortWrite`。
- **数字模式兼容**：root 和 `stdjsonv2` 同时启用 `UseNumber`、`UseInt64` 时，
  按 Go 1.27 下 Sonic v1.15.2 回退实现的行为由 `UseNumber` 优先。
- **差分与模糊测试**：仓库包含针对上游 Sonic v1.15.2 的 deterministic
  differential tests 和 fuzz targets。
- **Root JSON v2 build tag**：启用 `sonic_jsonv2` 后，root 的 Marshal、
  Unmarshal、Valid、Get 和流式接口统一切换到 `encoding/json/v2`；非法构建组合
  会直接编译失败，不会静默回退。

## 安装与本地替换

预定源码发布位置是 <https://github.com/hugefiver/paimon-go>。该 GitHub 仓库当前
为空，不能通过 clone 获得 `go.mod` 或源码，也不是当前可安装来源。当前可执行流程
要求已有一个包含本项目 `go.mod` 和源码的本地 checkout；当它与 consumer 目录相邻
时，在 consumer 的 `go.mod` 中保留原 Sonic 依赖并指向该 checkout：

```go
require github.com/bytedance/sonic v1.15.2

replace github.com/bytedance/sonic => ../paimon-go
```

该 checkout 的根 `go.mod` 仍声明 `module github.com/bytedance/sonic`；GitHub 托管
地址不是新的 Go module path。现有源码继续保留：

```go
import "github.com/bytedance/sonic"
```

只有用户另行授权发布源码后，GitHub clone 才成为可用流程，届时再补充 clone
说明；只有正式 release 发布后，才能补充远程 module-version 替换流程。当前不提供
clone 命令或任何未发布版本命令。

## 快速开始

```go
package main

import (
    "bytes"
    "fmt"
    "strings"

    "github.com/bytedance/sonic"
)

type User struct {
    Name string `json:"name"`
    Age  int    `json:"age"`
}

func main() {
    data, err := sonic.Marshal(User{Name: "Ada", Age: 37})
    if err != nil {
        panic(err)
    }

    var user User
    if err := sonic.Unmarshal(data, &user); err != nil {
        panic(err)
    }
    fmt.Println(user.Name)

    fmt.Println(sonic.Valid([]byte(`{"name":"Ada","age":37}`)))

    node, err := sonic.Get(
        []byte(`{"users":[{"name":"Ada"}]}`),
        "users", 0, "name",
    )
    if err != nil {
        panic(err)
    }
    name, err := node.String()
    if err != nil {
        panic(err)
    }
    fmt.Println(name)

    var out bytes.Buffer
    enc := sonic.ConfigDefault.NewEncoder(&out)
    if err := enc.Encode(User{Name: "Grace", Age: 85}); err != nil {
        panic(err)
    }

    dec := sonic.ConfigDefault.NewDecoder(strings.NewReader(out.String()))
    var decoded User
    if err := dec.Decode(&decoded); err != nil {
        panic(err)
    }
    fmt.Println(decoded.Name)
}
```

## Root 后端与 build tag

所有新接入和模式切换都保留同一个 import：

```go
import "github.com/bytedance/sonic"
```

| 项目 build tag | Root 行为 | 工具链要求 |
|---|---|---|
| 无 | 当前 Sonic-compatible 默认路径 | Go 1.27 |
| `sonic_stdjson` | 现有严格 raw JSON 模式 | Go 1.27；可与 `GOEXPERIMENT=none` 一起使用 |
| `sonic_jsonv2` | Marshal、MarshalIndent、Unmarshal、Valid、Get、Encoder、Decoder 使用 JSON v2 | 必须存在有效的 `goexperiment.jsonv2`；Go 1.27 默认满足 |

```powershell
go test -mod=readonly -tags sonic_stdjson ./... -count=1
go test -mod=readonly -tags sonic_jsonv2 ./... -count=1
```

`sonic_stdjson` 与 `sonic_jsonv2` 互斥。显式设置
`GOEXPERIMENT=none` 后再启用 `sonic_jsonv2` 也会编译失败；两种情况都不会静默
回退。仅设置 `GOEXPERIMENT=jsonv2` 不会切换 root 后端。
三种有效模式下，`APIKind` 始终等于 `UseSonicJSON`。

`fastjson` 和 `stdjsonv2` 子包继续供已经使用这些 import 的源码兼容；它们不是
新迁移或后端选择流程。运行时 `Config` 和 Encoder/Decoder 选项仍由调用代码控制。
`github.com/bytedance/sonic/loader` 不在本项目兼容范围内。

## Benchmark：与上游 Sonic v1.15.2 对比

以下结果来自 2026-08-24 对当前 build-tag root 接线版本的一次本机实测，每个
benchmark 串行执行 3 次并取 `ns/op` 中位数。数值越低越好。

### 测试环境

- 操作系统：Windows/amd64
- Local default、`sonic_stdjson`、JSON v2 implementation：`go1.27.0`
- Upstream Sonic v1.15.2：`go1.26.7`（通过 `GOTOOLCHAIN` 单独选择）
- CPU：12th Gen Intel Core i9-12900H
- 并行度：20（benchmark 名称后缀 `-20`）
- 参数：`-benchmem -count=3`，使用默认 benchtime
- 依赖模式：全部使用 `-mod=readonly`

> **重要限制：** 本项目要求 Go 1.27；上游 Sonic v1.15.2 为进入其受支持的
> 原生/JIT 路径而单独使用 Go 1.26.7。因此下表是在相同硬件和 payload 上进行的
> **跨 Go 工具链对比**，不是只改变 JSON 实现的单变量实验。结果会随硬件、
> 系统负载和 Go 版本变化。

### 时间结果（中位数）

| Benchmark | Local default / Go 1.27 | Local `sonic_stdjson` / Go 1.27 | Local JSON v2 implementation / Go 1.27 | Upstream v1.15.2 / Go 1.26.7 |
|---|---:|---:|---:|---:|
| Marshal small struct | 2,017 ns/op | 1,962 ns/op | 2,291 ns/op | **537.8 ns/op** |
| Unmarshal medium map | 30,962 ns/op | 43,193 ns/op | 45,044 ns/op | **11,138 ns/op** |
| Valid medium JSON | 1,547 ns/op | 3,393 ns/op | 1,503 ns/op | **1,069 ns/op** |
| Get nested path | 536.3 ns/op | 1,908 ns/op | 1,061 ns/op | **278.4 ns/op** |

相对同机 upstream Go 1.26.7 中位数：

- Local default：Marshal 约为 upstream 的 **3.75 倍耗时**，Unmarshal 慢约
  **178.0%**，Valid 慢约 **44.7%**，Get 慢约 **92.6%**。
- Local `sonic_stdjson`：Marshal 约为 upstream 的 **3.65 倍耗时**，
  Unmarshal 慢约 **287.8%**，Valid 慢约 **217.4%**，Get 约为 upstream 的
  **6.85 倍耗时**。
- Local JSON v2 implementation：Marshal 约为 upstream 的 **4.26 倍耗时**，
  Unmarshal 慢约 **304.4%**，Valid 慢约 **40.6%**，Get 约为 upstream 的
  **3.81 倍耗时**。

### 内存分配

表格单元格式为 `B/op · allocs/op`：

| Benchmark | Local default | Local `sonic_stdjson` | Local JSON v2 implementation | Upstream Go 1.26.7 |
|---|---:|---:|---:|---:|
| Marshal small struct | 496 · 5 | 496 · 5 | 336 · 3 | **268 · 3** |
| Unmarshal medium map | 4,876 · 147 | 4,876 · 147 | 4,940 · 148 | 5,402 · 59 |
| Valid medium JSON | 0 · 0 | 0 · 0 | 0 · 0 | 0 · 0 |
| Get nested path | 148 · 2 | 148 · 2 | 148 · 2 | **40 · 2** |

本次结果显示：使用 Go 1.26.7 原生路径的 upstream Sonic 在四项时间指标上都
更快，且 Marshal/Get 的分配量更低。本地三种模式中，`sonic_stdjson` 的
Marshal 中位数最低，default 的 Unmarshal/Get 最低，JSON v2 implementation 的
Valid 最低并减少了 Marshal 分配。本次只运行 3 轮且单轮波动明显，适合作为开发期
横向参考，不应直接作为发布级性能承诺。

### 复现命令

从仓库根目录执行：

```powershell
Push-Location .\bench

# Local benchmark 使用当前项目工具链；应输出 go1.27.x。
go version

# Local root / default
go test -mod=readonly '-modfile=go.local.mod' -run '^$' -bench '.' -benchmem -count=3 ./rootbench

# Local root / strict standard JSON
go test -mod=readonly '-modfile=go.local.mod' -tags sonic_stdjson -run '^$' -bench '.' -benchmem -count=3 ./rootbench

# Local root / JSON v2 tag
$hadExperiment = Test-Path Env:GOEXPERIMENT
$oldExperiment = $env:GOEXPERIMENT
$hadToolchain = Test-Path Env:GOTOOLCHAIN
$oldToolchain = $env:GOTOOLCHAIN
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

# Upstream Sonic v1.15.2
try {
    $env:GOTOOLCHAIN = 'go1.26.7'
    Remove-Item Env:GOEXPERIMENT -ErrorAction SilentlyContinue
    go test -mod=readonly -run '^$' -bench '.' -benchmem -count=3 ./rootbench
}
finally {
    if ($hadExperiment) {
        $env:GOEXPERIMENT = $oldExperiment
    }
    else {
        Remove-Item Env:GOEXPERIMENT -ErrorAction SilentlyContinue
    }
    if ($hadToolchain) {
        $env:GOTOOLCHAIN = $oldToolchain
    }
    else {
        Remove-Item Env:GOTOOLCHAIN -ErrorAction SilentlyContinue
    }
}

Pop-Location
```

benchmark payload 和更详细的模块说明见 [`bench/README.md`](bench/README.md)。

## 测试与验证

### 默认模式

```powershell
go test -mod=readonly ./... -count=1
go vet -mod=readonly ./...
```

### JSON v2 模式

```powershell
$hadExperiment = Test-Path Env:GOEXPERIMENT
$oldExperiment = $env:GOEXPERIMENT
try {
    $env:GOEXPERIMENT = 'jsonv2'
    go test -mod=readonly -tags sonic_jsonv2 ./... -count=1
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

### Bounded fuzz 矩阵

2026-08-24 的发布前验证顺序运行了全部 18 个 fuzz target：root 4 个、AST 3 个、
decoder 2 个、encoder 4 个、unquote 2 个、utf8 2 个，以及 upstream differential
1 个。常规 target 使用 `-fuzztime=5s -parallel=4`；昂贵的 differential target
使用 `-fuzztime=120s -parallel=1`，完成 38 个 baseline corpus 后进入随机变异。

此外，`FuzzValidParity` 和 `FuzzGetNoPanic` 分别在 `sonic_stdjson` 与
`sonic_jsonv2` 下运行 5 秒。该轮测试发现并修正了 `sonic_jsonv2` 仍使用默认
Sonic-compatible Valid oracle 的测试缺陷，并保留最小输入为回归 corpus；修正后
所有 bounded fuzz 均通过，未发现产品 panic 或新的行为不一致。

单个 target 的命令形式：

```powershell
go test -mod=readonly . -run '^$' -fuzz '^FuzzValidParity$' -fuzztime=5s -parallel=4
go test -mod=readonly -tags sonic_jsonv2 . -run '^$' -fuzz '^FuzzGetNoPanic$' -fuzztime=5s -parallel=4
```

### 与上游 Sonic 的差分 fuzz

```powershell
Push-Location .\difftest
go test -mod=readonly . -run '^$' -fuzz '^FuzzUpstreamSonicParity$' -fuzztime=120s -parallel=1
Pop-Location
```

## 兼容范围与限制

- 兼容目标是 Sonic `v1.15.2` 的常用公开 API，不是完整内部实现。
- Sonic 的 loader、汇编 parser/encoder/decoder、JIT 和原生浮点转换未移植。
- 部分依赖 Sonic native codec 的高级配置仅提供源码兼容，无法完整映射到底层行为。
- AST 对 malformed JSON 的错误文字和极端边界分类是 best effort，可能与上游存在差异。
- 本项目不承诺与上游 Sonic 原生/JIT 实现性能相当。

详细行为、已知差异和限制见
[`docs/compatibility.md`](docs/compatibility.md)。
