# Sonic 基准测试子项目

本目录是独立 Go module。四组使用**同一份** `rootbench` 源码和 fixture：

| 模式 | 来源 / 编译条件 | 工具链 |
|---|---|---|
| `local-default` | 本地 root，默认 backend | Go 1.27.x |
| `local-stdjson` | 本地 root，`sonic_stdjson` | Go 1.27.x |
| `local-jsonv2` | 本地 root，`sonic_jsonv2` + `GOEXPERIMENT=jsonv2` | Go 1.27.x |
| `upstream-native` | 上游 Sonic v1.15.2，原生 `sonic.go`，`APIKind=1` | Go 1.26.7 |

`go.local.mod` 将相同 module path `github.com/bytedance/sonic` replace 到仓库根目录；
`go.mod` 不带 replace，解析到上游 v1.15.2。所有 `go` 命令均使用
`-mod=readonly`、`GOPROXY=off` 和 `GONOPROXY=none`；显式覆盖继承的
`GONOPROXY`（包括通过 `GOPRIVATE` 得到的默认值），避免绕开代理直连 VCS。
缺失模块只会报离线查找失败，不会下载或修改 module 文件。
运行前必须已缓存依赖与 Go 1.26.7 工具链，且 PATH 上的本地 Go 是 1.27.x。
缺少缓存或版本不匹配时立即失败，不会把回退实现误记成原生 Sonic。

## 运行

从 `bench` 目录运行：

```powershell
pwsh -NoProfile -File .\run.ps1
# 快速走通所有模式（不适合做性能结论）：
pwsh -NoProfile -File .\run.ps1 -Rounds 1 -Benchtime 1x -Seed 42 -OutputDir "$env:TEMP\paimon-bench-smoke"
```

参数：`-Rounds` 默认 6、`-Benchtime` 默认 `250ms`（也支持 `1x`）、
`-Seed` 默认 `20260923`、`-BenchmarkPattern` 默认 `.`、`-OutputDir` 默认
`bench/results/<时间戳>`。输出目录必须尚不存在；可用唯一目录名重试。
`-BenchmarkPattern` 是 Go 的 `-test.bench` 正则；缩小范围时，仍会对**全部**
四组执行构建与正确性检查。不要将 `1x` 结果用于性能判断。

计时前 runner 检查 `go version` / `GOPROXY` / `GONOPROXY` / `GOEXPERIMENT`、module 来源与
实际选中的 Go 源文件，并为四组分别预编译 test exe；每组 checks 与计时
进程显式设置 `BENCH_MODE`，由二进制内部验证运行时条件（上游还需 `APIKind=1`）。
runner 要求 checks 日志出现对应 `PROOF`，再比较 fixture 哈希并验证正确性；
任一失败都不会启动计时。每轮
按固定种子洗牌四组，串行启动**新的** test 进程（每组每轮 `-test.count=1`）；
每个叶子 benchmark 在进程内预热相关调用并在计时前重置 timer。
解码每次使用新目标；流式/复用类 benchmark 明确标识其复用条件。运行时固定
`GOMAXPROCS=1`、`GOGC=100`、`GOAMD64=v1`；退出时恢复调用者的工作目录及
`BENCH_MODE`、`GOPROXY`、`GONOPROXY`、`GOEXPERIMENT`、`GOTOOLCHAIN` 和上述三个环境变量。

输出目录只保留各组 build/source 证明及 checks 日志、逐轮原始
`ns/op`/`MB/s`（有设置 bytes 时）/`B/op`/`allocs/op`、`order.txt` 和
`summary.csv`。仅在本次新建的结果目录内，本次生成的四个 test exe 会在
成功或失败时由 `finally` 精确删除；已有结果目录绝不删除。runner 检查各组
各轮 benchmark 名称一致且都有样本，再按名称
计算 `ns/op` 中位数、范围和样本标准差 / 均值的 CV（百分比；仅一轮时为 0）。
请先看逐轮波动：中位数和 CV 不是显著性检验。`SetBytes` 表示输入 JSON 的
字节数或输出 JSON 的标称字节数；Get / Searcher 可能只扫描部分输入，故不
给它们标注吞吐量。复用流 decoder 一次操作包含 100 条消息。

## 比较范围

local 与 upstream 使用不同 Go 版本，因此结果**同时包含工具链与实现差异**，
不是严格的同工具链性能归因。`APIKind=1` 只对 upstream 验证 native 选择；
本地 root 的 APIKind 不能说明是否 JIT，构建 tag 也不影响所有 AST / 子包
workload。`BenchmarkEncoder/StdStream` 和 `BenchmarkStringDecoder/StdDecoder`
是相同 fixture 的标准库基线，不应当成 Sonic 实现模式。CPU 频率、温度、后台
负载等仍会影响波动；如需发布级结论，应固定机器条件、检查原始数据并做进一步
统计。本地同工具链优化对照及原始日志另见
[docs/performance.md](../docs/performance.md)。
