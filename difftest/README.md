# Sonic differential fuzz harness

This directory contains a smoke differential harness for comparing this repository's `github.com/bytedance/sonic` replacement with the real upstream Sonic v1.15.2 module.

The repository itself uses the same module path as upstream Sonic, so a single Go module cannot import both implementations at once. To avoid that same-module-path collision, the harness is split into three modules:

- `local/` imports `github.com/bytedance/sonic v1.15.2` but uses `replace github.com/bytedance/sonic => ../..`, so its helper exercises the local checkout.
- `upstream/` imports `github.com/bytedance/sonic v1.15.2` without a replace directive, so its helper exercises the real upstream release.
- The top-level `difftest/` module runs both helpers and compares their JSON results.

The helper protocol sends fuzz payloads through stdin as JSON. The raw candidate bytes are base64 encoded in the request, which avoids Windows command-line length limits and avoids command-line quoting issues. Helpers write only the JSON result to stdout; the driver parses stdout only and captures stderr separately so diagnostic output cannot pollute the protocol.

## Commands

From `difftest/`:

```powershell
go test -run Test -count=1
go test -run=Fuzz -fuzz=FuzzUpstreamSonicParity -fuzztime=10s
```
