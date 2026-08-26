# Paimon Go

Sonic `v1.15.2` 的纯 Go 源码兼容实现。`module github.com/bytedance/sonic`，Go 1.27。

## 项目定位与兼容边界

不移植 Sonic 的原生/JIT 编解码器，只覆盖公开 API。`APIKind` 始终等于 `UseSonicJSON`。

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

更多细节见 [docs/compatibility.md](docs/compatibility.md)。

## 基准

Windows/amd64, i9-12900H, 相同 payload, 各跑 5 次取中位数, `-benchmem`, `GOPROXY=off`。本地用 Go 1.27，upstream 用 Go 1.26.7。跨工具链，仅作参考。

| | default | `sonic_stdjson` | `sonic_jsonv2` | upstream |
|---|---:|---:|---:|---:|
| Marshal<br>small struct | 678.8 ns<br>288 B · 3 | 876.7 ns<br>288 B · 3 | 1475 ns<br>336 B · 3 | 420.8 ns<br>269 B · 3 |
| Unmarshal<br>medium map | 14875 ns<br>4736 B · 129 | 14975 ns<br>4736 B · 129 | 23067 ns<br>4956 B · 148 | 7061 ns<br>5402 B · 59 |
| Valid<br>medium JSON | 1327 ns<br>0 B · 0 | 2338 ns<br>0 B · 0 | 1476 ns<br>0 B · 0 | 764.2 ns<br>0 B · 0 |
| Get<br>nested path | 426.8 ns<br>164 B · 2 | 1540 ns<br>164 B · 2 | 933.3 ns<br>164 B · 2 | 264.3 ns<br>40 B · 2 |

相对于 upstream 的耗时差异（本地纯 Go vs 上游原生/JIT，慢是正常的）：

| | default | `sonic_stdjson` | `sonic_jsonv2` |
|---|---:|---:|---:|
| Marshal | **+61.3%** | +108.3% | *+250.5%* |
| Unmarshal | **+110.7%** | +112.1% | *+226.7%* |
| Valid | **+73.6%** | *+206.0%* | +93.1% |
| Get | **+61.5%** | *+482.8%* | +253.1% |

**加粗** 为该行本地最快，*斜体* 为本地最慢。

复现：

```powershell
pwsh -NoProfile -File .\bench\run.ps1
```

## 测试与 fuzz

18 个 fuzz target，提交的 fuzz corpus 总数为 7。

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