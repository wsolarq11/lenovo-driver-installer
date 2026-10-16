<#
.SYNOPSIS
Offline verification gate for the Lenovo driver installer Go migration.

.DESCRIPTION
Runs Go build/test/vet/gofmt, PowerShell syntax checks for the WPF layer, CLI
smoke tests, and workspace hygiene checks. It never calls the real Lenovo API,
downloads drivers, or installs anything.
#>
[CmdletBinding()]
param(
    [string]$GoExe = '',
    [switch]$FixBom
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $repoRoot

$failed = @()
$total = 0

# Read-GateText unifies every gate input read to explicit UTF-8 decoding.
# Windows PowerShell 5.1 reads BOM-less UTF-8 with the ANSI code page; a
# match against non-ASCII literals would fail silently. Every file the gate
# reads goes through this helper so the verdict stays the same on both hosts.
function Read-GateText {
    param([string]$LiteralPath)
    return [System.IO.File]::ReadAllText($LiteralPath, [System.Text.Encoding]::UTF8)
}

function Assert-Step {
    param(
        [string]$Name,
        [scriptblock]$Body
    )
    $script:total++
    Write-Host "==> $Name"
    try {
        & $Body
        Write-Host "PASS $Name"
    } catch {
        Write-Host "FAIL ${Name}: $($_.Exception.Message)"
        $script:failed += $Name
    }
}

. (Join-Path $PSScriptRoot 'lib\go-toolchain.ps1')
. (Join-Path $PSScriptRoot 'fix-bom.ps1')

# Go discovery is the one prerequisite every later step stands on, so it runs as
# a named step: a missing toolchain used to kill the gate with a raw throw
# before any step had run, leaving no step name and no failure count. The gate
# then stops instead of cascading "term '' is not recognized" through every
# step that invokes the toolchain.
$resolvedGoExe = ''
Assert-Step 'Go toolchain is discoverable' {
    $script:resolvedGoExe = Resolve-GoExe -GoExe $GoExe
}
if (-not $resolvedGoExe) {
    Write-Host 'The gate stopped: no Go toolchain to run its steps with.'
    Write-Host "Verify summary: $total steps, $($failed.Count) failed"
    Write-Host ("Failed: {0}" -f ($failed -join ', '))
    exit 1
}
$GoExe = $resolvedGoExe

# -FixBom is an explicit local convenience: restore drifted BOM before the gate
# runs. CI never passes this switch, so a BOM-less non-ASCII ps1 fails the gate
# there instead of being silently auto-repaired into a green run.
if ($FixBom) {
    $fixed = Restore-Ps1Bom -RepoRoot $repoRoot
    if ($fixed.Count -gt 0) {
        Write-Host "Restored UTF-8 BOM on: $($fixed -join ', ')"
    } else {
        Write-Host 'UTF-8 BOM consistent: nothing to fix.'
    }
}

Assert-Step 'Go build' {
    & $GoExe build ./...
    if ($LASTEXITCODE -ne 0) { throw "go build exited $LASTEXITCODE" }
}

Assert-Step 'Go test + coverage floor (side-effect packages)' {
    $coverageOutput = (& $GoExe test -cover ./... 2>&1)
    if ($LASTEXITCODE -ne 0) {
        # Surface the failing test's own output instead of a bare exit code:
        # a silent red is the expensive drift this gate exists to prevent.
        $coverageOutput | ForEach-Object { Write-Host $_ }
        throw "go test -cover exited $LASTEXITCODE"
    }
    $floors = @{
        'lenovo-driver/internal/inventory' = 8
        'lenovo-driver/internal/app'       = 22
        'lenovo-driver/internal/install'   = 45
        'lenovo-driver/internal/compare'   = 50
        'lenovo-driver/internal/audit'     = 50
        'lenovo-driver/internal/download'  = 35
        'lenovo-driver/internal/plan'      = 65
        'lenovo-driver/internal/api'       = 55
        # The gate's own rules are checked code, not unchecked prose. A rule whose
        # red-light test is deleted starts passing everything silently — the one
        # failure mode that leaves no trace anywhere else in this gate.
        'lenovo-driver/internal/gate'      = 70
    }
    $below = @()
    foreach ($line in @($coverageOutput)) {
        $text = [string]$line
        if ($text -notmatch '^ok\s+(\S+)\s+.*coverage: (\d+(?:\.\d+)?)% of statements') { continue }
        $pkg = $Matches[1]
        if (-not $floors.ContainsKey($pkg)) { continue }
        $cov = [double]$Matches[2]
        if ($cov -lt $floors[$pkg]) {
            $below += ("{0}={1}% (floor {2}%)" -f $pkg, $cov, $floors[$pkg])
        }
    }
    if ($below.Count -gt 0) {
        throw "side-effect package coverage below floor: $($below -join ', ')"
    }
}

Assert-Step 'Go vet' {
    & $GoExe vet ./...
    if ($LASTEXITCODE -ne 0) { throw "go vet exited $LASTEXITCODE" }
}

Assert-Step 'gofmt' {
    $unformatted = & $GoExe fmt ./...
    if ($LASTEXITCODE -ne 0) { throw "go fmt exited $LASTEXITCODE" }
    if ($unformatted) { throw "gofmt changed: $($unformatted -join ', ')" }
}

Assert-Step 'Go build (legacyps)' {
    & $GoExe build -tags legacyps ./...
    if ($LASTEXITCODE -ne 0) { throw "go build -tags legacyps exited $LASTEXITCODE" }
}

Assert-Step 'Go vet (legacyps)' {
    & $GoExe vet -tags legacyps ./...
    if ($LASTEXITCODE -ne 0) { throw "go vet -tags legacyps exited $LASTEXITCODE" }
}

Assert-Step 'Go source file size (prod <= 500)' {
    $over = @()
    foreach ($dir in @('cmd', 'internal')) {
        Get-ChildItem -LiteralPath (Join-Path $repoRoot $dir) -Recurse -Filter '*.go' -File |
            Where-Object { $_.Name -notlike '*_test.go' } |
            ForEach-Object {
                $count = @(((Read-GateText $_.FullName) -replace '\r?\n$', '') -split "\r?\n").Count
                if ($count -gt 500) { $over += ("{0} = {1} lines" -f $_.FullName, $count) }
            }
    }
    if ($over.Count -gt 0) { throw "production Go file exceeds 500 lines: $($over -join ', ')" }
}

Assert-Step 'PowerShell UTF-8 BOM (non-ASCII ps1 must carry BOM)' {
    $missing = @()
    foreach ($f in (Get-ProjectPs1Files -RepoRoot $repoRoot)) {
        if (Test-NeedsBom -File $f) { $missing += $f.FullName }
    }
    if ($missing.Count -gt 0) {
        throw "BOM-less non-ASCII ps1 (Windows PowerShell 5.1 will mis-decode): $($missing -join ', '). Fix with: .\scripts\fix-bom.ps1 or .\scripts\verify.ps1 -FixBom"
    }
}

Assert-Step 'every project file read names its encoding (host-independent)' {
    # Windows PowerShell 5.1 decodes BOM-less UTF-8 with the ANSI code page
    # while pwsh 7 defaults to UTF-8: the same file can match on one host and
    # not on the other, and a negative assertion then fails SILENTLY (proven by
    # injecting forbidden text into window.xaml — 5.1 read it as absent). So
    # this audits the readers, not the files: every non-comment Get-Content in
    # the repository must state -Encoding, and the gate's own reads go through
    # Read-GateText. The literal is split so this step does not match itself.
    $gc = 'Get-' + 'Content'
    $bare = @()
    foreach ($f in (Get-ProjectPs1Files -RepoRoot $repoRoot)) {
        $src = Read-GateText $f.FullName
        foreach ($line in ($src -split "\r?\n")) {
            if ($line -match '^\s*#') { continue }
            if ($line -match $gc -and $line -notmatch '-Encoding') {
                $bare += ("{0}: {1}" -f $f.Name, $line.Trim())
            }
        }
    }
    if ($bare.Count -gt 0) {
        throw "reads without an explicit encoding (the verdict would flip per host): $($bare -join ' | ')"
    }
}

Assert-Step 'PowerShell parse' {
    foreach ($f in (Get-ProjectPs1Files -RepoRoot $repoRoot)) {
        $tokens = $null
        $errors = $null
        [System.Management.Automation.Language.Parser]::ParseFile(
            $f.FullName,
            [ref]$tokens,
            [ref]$errors
        ) | Out-Null
        if ($errors) {
            $errors | Format-List | Out-String | Write-Host
            throw "parse failed: $($f.FullName)"
        }
    }
}

Assert-Step 'WPF argument quoting (shared golden)' {
    . (Join-Path $repoRoot 'wpf\worker.ps1')
    $casesPath = Join-Path $repoRoot 'internal\app\testdata\windows_argument_quoting.json'
    $cases = @((Read-GateText $casesPath | ConvertFrom-Json).cases)
    if ($cases.Count -lt 10) { throw "shared quoting golden too small: $($cases.Count) cases" }
    foreach ($case in $cases) {
        $got = ConvertTo-CommandLineArgument -Argument ([string]$case.input)
        if ($got -ne $case.token) {
            throw "quoting drift on '$($case.name)': got '$got', want '$($case.token)'"
        }
    }
}

Assert-Step 'Legacy PowerShell files removed' {
    $legacyFiles = @(
        'install_lenovo_drivers.ps1',
        'lenovo_driver_core.ps1',
        'lenovo_driver_core.tests.ps1',
        'lenovo_installer_improvement_plan.md'
    )
    foreach ($file in $legacyFiles) {
        if (Test-Path -LiteralPath (Join-Path $repoRoot $file)) {
            throw "legacy file remains: $file"
        }
    }
}

Assert-Step 'CLI build to temp' {
    $script:SmokeDir = Join-Path $env:TEMP 'lenovo-driver-verify'
    if (Test-Path -LiteralPath $script:SmokeDir) { Remove-Item -LiteralPath $script:SmokeDir -Recurse -Force }
    New-Item -ItemType Directory -Path $script:SmokeDir | Out-Null
    & $GoExe build -o (Join-Path $script:SmokeDir 'lenovo-driver.exe') ./cmd/lenovo-driver
    if ($LASTEXITCODE -ne 0) { throw "go build smoke exited $LASTEXITCODE" }
    $script:SmokeExe = Join-Path $script:SmokeDir 'lenovo-driver.exe'
}

Assert-Step 'CLI help' {
    & $script:SmokeExe -Help | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Help exited $LASTEXITCODE" }
}

# ── Go source checks ─────────────────────────────────────────────────────────
# Six rules used to be PowerShell regexes over Go source text. Text matching
# cannot tell a package qualifier from a local variable that happens to share
# its name, cannot see a block-form `const (...)` declaration at all, and reads
# guidance prose ("do not run pnputil /delete-driver") as a violation. Those
# rules now live in internal/gate, driven by go/parser + go/types, which resolve
# every name to the object it actually names and let the compiler own what it
# can express. cmd/gatecheck is the entry point; its registry is the single list
# of checks. Each name becomes its own gate step, so the FAIL line and the
# summary count keep pointing at one rule.
Assert-Step 'Go source checks build' {
    $script:GateCheckExe = Join-Path $script:SmokeDir 'gatecheck.exe'
    & $GoExe build -o $script:GateCheckExe ./cmd/gatecheck
    if ($LASTEXITCODE -ne 0) { throw "go build gatecheck exited $LASTEXITCODE" }
    $script:SourceCheckNames = @(& $script:GateCheckExe -list -root $repoRoot)
    if ($LASTEXITCODE -ne 0) { throw "gatecheck -list exited $LASTEXITCODE" }
    if ($script:SourceCheckNames.Count -eq 0) { throw 'gatecheck -list returned no checks; the registry is empty' }
}

foreach ($sourceCheck in $script:SourceCheckNames) {
    Assert-Step $sourceCheck {
        & $script:GateCheckExe -check $sourceCheck -root $repoRoot -go $GoExe
        if ($LASTEXITCODE -ne 0) { throw 'the source check reported a violation (see the detail above)' }
    }
}

Assert-Step 'CLI exit codes match docs contract' {
    # Single source of truth: internal/app/help.go helpText. docs/spec/contracts.md
    # must carry the same numeric codes; drift fails the gate.
    $help = Read-GateText (Join-Path $repoRoot 'internal\app\help.go')
    if ($help -notmatch '(?ms)Exit codes:\r?\n(.*?)(\r?\n\r?\n)') {
        throw "help.go exit-code block not found"
    }
    $helpCodes = @([regex]::Matches($Matches[1], '(?m)^\s*(\d)\s+') | ForEach-Object { $_.Groups[1].Value })

    $contract = Read-GateText (Join-Path $repoRoot 'docs\spec\contracts.md')
    $contractCodes = @([regex]::Matches($contract, '(?m)^\|\s*`(\d)`\s*\|') | ForEach-Object { $_.Groups[1].Value })

    if ($helpCodes.Count -eq 0) { throw "help.go parsed zero exit codes" }
    if ($contractCodes.Count -eq 0) { throw "contracts.md parsed zero exit codes" }
    $diff = Compare-Object -ReferenceObject $helpCodes -DifferenceObject $contractCodes
    if ($diff) {
        $detail = ($diff | ForEach-Object { "$($_.SideIndicator)$($_.InputObject)" }) -join ','
        throw "exit-code drift (help vs contract): $detail"
    }
}

Assert-Step 'CLI artifact paths match docs contract' {
    # Single source of truth: internal/app/help.go Artifacts block. The three
    # audit artifacts (log/history/plan) must carry the same stable path in
    # docs/spec/contracts.md; a drifted copy fails the gate.
    $help = Read-GateText (Join-Path $repoRoot 'internal\app\help.go')
    $helpPaths = @([regex]::Matches($help, '(?m)^\s+(%LOCALAPPDATA%\\Lenovo\\DriverInstaller\\lenovo_driver_\S+\.(?:log|csv|txt))') | ForEach-Object { $_.Groups[1].Value })

    $contract = Read-GateText (Join-Path $repoRoot 'docs\spec\contracts.md')
    $contractPaths = @([regex]::Matches($contract, '(?m)^(%LOCALAPPDATA%\\Lenovo\\DriverInstaller\\lenovo_driver_\S+\.(?:log|csv|txt))') | ForEach-Object { $_.Groups[1].Value })

    if ($helpPaths.Count -lt 3) { throw "help.go parsed fewer than 3 audit artifact paths" }
    if ($contractPaths.Count -lt 3) { throw "contracts.md parsed fewer than 3 audit artifact paths" }
    $diff = Compare-Object -ReferenceObject ($helpPaths | Sort-Object) -DifferenceObject ($contractPaths | Sort-Object)
    if ($diff) {
        $detail = ($diff | ForEach-Object { "$($_.SideIndicator)$($_.InputObject)" }) -join ','
        throw "artifact-path drift (help vs contract): $detail"
    }
    # The offer file's name has a machine source of its own: the Go constant the
    # writer uses. help.go, contracts.md and the WPF layer are copies of it.
    $rollback = Read-GateText (Join-Path $repoRoot 'internal\app\rollback.go')
    $offerConst = [regex]::Match($rollback, 'rollbackOfferFileName\s*=\s*"([^"]+)"')
    if (-not $offerConst.Success) {
        throw 'rollbackOfferFileName is gone; the offer artifact lost its machine name'
    }
    $offerName = $offerConst.Groups[1].Value
    if ($help -notmatch [regex]::Escape($offerName)) {
        throw "help.go's Artifacts block does not declare $offerName although the GUI opens it"
    }
    # The WPF layer re-derives the plan and offer paths instead of asking the
    # engine, so it spells artifact names a second and third time. Those copies
    # are not exempt from the lock: every name the PowerShell layer writes must
    # be one the engine declares, or a renamed artifact leaves the GUI opening
    # a path nothing writes.
    $declaredNames = @([regex]::Matches($help, 'lenovo_driver_[A-Za-z0-9_]+\.(?:log|csv|txt|json)') |
        ForEach-Object { $_.Value } | Sort-Object -Unique)
    $psNames = @()
    foreach ($f in (Get-ProjectPs1Files -RepoRoot $repoRoot)) {
        $psNames += @([regex]::Matches((Read-GateText $f.FullName), 'lenovo_driver_[A-Za-z0-9_]+\.(?:log|csv|txt|json)') |
            ForEach-Object { $_.Value })
    }
    $stray = @($psNames | Sort-Object -Unique | Where-Object { $declaredNames -notcontains $_ })
    if ($stray.Count -gt 0) {
        throw "PowerShell spells artifact names the engine does not declare: $($stray -join ', ')"
    }
}

Assert-Step 'the documented JSON contracts match the engine' {
    # contracts.md carries the human copy of both JSON surfaces the WPF parses:
    # the driver view export (§2) and the rollback offer (§4). Their machine
    # sources are the struct tags in export.go, model.OSListEntry and
    # rollback.go. export_test.go's golden locks the encoder, not the human
    # copy, so the two could drift with every test still green. Compare the
    # field sets directly.
    $contract = Read-GateText (Join-Path $repoRoot 'docs\spec\contracts.md')
    $blocks = @([regex]::Matches($contract, '(?s)```json\r?\n(.*?)```') | ForEach-Object { $_.Groups[1].Value })
    if ($blocks.Count -ne 2) {
        throw "contracts.md must carry exactly two JSON contract blocks (driver view, rollback offer), found $($blocks.Count)"
    }
    $documented = @($blocks | ForEach-Object {
            [regex]::Matches($_, '"([A-Za-z0-9_]+)"\s*:') | ForEach-Object { $_.Groups[1].Value }
        })

    $engine = @()
    $export = Read-GateText (Join-Path $repoRoot 'internal\app\export.go')
    $engine += @([regex]::Matches($export, 'json:"([A-Za-z0-9_]+)') | ForEach-Object { $_.Groups[1].Value })
    $osEntry = [regex]::Match((Read-GateText (Join-Path $repoRoot 'internal\model\model.go')), '(?s)type OSListEntry struct \{(.*?)\n\}')
    if (-not $osEntry.Success) { throw 'model.OSListEntry is gone; the export OsList field lost its shape' }
    $engine += @([regex]::Matches($osEntry.Groups[1].Value, 'json:"([A-Za-z0-9_]+)') | ForEach-Object { $_.Groups[1].Value })
    $offer = Read-GateText (Join-Path $repoRoot 'internal\app\rollback.go')
    $engine += @([regex]::Matches($offer, 'json:"([A-Za-z0-9_]+)') | ForEach-Object { $_.Groups[1].Value })

    if ($engine.Count -lt 20) {
        throw "the engine scan found only $($engine.Count) JSON fields; the tag scan is broken"
    }
    $diff = Compare-Object -ReferenceObject (@($engine | Sort-Object -Unique)) -DifferenceObject (@($documented | Sort-Object -Unique))
    if ($diff) {
        $detail = ($diff | ForEach-Object { "$($_.SideIndicator)$($_.InputObject)" }) -join ','
        throw "JSON contract drift (engine vs contracts.md): $detail"
    }
}

$invalidCombinations = @(
    @('-CurrentOSOnly', '-LatestAcrossOS'),
    @('-CurrentOSOnly', '-TargetOS', '248'),
    @('-LatestAcrossOS', '-TargetOS', '248')
)
for ($i = 0; $i -lt $invalidCombinations.Count; $i++) {
    $name = "CLI invalid combination $($i + 1)"
    Assert-Step $name {
        $stdoutPath = Join-Path $script:SmokeDir "invalid-$($i + 1).out"
        $stderrPath = Join-Path $script:SmokeDir "invalid-$($i + 1).err"
        $process = Start-Process -FilePath $script:SmokeExe `
            -ArgumentList $invalidCombinations[$i] `
            -NoNewWindow -Wait -PassThru `
            -RedirectStandardOutput $stdoutPath `
            -RedirectStandardError $stderrPath
        if ($process.ExitCode -ne 2) { throw "expected exit 2, got $($process.ExitCode)" }
        # The CLI must put refusal text on stderr, never stdout. Read with the
        # gate's own UTF-8 helper so the check does not depend on the host's
        # default code page.
        $stdout = if (Test-Path -LiteralPath $stdoutPath) { Read-GateText $stdoutPath } else { '' }
        if ($stdout -match 'Error:') { throw "CLI error text leaked to stdout: $stdout" }
    }
}

Assert-Step 'the invariants spec carries every rule the code enforces' {
    # docs/spec/invariants.md is the semantic single source. A rule that lives
    # only in a gate is a rule nobody reads; a rule that lives only in the spec is
    # a rule nothing checks. Each sub-section must name the gate that forces it.
    $inv = Read-GateText (Join-Path $repoRoot 'docs\spec\invariants.md')
    # The gate's own text is the enforcement side of the pairing: invariants.md
    # cites gate step names, and a citation is only proof if the named step
    # actually exists in this file. Read it through the gate's own UTF-8 helper
    # so the check does not depend on the host's default code page.
    $selfText = Read-GateText $PSCommandPath
    $required = @(
        '### 3.1 自动安装集必须由实测证据支撑',
        '### 2.1 静默安装：开关派发只由端到端验证决定',
        '### 4.1 比较只有一个原语'
    )
    foreach ($heading in $required) {
        if ($inv -notmatch [regex]::Escape($heading)) {
            throw "invariants.md is missing '$heading'; the spec has drifted from the code"
        }
    }
    # The silent rule must keep naming the gate that forces it, or the two drift
    # apart silently.
    if ($inv -notmatch 'silent install dispatches on the package, not on the vendor column') {
        throw 'invariants.md no longer names the silent-dispatch gate'
    }
    if ($inv -notmatch 'one comparison primitive decides every version question') {
        throw 'invariants.md no longer names the comparison-primitive gate'
    }
    # The citations above name gate steps by title; a citation to a step that no
    # longer exists is the silent drift this whole gate family exists to catch.
    # Verify both directions: the spec names the step, and the step is declared.
    #
    # Step names come from two places now — the literal Assert-Step lines in this
    # file, and the Go registry internal/gate exposes through cmd/gatecheck -list.
    # Both are consulted, so a rule that moved from a PowerShell regex into Go
    # keeps its citation resolvable instead of silently dropping out of the audit.
    $declared = @([regex]::Matches($selfText, "Assert-Step '([^']+)'") | ForEach-Object { $_.Groups[1].Value })
    if ($script:GateCheckExe -and (Test-Path -LiteralPath $script:GateCheckExe)) {
        $declared += @(& $script:GateCheckExe -list -root $repoRoot)
    }
    # Every gate step the spec cites must be declared. The citations are read out
    # of the spec rather than listed here: a hardcoded list only guards the names
    # someone remembered to add, so renaming a step would leave the spec pointing
    # at a title nothing enforces and this check would not notice. A gate step
    # title is spelled in backticks, starts lowercase, holds a space, is long
    # enough to be a sentence, and carries no path or version punctuation -- which
    # is what tells it apart from the backticked status names, paths and version
    # strings the spec also uses.
    $cited = @()
    foreach ($m in [regex]::Matches($inv, '`([^`]+)`')) {
        $span = $m.Groups[1].Value
        if ($span -notmatch '^[a-z]') { continue }
        if ($span -notmatch ' ') { continue }
        if ($span.Length -lt 25) { continue }
        if ($span -match '[\\/.:%]') { continue }
        $cited += $span
    }
    if ($cited.Count -eq 0) {
        throw 'invariants.md cites no gate step titles; this audit lost its subjects'
    }
    foreach ($gateName in $cited) {
        if ($declared -notcontains $gateName) {
            throw "invariants.md cites the gate step '$gateName' but neither this file nor the Go registry declares it"
        }
    }
    # A sub-section may be added, but the six top-level invariants are what the
    # frozen decision record cites. Renumbering them would invalidate that record.
    $top = [regex]::Matches($inv, '(?m)^## (\d+)\.')
    if ($top.Count -ne 6) {
        throw "invariants.md has $($top.Count) top-level invariants, want 6; docs/decisions/distribution-frozen.md cites six"
    }
    for ($i = 0; $i -lt $top.Count; $i++) {
        if ([int]$top[$i].Groups[1].Value -ne ($i + 1)) {
            throw "top-level invariants are out of order at position $($i + 1)"
        }
    }
}

Assert-Step 'the CI trigger is public' {
    # A CI run on a private repository burns a runner and, when it fails for a
    # visibility reason, the fix is to make the repo public and run again. Doing
    # it the other way round turns a known failure into a probe and overwrites
    # the real reason between two runs. So visibility is decided before the
    # trigger, never after one fails.
    $workflows = Join-Path $repoRoot '.github\workflows'
    if (-not (Test-Path -LiteralPath $workflows)) {
        return
    }
    foreach ($wf in Get-ChildItem -LiteralPath $workflows -Filter '*.yml') {
        $text = Read-GateText $wf.FullName
        # Repository and organization visibility are the two settings that make
        # a workflow file unusable; a self-hosted runner or a manual dispatch is
        # unrelated and must not trip this.
        foreach ($setting in @('visibility', 'visibilityComment')) {
            $cm = [regex]::Match($text, "(?im)^\s*$setting\s*:\s*(\S+)\s*$")
            if ($cm.Success) {
                $value = $cm.Groups[1].Value.Trim("'`"")
                if ($value -match '^(?i)(private|internal)$') {
                    throw ("$($wf.Name) sets $setting to '$value'. Make the repository public before " +
                           "triggering CI: a private run cannot reach the steps it declares, and retrying " +
                           "after converting only overwrites the reason the first run gave")
                }
            }
        }
    }
}

Assert-Step 'evidence chain runs on real fixtures' {
    # The chain's whole value is that it reproduces the live dry-run on recorded
    # 82JQ data. A missing fixture or a renamed loader would leave the tests
    # green for the wrong reason, so require the chain and its inputs to exist.
    $chainFile = Join-Path $repoRoot 'internal\app\evidence_chain_test.go'
    if (-not (Test-Path -LiteralPath $chainFile)) {
        throw 'internal/app/evidence_chain_test.go is gone; the end-to-end evidence chain no longer exists'
    }
    $chain = Read-GateText $chainFile
    foreach ($step in @('ParseQuickFix', 'filterDriverRows', 'SelectLatestDrivers',
            'TestDriverApplicable', 'ResolveLocalDriverVersion', 'CompareDriverStatus',
            'partitionViewDrivers', 'WriteHistoryRecord', 'diffDeviceSnapshots')) {
        if ($chain -notmatch [regex]::Escape($step)) {
            throw "evidence chain no longer exercises $step; a link was dropped from the chain"
        }
    }
    # The required fixtures are read off the test sources rather than listed
    # here. A hardcoded list stops covering the next fixture someone adds -- the
    # OS-248 cross-edition fixture already sat outside the old three names --
    # while a path in a test is exactly the dependency worth checking. Both
    # spellings a test can use are matched: 'testdata/x.json' and
    # filepath.Join("testdata", "x.json"); covering one and not the other would
    # reintroduce the same blind spot in a new shape.
    $fixturePattern = 'testdata[/\\]([A-Za-z0-9_.\-]+)|"testdata"\s*,\s*"([A-Za-z0-9_.\-]+)"'
    $fixtures = @()
    foreach ($src in Get-ChildItem -Path (Join-Path $repoRoot 'internal') -Recurse -File -Filter '*_test.go') {
        foreach ($m in [regex]::Matches((Read-GateText $src.FullName), $fixturePattern)) {
            $name = if ($m.Groups[1].Success) { $m.Groups[1].Value } else { $m.Groups[2].Value }
            if ($name -and ($fixtures -notcontains $name)) { $fixtures += $name }
        }
    }
    if ($fixtures.Count -lt 3) {
        throw "the tests reference only $($fixtures.Count) testdata fixtures; the evidence chain was dismantled or this scan lost its targets"
    }
    if (-not ($fixtures | Where-Object { $_ -like '*_82jq*' })) {
        throw 'no 82JQ fixture is referenced by any test; the recorded-data size floor below would guard nothing'
    }
    foreach ($fixture in $fixtures) {
        $p = Get-ChildItem -Path (Join-Path $repoRoot 'internal') -Recurse -File -Filter $fixture | Select-Object -First 1
        if (-not $p) {
            throw "a test reads testdata/$fixture but no such fixture exists"
        }
        # A recorded machine fixture is bulk data, so an emptied or truncated one
        # would keep the tests green while proving nothing. Small hand-written
        # goldens are told apart by the same marker that names the machine the
        # evidence came off.
        if ($fixture -like '*_82jq*' -and $p.Length -lt 2048) {
            throw "recorded fixture $fixture is too small to be real recorded data"
        }
    }
}

Assert-Step 'recorded fixtures carry no live credentials or device serials' {
    # These fixtures are committed, so a download token or a machine serial in
    # one is a leak that no test would catch. The token is refreshed per fetch
    # anyway, and the instance tail is boot-specific, so redacting costs the
    # chain nothing.
    #
    # The set is every file under a testdata directory, not a list of names: a
    # named list only guards the fixtures that existed when it was written, and
    # the OS-248 fixture proved that by sitting outside it.
    $fixtures = Get-ChildItem -Path (Join-Path $repoRoot 'internal') -Recurse -File |
        Where-Object { $_.Directory.Name -eq 'testdata' }
    if ($fixtures.Count -lt 3) {
        throw "found only $($fixtures.Count) testdata fixtures; this scan lost its targets and would pass everything"
    }
    foreach ($fixture in $fixtures) {
        $text = Read-GateText $fixture.FullName
        if ($text -match '\?token=[A-Za-z0-9_\-]{20,}') {
            throw "$($fixture.Name) contains an unredacted download token; refresh via the redaction step before committing"
        }
        if ($text -match 'PF2SBWJA') {
            throw "$($fixture.Name) contains the machine serial; fixtures must carry hardware ids only"
        }
        # PnpDeviceID is the ledger join key, so the instance tail has to stay
        # recognisable, but it must be the placeholder rather than a real one.
        # Checked in every fixture, not just the device one: a real instance id
        # is a leak wherever it lands, including in a fixture added tomorrow.
        if ($text -match '\\\d+&\w+&\d+&\d+\\?"') {
            throw "$($fixture.Name) carries a real device instance id"
        }
    }
    Write-Host ("     {0} fixtures scanned for tokens, serials and instance ids" -f $fixtures.Count)
}

Assert-Step 'every text type the repo carries has an explicit line ending' {
    # An unpinned type gets whatever ending the editing tool wrote, and
    # core.autocrlf decides whether that reaches the index. A Windows GUI editor
    # saving CRLF into a file the gate parses is a whole-file diff and a parse
    # hazard that no other step would notice, so each type is pinned here rather
    # than left to the machine's git config.
    $attrs = Read-GateText (Join-Path $repoRoot '.gitattributes')
    foreach ($rule in @('*.md text eol=lf', '*.yml text eol=lf', '*.yaml text eol=lf',
            '*.json text eol=lf', '*.bat text eol=crlf', '*.ps1 text eol=lf',
            '*.go text eol=lf', '*.xaml text eol=lf')) {
        if ($attrs -notmatch ('(?m)^' + [regex]::Escape($rule) + '$')) {
            throw ".gitattributes no longer pins '$rule'"
        }
    }
}

Assert-Step 'git diff check' {
    $diff = git diff --check
    if ($LASTEXITCODE -ne 0) { throw "git diff --check failed`n$diff" }
}

Assert-Step 'untracked artifact hygiene' {
    $untracked = git status --porcelain | Where-Object { $_ -match '^\?\?' }
    foreach ($line in @($untracked)) {
        $path = ($line -replace '^\?\?\s+', '').Trim('"')
        if ($path -match '^(\.github/|scripts/)') {
            continue
        }
        if ($path -match '\.(go|md|ps1|bat|json)$' -or $path -eq 'go.mod' -or $path -eq '.gitignore' -or $path -eq '.gitattributes') {
            continue
        }
        if ($path -match '^(bin/|sessionlogs/|nul$)') {
            throw "untracked generated artifact remains: $path"
        }
    }
}

Write-Host "Verify summary: $total steps, $($failed.Count) failed"
if ($failed.Count -gt 0) {
    Write-Host ("Failed: {0}" -f ($failed -join ', '))
    exit 1
}
Write-Host 'VERIFY_OK'
exit 0
