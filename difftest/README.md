# Sonic differential fuzz harness

This directory contains a smoke differential harness for comparing this repository's `github.com/bytedance/sonic` replacement with the real upstream Sonic v1.15.2 module. It is intended for the default mode, where non-standard Sonic parser behavior is enabled for hot raw JSON paths because it is faster than strict standard validation in this implementation.

The repository itself uses the same module path as upstream Sonic, so a single Go module cannot import both implementations at once. To avoid that same-module-path collision, the harness is split into three modules:

- `local/` imports `github.com/bytedance/sonic v1.15.2` but uses `replace github.com/bytedance/sonic => ../..`, so its helper exercises the local checkout.
- `upstream/` imports `github.com/bytedance/sonic v1.15.2` without a replace directive, so its helper exercises the real upstream release.
- The top-level `difftest/` module runs both helpers and compares their JSON results.

The helper protocol sends fuzz payloads through stdin as JSON. The raw candidate bytes are base64 encoded in the request, which avoids Windows command-line length limits and avoids command-line quoting issues. Helpers write only the JSON result to stdout; the driver parses stdout only and captures stderr separately so diagnostic output cannot pollute the protocol.

The driver builds direct helper executables once per test process in an owned OS temporary directory using `go build -mod=readonly`, then invokes those executables directly with `exec.CommandContext`. Fuzz workers inherit that temporary directory instead of rebuilding helpers. Each helper process has a 30-second deadline and a bounded `Cmd.WaitDelay`, so a timeout supervises the actual helper executable. Differential fuzz candidates are limited to 64 KiB before base64 encoding, and both the encoded JSON request and helper JSON response are limited to 1 MiB. The helpers bound stdin reads to 1 MiB and return an empty result for malformed or oversized protocol requests. Ordinary candidates compare every result field strictly. The documented raw-control-in-string difference is an explicit oracle: local Sonic must accept and normalize it while upstream rejects it, while both implementations' raw AST entry points agree after invalid UTF-8 in string tokens is normalized to U+FFFD.

## Commands

From `difftest/`:

```powershell
go test -mod=readonly -run Test -count=1
go test -mod=readonly -run=Fuzz -fuzz=FuzzUpstreamSonicParity -fuzztime=10s
```
