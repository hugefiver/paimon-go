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

$oldExperiment = $env:GOEXPERIMENT

Push-Location $scriptDir
try {
    Invoke-Bench -Label "=== local root fastjson/default ===" -Arguments @(
        "test",
        "-modfile=go.local.mod",
        "./rootbench",
        "-bench=.",
        "-benchmem",
        "-run=^$"
    )

    $env:GOEXPERIMENT = "jsonv2"
    Invoke-Bench -Label "=== local stdjsonv2 ===" -Arguments @(
        "test",
        "-modfile=go.local.mod",
        "./localv2bench",
        "-bench=.",
        "-benchmem",
        "-run=^$"
    )

    if ($null -eq $oldExperiment) {
        Remove-Item Env:GOEXPERIMENT -ErrorAction SilentlyContinue
    } else {
        $env:GOEXPERIMENT = $oldExperiment
    }
    Invoke-Bench -Label "=== upstream sonic v1.15.2 ===" -Arguments @(
        "test",
        "./rootbench",
        "-bench=.",
        "-benchmem",
        "-run=^$"
    )
} finally {
    if ($null -eq $oldExperiment) {
        Remove-Item Env:GOEXPERIMENT -ErrorAction SilentlyContinue
    } else {
        $env:GOEXPERIMENT = $oldExperiment
    }
    Pop-Location
}
