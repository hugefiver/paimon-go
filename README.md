# Sonic-compatible JSON replacement

This repository provides a source-compatible replacement for
`github.com/bytedance/sonic` targeting the Sonic `v1.15.2` public API. Use it
from a consuming module by keeping your existing Sonic imports and adding a
`replace` directive:

```go
require github.com/bytedance/sonic v1.15.2

replace github.com/bytedance/sonic => <path-to-this-checkout>
```

The goal is API/source compatibility for users that need a portable
implementation. It does **not** promise performance close to upstream Sonic's
native/JIT implementation. The compatibility rule is: support for standard JSON
must not be worse than upstream Sonic. For behavior where upstream Sonic accepts
non-standard JSON, the default implementation chooses the faster or better local
strategy. The current raw `Valid`/`Get` paths default to Sonic-compatible
behavior because that path benchmarks substantially faster while still accepting
standard JSON. Use the `sonic_stdjson` build tag to force strict standard JSON
behavior for those raw entry points.

## Backends and import paths

- **Root package (`github.com/bytedance/sonic`)**: the default backend accepts
  standard JSON and uses Sonic-compatible raw JSON behavior for hot paths where
  it benchmarks faster than strict standard validation, especially `Valid` and
  `Get`. Arbitrary Go value reflection (`Marshal`, `Unmarshal`, encoders, and
  decoders) falls back to the standard `encoding/json` v1 APIs.
- **`github.com/bytedance/sonic/fastjson`**: an explicit thin wrapper around
  the root package. Its exported types are aliases of the root types and its
  functions forward to the root implementation.
- **`github.com/bytedance/sonic/stdjsonv2`**: an explicit experimental backend.
  The required Go 1.27 default experiment set builds the real backend; explicit
  `$env:GOEXPERIMENT = "jsonv2"` does too; explicit
  `$env:GOEXPERIMENT = "none"` builds the deterministic disabled stub whose
  operational APIs return `ErrJSONv2ExperimentDisabled`.

`github.com/bytedance/sonic/loader` is out of scope for this replacement.

## Basic usage

Existing code can keep importing Sonic:

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
    fmt.Println(string(data))

    var user User
    if err := sonic.Unmarshal(data, &user); err != nil {
        panic(err)
    }
    fmt.Println(user.Name)

    fmt.Println(sonic.Valid([]byte(`{"name":"Ada","age":37}`)))

    node, err := sonic.Get([]byte(`{"users":[{"name":"Ada"}]}`), "users", 0, "name")
    if err != nil {
        panic(err)
    }
    text, _ := node.String()
    fmt.Println(text)

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

The `fastjson` subpackage can be used in the same style when downstream code
imports that path directly:

```go
import sonicfast "github.com/bytedance/sonic/fastjson"

ok := sonicfast.Valid([]byte(`{"ok":true}`))
_ = ok
```

The `stdjsonv2` subpackage is experimental; callers that explicitly build with
`GOEXPERIMENT=none` can detect the disabled stub:

```go
import "github.com/bytedance/sonic/stdjsonv2"

data, err := stdjsonv2.Marshal(map[string]bool{"ok": true})
if err == stdjsonv2.ErrJSONv2ExperimentDisabled {
    // GOEXPERIMENT=none selected the deterministic stub; use another backend.
}
_ = data
```

## Validation and comparison commands

Run these from the repository root unless a subdirectory is shown.

```powershell
go test ./... -count=1
```

Run the full test suite with the jsonv2 experiment enabled:

```powershell
$env:GOEXPERIMENT = "jsonv2"; go test ./... -count=1
```

Root package fuzz smoke:

```powershell
go test . -run=Fuzz -fuzz=FuzzValidParity -fuzztime=30s
```

Differential fuzz smoke against upstream Sonic v1.15.2:

```powershell
Push-Location .\difftest
go test -run=Fuzz -fuzz=FuzzUpstreamSonicParity -fuzztime=10s
Pop-Location
```

Benchmark comparison smoke:

```powershell
Push-Location .\bench
pwsh -NoProfile -File .\run.ps1
Pop-Location
```

The benchmark runner prints reproducible side-by-side measurements for the
local root/default backend, the local root `sonic_stdjson` build, the local `stdjsonv2` backend, and upstream Sonic
v1.15.2. These numbers are for comparison only; this project does not claim to
match upstream Sonic performance.

## Strict standard JSON mode

The default build keeps observed Sonic-compatible raw parser behavior for the
hot raw JSON entry points where that path is faster in this implementation. If
you prefer strict standard JSON behavior when Sonic and `encoding/json` disagree,
build with the `sonic_stdjson` tag:

```powershell
go test -tags sonic_stdjson ./... -count=1
```

With this tag, root `Valid`, `Unmarshal`, and `Get` reject non-standard inputs
such as raw control bytes inside strings or trailing garbage after the top-level
value. This trades some upstream Sonic parity and raw-path speed for strict JSON
semantics.

See [`docs/compatibility.md`](docs/compatibility.md) for detailed behavior and
known differences.
