# Paimon Go

## 项目定位与兼容边界

本仓库是 Sonic `v1.15.2` 公开 API 的纯 Go 源码兼容目标：module 为 `github.com/bytedance/sonic`，要求 Go 1.27，不移植 Sonic 的原生/JIT 编解码器。

## 克隆、原始 import 与本地 replace

```powershell
git clone https://github.com/hugefiver/paimon-go.git
```

在 consumer 的 `go.mod` 中保留 Sonic 依赖并指向本地 checkout：

```go
require github.com/bytedance/sonic v1.15.2

replace github.com/bytedance/sonic => ../paimon-go
```

代码仍使用原始 import：

```go
import "github.com/bytedance/sonic"
```

`github.com/hugefiver/paimon-go` 是源码托管地址，不是 import 或 `go install` 目标；根 module 仍是 `module github.com/bytedance/sonic`。

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

| build tag | Root 行为 | 要求 |
|---|---|---|
| 无 | Sonic-compatible 默认路径 | Go 1.27 |
| `sonic_stdjson` | 严格 raw JSON 路径 | Go 1.27 |
| `sonic_jsonv2` | root 高频 API 使用 JSON-v2 | 有效的 `goexperiment.jsonv2` |

`sonic_stdjson` 与 `sonic_jsonv2` 互斥。`GOEXPERIMENT=none` 加 `sonic_jsonv2` 会编译失败；只设置 `GOEXPERIMENT=jsonv2` 不会选择 root v2。`APIKind` 始终等于 `UseSonicJSON`。

## 兼容限制

- `github.com/bytedance/sonic/loader` 不在范围内。
- 不提供 Sonic 原生/JIT、汇编或 loader 实现。
- 依赖原生 codec 的高级配置只保证源码兼容，不能完整映射行为。
- malformed AST 的错误文字和极端边界分类可能与上游不同。
- 不承诺与 Sonic 原生/JIT 实现的性能一致。

详细行为见 [docs/compatibility.md](docs/compatibility.md)。

## 基准

在 Windows/amd64、i9-12900H 上，以相同 payload 串行采样五次并使用 `-benchmem`；local 三种模式为 Go 1.27，upstream 为 Go 1.26.7，运行器使用 `GOPROXY=off`，因此这些跨工具链数据仅作本地参考，不作性能或优劣结论。

`ns/op` 中位数：

| Benchmark | default | `sonic_stdjson` | `sonic_jsonv2` | upstream |
|---|---:|---:|---:|---:|
| Marshal small struct | 678.8 | 876.7 | 1475 | 420.8 |
| Unmarshal medium map | 14875 | 14975 | 23067 | 7061 |
| Valid medium JSON | 1327 | 2338 | 1476 | 764.2 |
| Get nested path | 426.8 | 1540 | 933.3 | 264.3 |

`B/op · allocs/op` 中位数：

| Benchmark | default | `sonic_stdjson` | `sonic_jsonv2` | upstream |
|---|---:|---:|---:|---:|
| Marshal small struct | 288 · 3 | 288 · 3 | 336 · 3 | 269 · 3 |
| Unmarshal medium map | 4736 · 129 | 4736 · 129 | 4956 · 148 | 5402 · 59 |
| Valid medium JSON | 0 · 0 | 0 · 0 | 0 · 0 | 0 · 0 |
| Get nested path | 164 · 2 | 164 · 2 | 164 · 2 | 40 · 2 |

复现：

```powershell
pwsh -NoProfile -File .\bench\run.ps1
```

## 测试与 fuzz

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

仓库有 18 个 fuzz target；提交的 fuzz corpus 总数为 7。
