# Paimon Go

Sonic `v1.15.2` 的纯 Go 源码兼容实现。`module github.com/bytedance/sonic`，Go 1.27。

## 项目定位与兼容边界

不移植 Sonic 的原生/JIT 编解码器，只覆盖公开 API。运行库仅依赖 Go 标准库。`APIKind` 始终等于 `UseSonicJSON`。

## 使用

在项目的 `go.mod` 里加一行 replace 就行，不需要改 import：

```go
require github.com/bytedance/sonic v1.15.2

replace github.com/bytedance/sonic => github.com/hugefiver/paimon-go v0.1.0
```

代码还是 `import "github.com/bytedance/sonic"`。`github.com/hugefiver/paimon-go` 只是托管地址，不是 import 路径。

如果不想走网络，也可以 clone 到本地然后用 `replace github.com/bytedance/sonic => ../paimon-go`。

## 快速开始

```go
package main

import (
    "fmt"
    "github.com/bytedance/sonic"
)

type User struct {
    Name string `json:"name"`
}

func main() {
    data, _ := sonic.Marshal(User{Name: "Ada"})
    var user User
    _ = sonic.Unmarshal(data, &user)
    fmt.Println(sonic.Valid(data))
    node, _ := sonic.Get([]byte(`{"user":{"name":"Ada"}}`), "user", "name")
    name, _ := node.String()
    fmt.Println(user.Name, name)
}
```

## Root build tags

| tag | 行为 | 要求 |
|---|---|---|
| 无 | Sonic-compatible 默认 | Go 1.27 |
| `sonic_stdjson` | 严格 raw JSON | Go 1.27 |
| `sonic_jsonv2` | Marshal/Unmarshal/Valid/Get 走 JSON-v2 | 需 `goexperiment.jsonv2` |

`sonic_stdjson` 与 `sonic_jsonv2` 互斥。`GOEXPERIMENT=none` 加 `sonic_jsonv2` 编译失败。只设 `GOEXPERIMENT=jsonv2` 不会切换 root。

## 兼容限制

- `github.com/bytedance/sonic/loader` 不在范围内。
- 不包含原生/JIT、汇编或 loader 实现。
- 部分依赖原生 codec 的高级配置只保证编译通过，不保证行为一致。
- AST 对 malformed JSON 的错误文字和边界分类可能与上游不同。
- 不承诺性能与上游 Sonic 原生/JIT 对等。

已修复数字模式、公开 StreamDecoder 方法、JSON quoting、AST 稳定指针与惰性加载等兼容问题。高级配置和复杂错误后的部分结果仍有明确边界，更多细节见 [docs/compatibility.md](docs/compatibility.md)。

## 基准

2026-09-22，同机 Linux/amd64、i9-13950HX、Go 1.27.1，固定 CPU 2、`GOMAXPROCS=1`，每项 200ms × 5 次取中位数。对照为优化前提交 `86f963e`，两边使用相同 workload 和工具链。

| 场景 | 优化前 | 优化后 | 分配次数变化 |
|---|---:|---:|---:|
| Unmarshal：中型 map | 11.41 µs | 5.35 µs | 147 → 112 |
| Valid：中型 JSON | 659.4 ns | 545.1 ns | 0 → 0 |
| Searcher：1000 个对象容器 | 703.28 µs | 36.38 µs | 8012 → 0 |
| GetFromString：64 KiB 尾部，只取前部字段 | 4.57 µs | 39.6 ns | 2 → 0 |
| GetCopyFromString：同一输入 | 8.68 µs | 45.5 ns | 3 → 1 |
| 流式 Encode | 278.7 ns | 159.6 ns | 3 → 0 |
| EncodeInto：复用目标 buffer | 290.4 ns | 167.0 ns | 3 → 0 |
| UseInt64：6 个数字 | 3.10 µs | 570.6 ns | 44 → 7 |
| Preorder：1024 层嵌套 | 769.82 µs | 6.34 µs | 0 → 0 |
| ValidString：64 KiB | 20.11 µs | 920.6 ns | 1 → 0 |

这些是具体 workload 的实测结果，不代表所有调用等比例提升。默认动态值解码和原生结构校验均有优化；typed 结构体、预填指针和非空 map 仍保留标准库路径。`LoadAll` 恢复了原生 Sonic 的按层惰性语义，其工作量变化没有计作纯性能收益。完整方法、原始数据、内存分配和复现命令见 [docs/performance.md](docs/performance.md)。

### 本机四模式复测（2026-09-23）

Windows/amd64、i9-12900H；本地三种模式使用 Go 1.27.1，上游原生 Sonic v1.15.2 使用 Go 1.26.7（已确认 `APIKind=1`）。四组使用相同 fixture，`GOMAXPROCS=1`、`GOGC=100`、`GOAMD64=v1`；预编译并完成正确性检查后，按固定种子 `20260923` 交错运行 6 轮，每轮每模式启动新进程，各项预热后测量 250ms。下表是部分场景的 **ns/op 中位数**（越低越好）：

| 场景 | 本地默认 | 本地 `sonic_stdjson` | 本地 `sonic_jsonv2` | 上游原生 |
|---|---:|---:|---:|---:|
| `UnmarshalMediumMap` | 9,354.5 | 7,604.5 | 21,033.5 | 5,646.5 |
| `Container/1000/SearcherDefault` | 56,350 | 55,210 | 54,521 | 54,035 |
| `Container/1000/SearcherNoValidate` | 67,626 | 69,723 | 63,366.5 | 1,855.5 |
| `StringGet/65536/String` | 70.44 | 40,876 | 40,205 | 40.18 |
| `DecodeNumbers/UseInt64` | 1,024.85 | 897.25 | 2,330 | 752.85 |
| `Encoder/EncodeIntoReuse` | 292.9 | 282.6 | 266.05 | 99.26 |
| `PreorderDepth/1024` | 27,287 | 25,905.5 | 25,768.5 | 37,639 |
| `ValidStringLarge` | 1,586 | 42,494.5 | 47,447.5 | 1,671 |

这些值来自 34 项 benchmark 中的 8 项；各项每模式均有 6 个样本，runner 的 `summary.csv` 和逐轮日志还包含波动、分配及完整数据。两代 Go 工具链不同，**不能把本地与上游的差距全部归因于实现**；部分场景的变异系数较高，也不宜凭一次测量下普遍性能结论。此表与上方 Linux 同工具链的优化前后对照是两次独立实验，不应交叉计算提升比例。复现和统计口径见 [bench/README.md](bench/README.md)：

```powershell
pwsh -NoProfile -File .\bench\run.ps1
```

runner 使用 `GOPROXY=off` 和 `GONOPROXY=none`，不下载依赖；请在本机预先缓存所需 Go 工具链及依赖。

## 测试与 fuzz

原生 Sonic 差分测试独立于根 module：上游 helper 固定 Go 1.26.7，并检查 `APIKind`，防止误用 Go 1.27 标准库回退。共享 consumer 同时检查默认、严格和 JSON-v2 模式；新发现的 scanner 边界保留为 fuzz 回归样例。

```powershell
go test -mod=readonly ./... -count=1
go test -mod=readonly -tags sonic_stdjson ./... -count=1
$env:GOEXPERIMENT = 'jsonv2'
go test -mod=readonly -tags sonic_jsonv2 ./... -count=1
go test -mod=readonly . -run '^$' -fuzz '^FuzzValidParity$' -fuzztime=5s
Push-Location .\difftest
go test -mod=readonly . -run '^$' -fuzz '^FuzzUpstreamSonicParity$' -fuzztime=120s -parallel=1
Pop-Location
```
跨模块原生契约与 race 检查：

```sh
go test -mod=readonly -race ./... -count=1
go -C difftest test -mod=readonly ./... -count=1
```
