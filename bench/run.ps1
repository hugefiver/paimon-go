$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

function Invoke-Bench {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Label,

        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    ""
    $Label
    & go @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Benchmark command failed with exit code ${LASTEXITCODE}: go $($Arguments -join ' ')"
    }
}

$hadExperiment = Test-Path Env:GOEXPERIMENT
$oldExperiment = $env:GOEXPERIMENT
$hadToolchain = Test-Path Env:GOTOOLCHAIN
$oldToolchain = $env:GOTOOLCHAIN

Push-Location $scriptDir
try {
    Invoke-Bench -Label "=== local root sonic/default ===" -Arguments @(
        "test",
        "-mod=readonly",
        "-modfile=go.local.mod",
        "-run=^$",
        "-bench=.",
        "-benchmem",
        "-count=3",
        "./rootbench"
    )

    Invoke-Bench -Label "=== local root stdjson tag ===" -Arguments @(
        "test",
        "-mod=readonly",
        "-modfile=go.local.mod",
        "-tags=sonic_stdjson",
        "-run=^$",
        "-bench=.",
        "-benchmem",
        "-count=3",
        "./rootbench"
    )

    $env:GOEXPERIMENT = "jsonv2"
    Invoke-Bench -Label "=== local root jsonv2 tag ===" -Arguments @(
        "test",
        "-mod=readonly",
        "-modfile=go.local.mod",
        "-tags=sonic_jsonv2",
        "-run=^$",
        "-bench=.",
        "-benchmem",
        "-count=3",
        "./rootbench"
    )

    if ($hadExperiment) {
        $env:GOEXPERIMENT = $oldExperiment
    } else {
        Remove-Item Env:GOEXPERIMENT -ErrorAction SilentlyContinue
    }
    $env:GOTOOLCHAIN = "go1.26.7"
    Remove-Item Env:GOEXPERIMENT -ErrorAction SilentlyContinue
    Invoke-Bench -Label "=== upstream sonic v1.15.2 / Go 1.26.7 ===" -Arguments @(
        "test",
        "-mod=readonly",
        "-run=^$",
        "-bench=.",
        "-benchmem",
        "-count=3",
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
    Pop-Location
}
