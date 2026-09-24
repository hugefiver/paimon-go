[CmdletBinding()]
param(
    [ValidateRange(1, 100)][int]$Rounds = 6,
    [string]$Benchtime = '250ms',
    [int]$Seed = 20260923,
    [string]$BenchmarkPattern = '.',
    [string]$OutputDir = ''
)

$ErrorActionPreference = 'Stop'
if ($Benchtime -notmatch '^(?:[1-9]\d*x|(?:\d+(?:\.\d+)?)(?:ns|us|ms|s|m|h))$') {
    throw "Invalid -Benchtime '$Benchtime' (use e.g. 250ms or 1x)"
}
if (-not $BenchmarkPattern) { throw '-BenchmarkPattern must not be empty' }

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$modes = @(
    [pscustomobject]@{ Name = 'local-default'; Version = 'go1.27'; Mod = @('-modfile=go.local.mod'); Tags = @(); Experiment = ''; Toolchain = 'local' },
    [pscustomobject]@{ Name = 'local-stdjson'; Version = 'go1.27'; Mod = @('-modfile=go.local.mod'); Tags = @('-tags=sonic_stdjson'); Experiment = ''; Toolchain = 'local' },
    [pscustomobject]@{ Name = 'local-jsonv2'; Version = 'go1.27'; Mod = @('-modfile=go.local.mod'); Tags = @('-tags=sonic_jsonv2'); Experiment = 'jsonv2'; Toolchain = 'local' },
    [pscustomobject]@{ Name = 'upstream-native'; Version = 'go1.26.7'; Mod = @(); Tags = @(); Experiment = ''; Toolchain = 'go1.26.7' }
)

function Set-BenchMode($mode) {
    $env:BENCH_MODE = $mode.Name
    $env:GOTOOLCHAIN = $mode.Toolchain
    if ($mode.Experiment) { $env:GOEXPERIMENT = $mode.Experiment }
    else { Remove-Item Env:GOEXPERIMENT -ErrorAction SilentlyContinue }
}

function Invoke-Logged([string]$executable, [string[]]$arguments, [string]$log) {
    & $executable @arguments 2>&1 | Tee-Object -FilePath $log -Append
    if ($LASTEXITCODE -ne 0) {
        throw "Command failed (exit $LASTEXITCODE), see ${log}: $executable $($arguments -join ' ')"
    }
}

function Assert-Mode($mode, [string]$log) {
    if ($env:GOPROXY -ne 'off' -or ((& go env GOPROXY) -join '').Trim() -ne 'off' -or $LASTEXITCODE -ne 0) {
        throw "GOPROXY is not off for $($mode.Name)"
    }
    if ($env:GONOPROXY -ne 'none' -or ((& go env GONOPROXY) -join '').Trim() -ne 'none' -or $LASTEXITCODE -ne 0) {
        throw "GONOPROXY is not none for $($mode.Name)"
    }
    $version = ((& go version) -join '').Trim()
    if ($LASTEXITCODE -ne 0 -or $version -notmatch "^go version $([regex]::Escape($mode.Version))(?:\.| )") {
        throw "Wrong Go toolchain for $($mode.Name): $version (want $($mode.Version))"
    }
    if ($mode.Name -eq 'upstream-native' -and $version -notmatch '^go version go1\.26\.7 ') {
        throw "Upstream requires exactly Go 1.26.7: $version"
    }
    $experiment = ((& go env GOEXPERIMENT) -join '').Trim()
    if ($LASTEXITCODE -ne 0 -or $experiment -ne $mode.Experiment) {
        throw "Wrong GOEXPERIMENT for $($mode.Name): '$experiment'"
    }
    "PROOF mode=$($mode.Name) GOPROXY=off GONOPROXY=none $version GOEXPERIMENT='$experiment' GOTOOLCHAIN=$($mode.Toolchain) GOMAXPROCS=$env:GOMAXPROCS GOGC=$env:GOGC GOAMD64=$env:GOAMD64" | Tee-Object -FilePath $log

    $module = & go list -mod=readonly @($mode.Mod) -m -json github.com/bytedance/sonic | ConvertFrom-Json
    if ($LASTEXITCODE -ne 0) { throw "Module lookup failed for $($mode.Name)" }
    if ($mode.Name -eq 'upstream-native') {
        if ($module.Version -ne 'v1.15.2' -or $module.Replace) { throw 'Upstream must resolve to unmodified Sonic v1.15.2' }
    } elseif (-not $module.Replace -or [IO.Path]::GetFullPath($module.Replace.Dir) -ne [IO.Path]::GetFullPath((Join-Path $scriptDir '..'))) {
        throw "Local mode did not resolve to this repository: $($module.Replace.Dir)"
    }
    "MODULE $($module.Path) $($module.Version) replace=$($module.Replace.Dir)" | Tee-Object -FilePath $log -Append

    $files = (& go list -mod=readonly @($mode.Mod) @($mode.Tags) '-f={{.GoFiles}}' github.com/bytedance/sonic) -join ' '
    if ($LASTEXITCODE -ne 0) { throw "Source selection failed for $($mode.Name)" }
    "SELECTED root=$files" | Tee-Object -FilePath $log -Append
    if ($mode.Name -eq 'upstream-native') {
        if ($files -notmatch '\bsonic\.go\b' -or $files -match '\bcompat\.go\b') {
            throw "Upstream did not select native Sonic source: $files"
        }
    } else {
        $want = if ($mode.Name -eq 'local-jsonv2') { 'backend_select_jsonv2.go' } else { 'backend_select_default.go' }
        if ($files -notmatch [regex]::Escape($want) -or ($mode.Name -ne 'local-jsonv2' -and $files -match 'backend_select_jsonv2\.go')) {
            throw "Wrong local backend source for $($mode.Name): $files"
        }
        $compat = (& go list -mod=readonly @($mode.Mod) @($mode.Tags) '-f={{.GoFiles}}' github.com/bytedance/sonic/internal/compatmode) -join ' '
        if ($LASTEXITCODE -ne 0) { throw "Compat mode lookup failed for $($mode.Name)" }
        "SELECTED compatmode=$compat" | Tee-Object -FilePath $log -Append
        $want = if ($mode.Name -eq 'local-stdjson') { 'mode_stdjson.go' } else { 'mode_sonic.go' }
        if ($compat -notmatch [regex]::Escape($want)) { throw "Wrong compat source for $($mode.Name): $compat" }
    }
}

$saved = @{}
foreach ($key in @('BENCH_MODE', 'GOEXPERIMENT', 'GOTOOLCHAIN', 'GOPROXY', 'GONOPROXY', 'GOMAXPROCS', 'GOGC', 'GOAMD64')) {
    $saved[$key] = @{ Present = Test-Path "Env:$key"; Value = [Environment]::GetEnvironmentVariable($key) }
}
$locationPushed = $false
$outputCreated = $false
$builtBinaries = [System.Collections.Generic.List[string]]::new()
try {
    $env:GOPROXY = 'off'
    $env:GONOPROXY = 'none'
    $env:GOMAXPROCS = '1'
    $env:GOGC = '100'
    $env:GOAMD64 = 'v1'
    Push-Location $scriptDir
    $locationPushed = $true
    if (-not $OutputDir) { $OutputDir = Join-Path $scriptDir ('results/' + (Get-Date -Format 'yyyyMMdd-HHmmss')) }
    $OutputDir = [IO.Path]::GetFullPath($OutputDir)
    if (Test-Path -LiteralPath $OutputDir) { throw "Output directory already exists: $OutputDir" }
    $parent = Split-Path -Parent $OutputDir
    if (-not (Test-Path -LiteralPath $parent)) { New-Item -ItemType Directory -Path $parent -Force | Out-Null }
    New-Item -ItemType Directory -Path $OutputDir | Out-Null
    $outputCreated = $true
    "Results: $OutputDir"

    # No timings until every binary has been built, its source selection checked,
    # and its fixture/correctness tests have passed.
    $fixtureProof = $null
    foreach ($mode in $modes) {
        Set-BenchMode $mode
        $buildLog = Join-Path $OutputDir "$($mode.Name)-build.log"
        Assert-Mode $mode $buildLog
        $binary = Join-Path $OutputDir "$($mode.Name).test.exe"
        $builtBinaries.Add($binary)
        $build = @('test', '-c', '-mod=readonly') + @($mode.Mod) + @($mode.Tags) + @('-o', $binary, './rootbench')
        Invoke-Logged 'go' $build $buildLog
        $checksLog = Join-Path $OutputDir "$($mode.Name)-checks.log"
        Invoke-Logged $binary @('-test.run=^Test(BenchmarkEnvironment|BenchmarkFixtures|WorkloadFixtures|BasicEquivalence)$', '-test.v') $checksLog
        $modeProof = @(Get-Content -LiteralPath $checksLog | Select-String -SimpleMatch "PROOF mode=$($mode.Name) runtime=")
        if ($modeProof.Count -ne 1 -or ($mode.Name -eq 'upstream-native' -and $modeProof[0].Line -notmatch '\bAPIKind=1\b')) {
            throw "Missing or wrong in-process mode proof in $checksLog"
        }
        $proof = @((Get-Content -LiteralPath $checksLog | Select-String 'FIXTURE_SHA256 \S+=[0-9a-f]+ bytes=\d+' | ForEach-Object { $_.Matches[0].Value } | Sort-Object) -join '|')
        if (-not $proof) { throw "No fixture proof in $checksLog" }
        if ($null -ne $fixtureProof -and $proof -ne $fixtureProof) { throw "Fixture hashes differ in $checksLog" }
        $fixtureProof = $proof
    }

    $random = [Random]::new($Seed)
    $order = @("seed=$Seed rounds=$Rounds benchtime=$Benchtime pattern=$BenchmarkPattern")
    $samples = @{}
    $expectedNames = $null
    $previousOrder = $null
    foreach ($mode in $modes) { $samples[$mode.Name] = @{} }
    for ($round = 1; $round -le $Rounds; $round++) {
        $shuffled = @($modes)
        for ($i = $shuffled.Count - 1; $i -gt 0; $i--) {
            $j = $random.Next($i + 1)
            $tmp = $shuffled[$i]; $shuffled[$i] = $shuffled[$j]; $shuffled[$j] = $tmp
        }
        if (($shuffled.Name -join ',') -eq $previousOrder) {
            $shuffled = @($shuffled[1..($shuffled.Count - 1)]) + $shuffled[0]
        }
        $previousOrder = $shuffled.Name -join ','
        $order += 'round-{0:D2}: {1}' -f $round, ($shuffled.Name -join ', ')
        foreach ($mode in $shuffled) {
            Set-BenchMode $mode
            $binary = Join-Path $OutputDir "$($mode.Name).test.exe"
            $log = Join-Path $OutputDir ('{0}-round-{1:D2}.log' -f $mode.Name, $round)
            Invoke-Logged $binary @('-test.run=^$', "-test.bench=$BenchmarkPattern", "-test.benchtime=$Benchtime", '-test.count=1', '-test.benchmem', '-test.cpu=1') $log
            $names = @()
            foreach ($line in (Get-Content -LiteralPath $log)) {
                if ($line -match '^Benchmark(?<name>\S+?)(?:-\d+)?\s+\d+\s+(?<ns>\d+(?:\.\d+)?)\s+ns/op\b') {
                    $name = $Matches.name
                    $names += $name
                    if (-not $samples[$mode.Name].ContainsKey($name)) { $samples[$mode.Name][$name] = [System.Collections.Generic.List[double]]::new() }
                    $samples[$mode.Name][$name].Add([double]::Parse($Matches.ns, [Globalization.CultureInfo]::InvariantCulture))
                }
            }
            $names = @($names | Sort-Object)
            if ($names.Count -eq 0 -or (@($names | Select-Object -Unique)).Count -ne $names.Count) { throw "Missing or duplicate benchmark samples in $log" }
            if ($null -eq $expectedNames) { $expectedNames = $names -join '|' }
            if (($names -join '|') -ne $expectedNames) { throw "Different benchmark set in $log" }
        }
    }
    $order | Set-Content -LiteralPath (Join-Path $OutputDir 'order.txt')

    $summary = foreach ($mode in $modes) {
        foreach ($name in ($samples[$mode.Name].Keys | Sort-Object)) {
            $values = @($samples[$mode.Name][$name] | Sort-Object)
            if ($values.Count -ne $Rounds) { throw "Missing rounds for $($mode.Name)/$name" }
            $mean = ($values | Measure-Object -Average).Average
            $squares = 0.0
            foreach ($v in $values) { $squares += [math]::Pow($v - $mean, 2) }
            $cv = if ($values.Count -gt 1) { 100 * [math]::Sqrt($squares / ($values.Count - 1)) / $mean } else { 0 }
            $middle = [int][math]::Floor($values.Count / 2)
            $median = if ($values.Count % 2) { $values[$middle] } else { ($values[$middle - 1] + $values[$middle]) / 2 }
            [pscustomobject]@{
                mode = $mode.Name; benchmark = $name; samples = $Rounds
                median_ns_op = [math]::Round($median, 2); cv_pct = [math]::Round($cv, 2)
                min_ns_op = $values[0]; max_ns_op = $values[-1]
            }
        }
    }
    $summary | Export-Csv -NoTypeInformation -LiteralPath (Join-Path $OutputDir 'summary.csv')
    $summary | Format-Table -AutoSize
    "Raw logs, build proofs, order, and summary: $OutputDir"
} finally {
    try {
        if ($outputCreated) {
            foreach ($binary in $builtBinaries) {
                if (Test-Path -LiteralPath $binary -PathType Leaf) {
                    Remove-Item -LiteralPath $binary -Force
                }
            }
        }
    } finally {
        foreach ($key in $saved.Keys) {
            if ($saved[$key].Present) { [Environment]::SetEnvironmentVariable($key, $saved[$key].Value) }
            else { Remove-Item "Env:$key" -ErrorAction SilentlyContinue }
        }
        if ($locationPushed) { Pop-Location }
    }
}
