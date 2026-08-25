$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

function Invoke-Bench {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Label,

        [Parameter(Mandatory = $true)]
        [string[]]$Arguments,

        [Parameter(Mandatory = $true)]
        [string]$ExpectedGoVersion
    )

    ""
    $Label
    Assert-BenchPreflight -Label $Label -ExpectedGoVersion $ExpectedGoVersion
    & go @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Benchmark command failed with exit code ${LASTEXITCODE}: go $($Arguments -join ' ')"
    }
}

function Assert-BenchPreflight {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Label,

        [Parameter(Mandatory = $true)]
        [string]$ExpectedGoVersion
    )

    if ($env:GOPROXY -ne 'off') {
        throw "Benchmark preflight failed for ${Label}: GOPROXY must be off, got '$($env:GOPROXY)'"
    }

    $goProxy = (& go env GOPROXY)
    if ($LASTEXITCODE -ne 0) {
        throw "Benchmark preflight failed for ${Label}: go env GOPROXY exited with ${LASTEXITCODE}"
    }
    $goProxy = ($goProxy -join "`n").Trim()
    if ($goProxy -ne 'off') {
        throw "Benchmark preflight failed for ${Label}: go env GOPROXY returned '$goProxy', want 'off'"
    }

    $goVersion = (& go version)
    if ($LASTEXITCODE -ne 0) {
        throw "Benchmark preflight failed for ${Label}: go version exited with ${LASTEXITCODE}"
    }
    $goVersion = ($goVersion -join "`n").Trim()
    if ($goVersion -notlike "*${ExpectedGoVersion}*") {
        throw "Benchmark preflight failed for ${Label}: go version '$goVersion' does not contain '$ExpectedGoVersion'"
    }

    "PROOF: label=${Label}; GOPROXY=off; go version=${goVersion}"
}

$hadExperiment = Test-Path Env:GOEXPERIMENT
$oldExperiment = $env:GOEXPERIMENT
$hadToolchain = Test-Path Env:GOTOOLCHAIN
$oldToolchain = $env:GOTOOLCHAIN
$hadProxy = Test-Path Env:GOPROXY
$oldProxy = $env:GOPROXY
$locationPushed = $false

try {
    # Every block is offline. A missing cache must fail rather than cause a
    # benchmark run to download modules or a toolchain.
    $env:GOPROXY = "off"
    Push-Location $scriptDir
    $locationPushed = $true

    Invoke-Bench -Label "=== local root sonic/default ===" -ExpectedGoVersion "go1.27" -Arguments @(
        "test",
        "-mod=readonly",
        "-modfile=go.local.mod",
        "-run=^$",
        "-bench=.",
        "-benchmem",
        "-count=5",
        "./rootbench"
    )

    Invoke-Bench -Label "=== local root stdjson tag ===" -ExpectedGoVersion "go1.27" -Arguments @(
        "test",
        "-mod=readonly",
        "-modfile=go.local.mod",
        "-tags=sonic_stdjson",
        "-run=^$",
        "-bench=.",
        "-benchmem",
        "-count=5",
        "./rootbench"
    )

    $env:GOEXPERIMENT = "jsonv2"
    Invoke-Bench -Label "=== local root jsonv2 tag ===" -ExpectedGoVersion "go1.27" -Arguments @(
        "test",
        "-mod=readonly",
        "-modfile=go.local.mod",
        "-tags=sonic_jsonv2",
        "-run=^$",
        "-bench=.",
        "-benchmem",
        "-count=5",
        "./rootbench"
    )

    if ($hadExperiment) {
        $env:GOEXPERIMENT = $oldExperiment
    } else {
        Remove-Item Env:GOEXPERIMENT -ErrorAction SilentlyContinue
    }
    $env:GOTOOLCHAIN = "go1.26.7"
    Remove-Item Env:GOEXPERIMENT -ErrorAction SilentlyContinue
    Invoke-Bench -Label "=== upstream sonic v1.15.2 / Go 1.26.7 ===" -ExpectedGoVersion "go1.26.7" -Arguments @(
        "test",
        "-mod=readonly",
        "-run=^$",
        "-bench=.",
        "-benchmem",
        "-count=5",
        "./rootbench"
    )
} finally {
    if ($hadExperiment) {
        $env:GOEXPERIMENT = $oldExperiment
    } else {
        Remove-Item Env:GOEXPERIMENT -ErrorAction SilentlyContinue
    }
    if ($hadToolchain) {
        $env:GOTOOLCHAIN = $oldToolchain
    } else {
        Remove-Item Env:GOTOOLCHAIN -ErrorAction SilentlyContinue
    }
    if ($hadProxy) {
        $env:GOPROXY = $oldProxy
    } else {
        Remove-Item Env:GOPROXY -ErrorAction SilentlyContinue
    }
    if ($locationPushed) {
        Pop-Location
    }
}
