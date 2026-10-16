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

Assert-Step 'ledger device identity stays a column (not free text)' {
    # The ledger's Devices column is the single machine-readable source for
    # "which device this row acted on". Re-embedding a device id into the
    # free-text Message would let two sources of device identity drift apart and
    # force downstream parsing, so the pattern is gated in production code.
    $hits = Get-ChildItem -Path 'internal' -Recurse -Filter *.go |
        Where-Object { $_.FullName -notmatch '_test\.go$' } |
        Select-String -Pattern '"device "\s*\+|"devices="|"device="'
    if ($hits) {
        $files = ($hits | ForEach-Object { "$($_.Path):$($_.LineNumber)" }) -join ', '
        throw "device identity must ride in the ledger Devices column, not in free-text messages: $files"
    }
}

Assert-Step 'ledger hash column located by header name' {
    # Chain verification must find the hash column by name in the on-disk
    # header. A positional lookup (len(historyColumns)) silently verifies the
    # wrong cells once a column is added, so tampering stops being detectable
    # without any error surfacing.
    $src = Read-GateText (Join-Path $repoRoot 'internal\app\history.go')
    if ($src -notmatch 'func\s+resolveHashIndex') {
        throw 'history.go must expose resolveHashIndex (header-name lookup)'
    }
    if ($src -match 'row\[len\(historyColumns\)\]') {
        throw 'history.go indexes the ledger positionally by column count; the hash column must be located by header name'
    }
}

Assert-Step 'tests never write to the real audit directory' {
    # New() points LogPath/PlanPath/HistoryPath at the real per-user audit
    # directory, which is the authoritative evidence store on a live machine.
    # A test calling New() directly appends fixture lines there, where they
    # become indistinguishable from a real operation. newTestApp redirects every
    # artifact into t.TempDir(); it is the only sanctioned test constructor.
    $hits = Get-ChildItem -Path 'internal' -Recurse -Filter *_test.go |
        Where-Object { $_.Name -ne 'testapp_test.go' } |
        Select-String -Pattern '=\s*New\(' -CaseSensitive
    if ($hits) {
        $files = ($hits | ForEach-Object { "$($_.Path):$($_.LineNumber)" }) -join ', '
        throw "tests must construct via newTestApp(t) so audit artifacts stay in t.TempDir(): $files"
    }
    $helper = Read-GateText (Join-Path $repoRoot 'internal\app\testapp_test.go')
    foreach ($artifact in @('LogPath', 'PlanPath', 'HistoryPath', 'driverListCacheDir')) {
        if ($helper -notmatch "\.$artifact\s*=") {
            throw "newTestApp must redirect $artifact into t.TempDir()"
        }
    }
}

Assert-Step 'automatic install set excludes downgrade and no-op statuses' {
    # Invariant 3 admits only Update and Not installed into the unattended set.
    # A loose "!= Not applicable" admits Local newer, which downgrades the
    # device (82JQ: 9 of 11 candidates, AMD VGA 30.0.14052.9003 -> 27.20.15026.8004)
    # and Up to date, which is a no-op. Membership is owned by one model
    # predicate so the rule cannot drift back into the view layer.
    $model = Read-GateText (Join-Path $repoRoot 'internal\model\model.go')
    if ($model -notmatch '(?s)func\s+InAutomaticInstallSet\(status\s+CompareStatus\)\s*bool\s*\{(.*?)\n\}') {
        throw 'model.go must own InAutomaticInstallSet as the single membership authority'
    }
    $body = $Matches[1]
    if ($body -match '!=') {
        throw 'InAutomaticInstallSet must be an explicit allow-list (==); a "!=" comparison silently re-admits downgrade/no-op statuses'
    }
    foreach ($required in @('StatusUpdate', 'StatusNotInstalled')) {
        if ($body -notmatch $required) {
            throw "InAutomaticInstallSet must admit $required explicitly"
        }
    }
    $view = Read-GateText (Join-Path $repoRoot 'internal\app\view.go')
    if ($view -match 'CompareStatus\s*!=\s*model\.StatusNotApplicable\s*\{\s*\n\s*applicable\s*=') {
        throw 'view.go partitions the automatic install set with a loose comparison; use model.InAutomaticInstallSet'
    }
    # The interactive prompt must describe the set it actually installs, and the
    # manual selection path must stay reachable for excluded statuses.
    $interactive = Read-GateText (Join-Path $repoRoot 'internal\app\interactive.go')
    if ($interactive -match 'all-applicable|all applicable') {
        throw 'interactive prompt still advertises the old "all applicable" set whose membership changed'
    }
    # The GUI export is a second exit onto the same set: the WPF "install all"
    # button filters on IsApplicable and passes the codes via -GuiInstallCodes,
    # which bypasses partitionViewDrivers entirely. It must project the same
    # predicate or the GUI keeps downgrading what the CLI now refuses.
    $export = Read-GateText (Join-Path $repoRoot 'internal\app\export.go')
    if ($export -match 'IsApplicable:\s*ad\.CompareStatus\s*!=\s*model\.StatusNotApplicable') {
        throw 'export.go redefines IsApplicable loosely; it must project model.InAutomaticInstallSet'
    }
    if ($export -notmatch 'IsApplicable:\s*model\.InAutomaticInstallSet') {
        throw 'export.go must project model.InAutomaticInstallSet onto IsApplicable'
    }
    $xaml = Read-GateText (Join-Path $repoRoot 'wpf\window.xaml')
    if ($xaml -match '安装全部可安装') {
        throw 'WPF install-all button still says "安装全部可安装"; the set no longer contains every applicable driver'
    }
}

Assert-Step 'a run reports only what the recheck confirmed' {
    # An installer can exit 0 without changing the machine. On 82JQ a drill for
    # DRV202102040007 logged "Install success" plus "Recheck: unchanged" while the
    # machine gained nothing, because the verdict came from the exit code alone.
    # The recheck's binding must gate the summary, and the labels that carry no
    # evidence must not be counted as successes.
    $install = Read-GateText (Join-Path $repoRoot 'internal\app\install.go')
    # The pure binding verdicts live beside install.go, not inside it, so the
    # gate follows them rather than pinning a file that a later split would move.
    $bindingPath = Join-Path $repoRoot 'internal\app\install_binding.go'
    if (-not (Test-Path -LiteralPath $bindingPath)) {
        throw 'install_binding.go is gone; the binding verdicts lost their single owner'
    }
    $binding = Read-GateText $bindingPath
    if ($binding -notmatch 'func\s+installBindingConfirmsEffect') {
        throw 'installBindingConfirmsEffect is gone; the run summary is no longer gated on the recheck'
    }
    # Read the confirmation function's own body: the labels share one case arm,
    # so matching case arms line by line would miss all three.
    $cm = [regex]::Match($binding, '(?s)func\s+installBindingConfirmsEffect\(binding\s+string\)\s*bool\s*\{(.*?)\n\}')
    if (-not $cm.Success) {
        throw 'could not locate installBindingConfirmsEffect'
    }
    $confirms = $cm.Groups[1].Value
    if ($confirms -notmatch '(?m)^\s*case\s') {
        throw 'installBindingConfirmsEffect no longer enumerates confirmed bindings by name'
    }
    foreach ($label in @('bound', 'staged', 'unchanged-same')) {
        if ($confirms -notmatch [regex]::Escape($label)) {
            throw "binding $label no longer counts as a confirmed install"
        }
    }
    foreach ($label in @('unchanged', 'undetected')) {
        if ($confirms -match ('case[^\r\n]*"' + [regex]::Escape($label) + '"[^\r\n]*:\s*\r?\n\s*return\s+true')) {
            throw "binding $label carries no evidence of effect but is counted as a confirmed install"
        }
    }
    # Comparability must come from the value, not the driver family, and the
    # decision must live in the binding owner rather than at the call site.
    # Re-keying it on the driver-family predicate silently reports every
    # correctly installed software-versioned driver as unverified - which is why
    # the next assertion names that predicate and is re-checked when it moves.
    if ($binding -notmatch 'model\.IsMeasuredLocalVersion\(after\)') {
        throw 'installBindingLabel no longer decides comparability from the value it read'
    }
    if ($install -match 'installBindingLabel\([^)]*isSoftwareVersionedDriver') {
        throw 'installBindingLabel is keyed on the driver family again; a real version read back from the ' +
              'installed-app list (Energy Management 15.11.29.65) is measurable and must carry a verdict'
    }
    $cm2 = [regex]::Match($binding, '(?s)func\s+installBindingLabel\([^)]*\)')
    if ($cm2.Success -and $cm2.Value -match 'bool\s*[,)]') {
        throw 'installBindingLabel takes a comparability flag again; the call site must not decide the verdict'
    }
    $model = Read-GateText (Join-Path $repoRoot 'internal\model\model.go')
    if ($model -notmatch 'const\s+LocalVersionProvisioned\s+=') {
        throw 'model.LocalVersionProvisioned is gone; the provisioning placeholder lost its single owner'
    }
    if ($model -notmatch 'func\s+IsMeasuredLocalVersion') {
        throw 'model.IsMeasuredLocalVersion is gone; callers must not each decide what counts as a version'
    }
    $stale = Select-String -LiteralPath (Join-Path $repoRoot 'internal\compare\matching.go') -Pattern '"Provisioned"' -SimpleMatch
    if ($stale) {
        throw "matching.go still spells the provisioning placeholder itself at line(s): $($stale.LineNumber -join ',')"
    }
    if ($install -notmatch 'unverified=\%d') {
        throw 'the Finished summary no longer reports an unverified count'
    }
    if ($install -notmatch 'installBindingConfirmsEffect\(binding\)') {
        throw 'verifyInstalled no longer filters drivers through the confirmation gate'
    }
    $t = Join-Path $repoRoot 'internal\app\app_test.go'
    $text = Read-GateText $t
    foreach ($fn in @('TestInstallBindingLabelHasNoVerdictWithoutComparableVersions',
                      'TestOnlyConfirmedBindingsCountAsSuccess')) {
        if ($text -notmatch ('func\s+' + [regex]::Escape($fn))) {
            throw "$fn is gone; the recheck-to-summary gate is unguarded"
        }
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
    foreach ($gateName in @('silent install dispatches on the package, not on the vendor column',
                            'one comparison primitive decides every version question')) {
        if ($selfText -notmatch [regex]::Escape("Assert-Step '$gateName'")) {
            throw "invariants.md cites the gate step '$gateName' but this file no longer declares it"
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

Assert-Step 'one comparison primitive decides every version question' {
    # ord(a,b) has four values, not three: Undecided is not Less. Folding them
    # together is how an unmeasurable value becomes a fact — nil reads as older
    # than everything, so an unparseable version would be reported as behind the
    # list and handed to the automatic set.
    $version = Read-GateText (Join-Path $repoRoot 'internal\compare\version.go')
    if ($version -notmatch 'type\s+VersionOrder\s+int') {
        throw 'VersionOrder is gone; the undecided outcome is lost again'
    }
    foreach ($v in @('OrderUndecided', 'OrderLess', 'OrderEqual', 'OrderGreater')) {
        if ($version -notmatch ("\b$v\b")) {
            throw "VersionOrder lost the $v outcome"
        }
    }
    $ord = [regex]::Match($version, '(?s)func\s+Order\(a, b \*Version\)\s*VersionOrder\s*\{(.*?)\n\}')
    if (-not $ord.Success) {
        throw 'the single Order comparator is gone'
    }
    if ($ord.Groups[1].Value -notmatch '(?s)a\s*==\s*nil\s*\|\|\s*b\s*==\s*nil\s*\{\s*return\s+OrderUndecided') {
        throw 'Order must return OrderUndecided for a missing version before it considers direction'
    }
    if ($version -notmatch 'func\s+ResolveComparableVersion\(raw, vendor string\)') {
        throw 'ResolveComparableVersion is gone; callers will compare raw strings again'
    }
    # The recheck and the compare pass must speak through that one primitive.
    $binding = Read-GateText (Join-Path $repoRoot 'internal\app\install_binding.go')
    if ($binding -match '==\s*packageVersion|==\s*after\b|==\s*before\b') {
        throw 'the post-install recheck is comparing raw version strings again; the WLAN rows carry ' +
              '"Intel_22.10.0.7/Realtek8852AE_6001.0.10.336/Mediatek_3.0.1.1314" in Version, which no ' +
              'local version can equal, so those drivers could never be confirmed bound'
    }
    if ($binding -notmatch 'compare\.Order\(') {
        throw 'the post-install recheck no longer uses the one comparator'
    }
    $matching = Read-GateText (Join-Path $repoRoot 'internal\compare\matching.go')
    if ($matching -match 'localVersion\.Compare\(') {
        throw 'CompareDriverStatus calls Version.Compare directly again; the undecided outcome would be ' +
              'folded into a direction'
    }
    if ($matching -notmatch 'Order\(localVersion, remoteVersion\)') {
        throw 'CompareDriverStatus no longer routes through Order'
    }
}

Assert-Step 'silent install dispatches on the package, not on the vendor column' {
    # The vendor Parameter column is falsified: DRV202102040007 declares
    # "-QuietInstall" and its binary contains no such literal but carries the
    # Inno header "Inno Setup Setup Data". Trusting the column launched a window
    # that the operator dismissed, and the machine gained nothing. The formula
    # therefore recognises the family from the package's own bytes, and drives
    # every .exe to interactive because no .exe silent switch has a verified
    # end-to-end run on 82JQ. Recognising a family is not verifying its switch.
    $family = Read-GateText (Join-Path $repoRoot 'internal\install\installer_family.go')
    if ($family -notmatch 'func\s+planSilentInstall') {
        throw 'planSilentInstall is gone; the silent dispatch has no single owner'
    }
    # No family may send flags: any switch array here is a guess being launched,
    # and a wrong switch is a silent no-op that reports success while changing
    # nothing. The moment one switch is verified end-to-end, this assert and the
    # refusing tests change together.
    if ($family -match 'return\s+\[\]string\{') {
        throw 'a switch array reappeared; no .exe silent switch is verified, so no family may send flags'
    }
    # Family recognition survives for the audit trail even though no family runs
    # unattended: the refusal evidence must name what was recognised.
    if ($family -notmatch 'familyMarkers') {
        throw 'familyMarkers is gone; the refusal evidence can no longer name the recognised family'
    }
    $t = Read-GateText (Join-Path $repoRoot 'internal\install\install_test.go')
    foreach ($fn in @('TestSilentPlanRefusesUnverifiedInno',
                      'TestSilentPlanRefusesEveryUnverifiedFamily',
                      'TestSilentPlanEvidenceNamesTheRefusedFamily',
                      'TestSilentPlanEvidenceAgreesWithTheDecision',
                      'TestInstallDriverFileEXEIsTerminal')) {
        if ($t -notmatch ('func\s+' + [regex]::Escape($fn))) {
            throw "$fn is gone; the formula is unguarded"
        }
    }
}

Assert-Step 'the silent mechanism reaches the ledger as prose, never as a version' {
    # The evidence that authorised an unattended install has to survive into
    # the audit row. It is prose: it names the installer family. The ledger's
    # VerifiedVersion column is not a free slot, and writing a mechanism into
    # it would silently convert "Inno Setup marker" into a version claim. The
    # evidence therefore lives inside Message, which nothing parses
    # (TestAuditAttributionIgnoresMessageText).
    $app = Read-GateText (Join-Path $repoRoot 'internal\app\install.go')
    $silent = Read-GateText (Join-Path $repoRoot 'internal\app\install_silent.go')
    $fam = Read-GateText (Join-Path $repoRoot 'internal\install\installer_family.go')
    if ($fam -notmatch 'func\s+SilentPlanFor\(') {
        throw 'SilentPlanFor is gone; the evidence has no single source'
    }
    if ($silent -notmatch 'install\.SilentPlanFor\(') {
        throw 'the app no longer asks for the silent mechanism; the audit row is blind again'
    }
    if ($app -notmatch 'silent via') {
        throw 'the Installed row no longer records which mechanism authorised the run'
    }
    # The evidence variable must never appear as a bare positional argument:
    # that is how it leaks into VerifiedVersion/BeforeVersion and turns a
    # mechanism into a version claim.
    if ($app -match ', silence, ') {
        throw 'silence is a bare positional argument to a write; the evidence is leaking into a version column'
    }
}

Assert-Step 'the provisioning placeholder never reaches a decision' {
    # "Provisioned" is a fact about HKLM\...\Provisioning\Results, not a version.
    # It reached a decision twice: as a local version in the recheck, and as
    # StatusUpToDate in CompareDriverStatus, which is fact-grade and therefore
    # treated as measured. A placeholder must be undetermined everywhere.
    $matching = Read-GateText (Join-Path $repoRoot 'internal\compare\matching.go')
    $cm = [regex]::Match($matching, '(?s)if\s+local\s*==\s*model\.LocalVersionProvisioned\s*\{(.*?)\n\t\}')
    if (-not $cm.Success) {
        throw 'CompareDriverStatus no longer handles the provisioning placeholder'
    }
    if ($cm.Groups[1].Value -notmatch 'return\s+model\.StatusUnknown') {
        throw 'the provisioning placeholder is judged again; it proves neither presence nor absence, ' +
              'so only an undetermined status is honest'
    }
    $t = Read-GateText (Join-Path $repoRoot 'internal\app\evidence_chain_test.go')
    if ($t -notmatch 'func\s+TestProvisionedIsNotAVersion') {
        throw 'TestProvisionedIsNotAVersion is gone; the placeholder-to-status rule is unguarded'
    }
}

Assert-Step 'driver selection order is a total order' {
    # PartID is not a total sort key: on 82JQ one PartID covers several groups
    # (249 alone holds five different WLAN vendors). A PartID-only stable sort
    # therefore left the surviving rows in map-iteration order, and consecutive
    # -DryRun exports of identical input disagreed on row order. The tie-breaks
    # below must match the grouping key so no two rows can tie.
    $sel = Read-GateText (Join-Path $repoRoot 'internal\compare\selection.go')
    $m = [regex]::Match($sel, '(?s)sort\.Slice\(selected, func\(i, j int\) bool \{(.*?)\n\t\}\)')
    if (-not $m.Success) {
        throw 'could not locate the SelectLatestDrivers sort; ordering is unguarded'
    }
    $body = $m.Groups[1].Value
    foreach ($key in @('PartID', 'DriverName', 'DriverCode')) {
        if ($body -notmatch [regex]::Escape($key)) {
            throw "the selection sort does not tie-break on $key; equal prefixes would fall back to map order"
        }
    }
    if ($sel -match 'sort\.SliceStable\(selected') {
        throw 'selection still uses SliceStable on a non-total key'
    }
    $t = Join-Path $repoRoot 'internal\app\evidence_firmware_determinism_test.go'
    if (-not (Test-Path -LiteralPath $t)) {
        throw 'evidence_firmware_determinism_test.go is gone; determinism is unguarded'
    }
    $text = Read-GateText $t
    if ($text -notmatch 'func\s+TestSelectionOrderIsDeterministic') {
        throw 'TestSelectionOrderIsDeterministic is gone'
    }
    if ($text -notmatch 'func\s+TestFirmwareRowsAreDroppedUnlessRequested') {
        throw 'the firmware branch has no coverage and no recorded list can provide it'
    }
}

Assert-Step 'the automatic install set is backed by measurements only' {
    # Two decisions, two files: which statuses may be installed unattended, and
    # how strongly each status is evidenced. If they drift apart, the tool
    # changes machine state on something it did not measure. The pairing is
    # asserted in Go; this gate keeps the test from being deleted to unblock a
    # change, and keeps the doc comment claiming the invariant.
    $model = Read-GateText (Join-Path $repoRoot 'internal\model\model.go')
    $inv = Join-Path $repoRoot 'internal\model\evidence_basis_test.go'
    if (-not (Test-Path -LiteralPath $inv)) {
        throw 'evidence_basis_test.go is gone; the fact/inference pairing is unguarded'
    }
    $text = Read-GateText $inv
    if ($text -notmatch 'func\s+TestAutomaticSetStatusesAreMeasured') {
        throw 'TestAutomaticSetStatusesAreMeasured is gone; the automatic set can drift onto unmeasured statuses'
    }
    if ($text -notmatch 'func\s+TestNonFactStatusesStayOutOfTheAutomaticSet') {
        throw 'TestNonFactStatusesStayOutOfTheAutomaticSet is gone; widening the set no longer has to be argued'
    }
    # StatusEvidenceBasis must route every automatic-set member through fact.
    $m = [regex]::Match($model, '(?s)func\s+StatusEvidenceBasis\(status\s+CompareStatus\)\s*string\s*\{(.*?)\n\}')
    if (-not $m.Success) {
        throw 'could not locate StatusEvidenceBasis in model.go'
    }
    $body = $m.Groups[1].Value
    $factCase = [regex]::Match($body, 'case\s+([^\r\n:]+):\s*\r?\n\s*return\s+"fact"')
    if (-not $factCase.Success) {
        throw 'StatusEvidenceBasis no longer returns fact from an explicit case list'
    }
    foreach ($status in @('StatusUpdate', 'StatusUpToDate', 'StatusNotInstalled')) {
        if ($factCase.Groups[1].Value -notmatch [regex]::Escape($status)) {
            throw "automatic-set member $status is not routed to fact by StatusEvidenceBasis"
        }
    }
    if ($body -match '"inference"') {
        throw 'StatusEvidenceBasis still emits inference; every status it can name is either measured or undecided'
    }
}

Assert-Step 'package cleanup is never presented as rollback' {
    # Invariant 6: rollback is device-level — DiRollbackDriver, or reinstalling
    # the previous INF. Deleting the published package by INF does not put a
    # device back on its previous driver and can leave it with none until PnP
    # re-enumerates, so no code may carry that argument vector. The dead
    # primitive that held it was removed; this step keeps it gone rather than
    # trusting that nobody re-adds it. Tests are scanned too: a test that pins
    # the vector is exactly how the primitive comes back.
    $hits = Get-ChildItem -Path 'internal' -Recurse -Filter *.go |
        Select-String -Pattern '"/delete-driver"' -SimpleMatch
    if ($hits) {
        $files = ($hits | ForEach-Object { "$($_.Path):$($_.LineNumber)" }) -join ', '
        throw "engine code carries the driver-package delete vector ($files); package cleanup is not rollback and must not exist as an action"
    }
    # Refusing package cleanup is only meaningful while a real device-level
    # rollback path exists, so pin that side too.
    $native = Read-GateText (Join-Path $repoRoot 'internal\install\rollback_native_windows.go')
    if ($native -notmatch 'DiRollbackDriver') {
        throw 'the device-level rollback call is gone; refusing package cleanup guards nothing'
    }
}

Assert-Step 'software-versioned compare falls back to the measured device version' {
    # Not installed is the only status the automatic set acts on, so what
    # produces it has to rest on measurement. A software-versioned driver whose
    # name is missing from InstalledApps used to return an empty local version,
    # which CompareDriverStatus turned into "not installed" while the device was
    # in fact running a vendor package. On 82JQ that put a redundant reinstall
    # of ACPI\VPC2004 in front of the user. The measured device version must win
    # whenever the software lookup finds nothing.
    $matching = Read-GateText (Join-Path $repoRoot 'internal\compare\matching.go')
    if ($matching -notmatch 'func\s+deviceVersionFrom') {
        throw 'deviceVersionFrom is gone; the device-measured version has no single extraction point'
    }
    $m = [regex]::Match($matching, '(?s)if\s+isSoftwareVersionedDriver\(driver\.DriverName\)\s*\{(.*?)\n\t\}')
    if (-not $m.Success) {
        throw 'could not locate the software-versioned branch in matching.go'
    }
    $branch = $m.Groups[1].Value
    if ($branch -notmatch 'deviceVersionFrom') {
        throw 'the software-versioned branch returns the software version with no device fallback; ' +
              'a failed InstalledApps lookup would again be read as evidence of absence'
    }
    $t = Join-Path $repoRoot 'internal\compare\software_version_fallback_test.go'
    if (-not (Test-Path -LiteralPath $t)) {
        throw 'software_version_fallback_test.go is gone; the fallback rule is unguarded'
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

Assert-Step 'no package exports what nothing outside it names' {
    # An exported name nothing outside its package spells is a false API
    # promise: it invites a consumer the package never meant to serve, and it
    # rots into a second surface nobody maintains. Each package's export set
    # must equal what its consumers use, so this step fails on any name that
    # only its own package mentions and asks for it to be unexported.
    #
    # Deliberately out of scope: methods are never spelled pkg.Name, so this
    # scan cannot tell whether a *method* has a consumer - judging one here
    # would fail on every method of an exported type. But an exported method is
    # still a declaration callers use, so its signature does count when asking
    # whether some other declaration needs a type's name (see Get-ExportedDecls
    # below, which includes methods for exactly that reason).
    # Types another exported declaration mentions must stay exported too, or
    # that declaration returns something callers cannot name.
    # Consumers include tests: an in-package test uses bare names, so only a
    # test in another package counts, and that is a real consumer.
    $goFiles = Get-ChildItem -Path $repoRoot -Recurse -File -Filter '*.go' |
        Where-Object { $_.FullName -notmatch '\\\.' }
    $texts = @{}
    foreach ($f in $goFiles) { $texts[$f.FullName] = Read-GateText $f.FullName }

    # Exported declarations of one source file, with the struct body attached
    # when the declaration is a struct, because an exported struct's fields are
    # where a type gets named by another exported declaration.
    #
    # Returned as a list of name/body pairs rather than a map keyed by name: two
    # types in one package can each have an Error method, and keying by name
    # would keep only the last one, silently reporting the other type as unused.
    function Get-ExportedDecls {
        param([string]$Source)
        $decls = @()
        $lines = $Source -split "`n"
        for ($i = 0; $i -lt $lines.Count; $i++) {
            $name = $null
            if ($lines[$i] -cmatch '^func\s+([A-Z][A-Za-z0-9_]*)\s*[\(\[]') { $name = $Matches[1] }
            elseif ($lines[$i] -cmatch '^func\s+\([^)]*\)\s+([A-Z][A-Za-z0-9_]*)\s*[\(\[]') { $name = $Matches[1] }
            elseif ($lines[$i] -cmatch '^type\s+([A-Z][A-Za-z0-9_]*)\s') { $name = $Matches[1] }
            if (-not $name) { continue }
            $body = $lines[$i]
            if ($lines[$i] -cmatch '^type\s+[A-Z][A-Za-z0-9_]*\s+struct\s*\{') {
                for ($j = $i + 1; $j -lt $lines.Count -and $lines[$j] -notmatch '^\}'; $j++) { $body += "`n" + $lines[$j] }
            }
            $decls += , @($name, $body)
        }
        return $decls
    }

    $offenders = @()
    foreach ($dir in (Get-ChildItem -Path (Join-Path $repoRoot 'internal') -Recurse -Directory)) {
        $prod = @(Get-ChildItem -Path $dir.FullName -File -Filter '*.go' -ErrorAction SilentlyContinue |
            Where-Object { $_.Name -notlike '*_test.go' })
        if ($prod.Count -eq 0) { continue }
        $kind = @{}
        $decls = @()
        foreach ($f in $prod) {
            foreach ($line in ($texts[$f.FullName] -split "`n")) {
                if ($line -cmatch '^func\s+([A-Z][A-Za-z0-9_]*)\s*[\(\[]') { $kind[$Matches[1]] = 'func' }
                elseif ($line -cmatch '^type\s+([A-Z][A-Za-z0-9_]*)\s') { $kind[$Matches[1]] = 'type' }
                elseif ($line -cmatch '^(var|const)\s+([A-Z][A-Za-z0-9_]*)\s') { $kind[$Matches[2]] = 'var' }
            }
            $decls += Get-ExportedDecls -Source $texts[$f.FullName]
        }
        foreach ($name in ($kind.Keys | Sort-Object)) {
            $qualified = [regex]::Escape("$($dir.Name).$name")
            $consumed = $false
            foreach ($f in $goFiles) {
                if ($f.Directory.FullName -eq $dir.FullName) { continue }
                if ($texts[$f.FullName] -cmatch "(?m)(^|[^A-Za-z0-9_.])$qualified([^A-Za-z0-9_]|$)") { $consumed = $true; break }
            }
            if ($consumed) { continue }
            if ($kind[$name] -eq 'type') {
                $named = $false
                foreach ($pair in $decls) {
                    if ($pair[0] -eq $name) { continue }
                    if ($pair[1] -cmatch "(^|[^A-Za-z0-9_])$([regex]::Escape($name))([^A-Za-z0-9_]|$)") { $named = $true; break }
                }
                if ($named) { continue }
            }
            $offenders += "internal/$($dir.Name): $name ($($kind[$name]))"
        }
    }
    if ($offenders.Count -gt 0) {
        throw ("nothing outside its package names these exported identifiers, so they are an API that is not an API - unexport them:`n  " +
            ($offenders -join "`n  "))
    }
}

Assert-Step 'git diff check' {
    $diff = git diff --check
    if ($LASTEXITCODE -ne 0) { throw "git diff --check failed`n$diff" }
}

Assert-Step 'engine zero-network import (local side-effect packages)' {
    # Determinism invariant: only download/api may touch the network. Local
    # side-effect packages must not import net/http or net/url, otherwise a
    # hidden network call could slip in silently.
    $localPkgs = 'internal\install', 'internal\compare', 'internal\inventory', 'internal\plan', 'internal\audit'
    $hits = Get-ChildItem -Path $localPkgs -Recurse -Filter *.go |
        Where-Object { $_.FullName -notmatch '_test\.go$' } |
        Select-String -Pattern '"net/http"|"net/url"'
    if ($hits) {
        $files = ($hits | ForEach-Object { "$($_.Path):$($_.LineNumber)" }) -join ', '
        throw "local side-effect packages must not import net/http or net/url: $files"
    }
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
