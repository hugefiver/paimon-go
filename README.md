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
- 部分 malformed JSON 的错误文字与错误位置分类仍可能与上游不同。
- 不承诺性能与上游 Sonic 原生/JIT 对等。

更多细节见 [docs/compatibility.md](docs/compatibility.md)。

## 基准

Windows/amd64、i9-12900H；本地三种模式 Go 1.27.1，upstream Sonic v1.15.2 原生（`APIKind=1`）Go 1.26.7。四模式使用相同 fixture，预编译并离线校验后按固定种子交错 6 轮，每轮新进程、250ms、进程内预热，`GOMAXPROCS=1`、`GOGC=100`、`GOAMD64=v1`，取中位数。跨工具链，仅作参考。

| | default | `sonic_stdjson` | `sonic_jsonv2` | upstream |
|---|---:|---:|---:|---:|
| Marshal<br>small struct | 1094.5 ns<br>336 B · 3 | 1038 ns<br>336 B · 3 | 1126 ns<br>336 B · 3 | 360.1 ns<br>256 B · 3 |
| Unmarshal<br>medium map | 9354.5 ns<br>4368 B · 112 | 7604.5 ns<br>4368 B · 112 | 21033.5 ns<br>4872 B · 147 | 5646.5 ns<br>5360 B · 59 |
| Valid<br>medium JSON | 898.5 ns<br>0 B · 0 | 955.4 ns<br>0 B · 0 | 920.05 ns<br>0 B · 0 | 736.25 ns<br>0 B · 0 |
| Get<br>nested path | 289.8 ns<br>164 B · 2 | 861.25 ns<br>164 B · 2 | 778.4 ns<br>164 B · 2 | 206.45 ns<br>40 B · 2 |

相对于 upstream 的耗时差异（本地纯 Go vs 上游原生/JIT，慢是正常的）：

| | default | `sonic_stdjson` | `sonic_jsonv2` |
|---|---:|---:|---:|
| Marshal | +203.9% | **+188.3%** | *+212.7%* |
| Unmarshal | +65.7% | **+34.7%** | *+272.5%* |
| Valid | **+22.0%** | *+29.8%* | +25.0% |
| Get | **+40.4%** | *+317.2%* | +277.0% |

**加粗** 为该行本地最快，*斜体* 为本地最慢。完整 34 项 workload 数据、逐轮原始日志与方法说明见 [docs/performance.md](docs/performance.md)。

复现：

```powershell
pwsh -NoProfile -File .\bench\run.ps1
```

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
