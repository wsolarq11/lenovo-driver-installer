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
$GoExe = Resolve-GoExe -GoExe $GoExe

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
    if ($LASTEXITCODE -ne 0) { throw "go test -cover exited $LASTEXITCODE" }
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
                $count = @(Get-Content -LiteralPath $_.FullName).Count
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
    $cases = @((Get-Content -LiteralPath $casesPath -Raw | ConvertFrom-Json).cases)
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
    # Single source of truth: internal/app/help.go HelpText. docs/spec/contracts.md
    # must carry the same numeric codes; drift fails the gate.
    $help = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\app\help.go') -Raw
    if ($help -notmatch '(?ms)Exit codes:\r?\n(.*?)(\r?\n\r?\n)') {
        throw "help.go exit-code block not found"
    }
    $helpCodes = @([regex]::Matches($Matches[1], '(?m)^\s*(\d)\s+') | ForEach-Object { $_.Groups[1].Value })

    $contract = Get-Content -LiteralPath (Join-Path $repoRoot 'docs\spec\contracts.md') -Raw
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
    $help = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\app\help.go') -Raw
    $helpPaths = @([regex]::Matches($help, '(?m)^\s+(%LOCALAPPDATA%\\Lenovo\\DriverInstaller\\lenovo_driver_\S+\.(?:log|csv|txt))') | ForEach-Object { $_.Groups[1].Value })

    $contract = Get-Content -LiteralPath (Join-Path $repoRoot 'docs\spec\contracts.md') -Raw
    $contractPaths = @([regex]::Matches($contract, '(?m)^(%LOCALAPPDATA%\\Lenovo\\DriverInstaller\\lenovo_driver_\S+\.(?:log|csv|txt))') | ForEach-Object { $_.Groups[1].Value })

    if ($helpPaths.Count -lt 3) { throw "help.go parsed fewer than 3 audit artifact paths" }
    if ($contractPaths.Count -lt 3) { throw "contracts.md parsed fewer than 3 audit artifact paths" }
    $diff = Compare-Object -ReferenceObject ($helpPaths | Sort-Object) -DifferenceObject ($contractPaths | Sort-Object)
    if ($diff) {
        $detail = ($diff | ForEach-Object { "$($_.SideIndicator)$($_.InputObject)" }) -join ','
        throw "artifact-path drift (help vs contract): $detail"
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
        $stdout = Get-Content -LiteralPath $stdoutPath -Raw -ErrorAction SilentlyContinue
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
    $src = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\app\history.go') -Raw
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
    $helper = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\app\testapp_test.go') -Raw
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
    $model = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\model\model.go') -Raw
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
    $view = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\app\view.go') -Raw
    if ($view -match 'CompareStatus\s*!=\s*model\.StatusNotApplicable\s*\{\s*\n\s*applicable\s*=') {
        throw 'view.go partitions the automatic install set with a loose comparison; use model.InAutomaticInstallSet'
    }
    # The interactive prompt must describe the set it actually installs, and the
    # manual selection path must stay reachable for excluded statuses.
    $interactive = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\app\interactive.go') -Raw
    if ($interactive -match 'all-applicable|all applicable') {
        throw 'interactive prompt still advertises the old "all applicable" set whose membership changed'
    }
    # The GUI export is a second exit onto the same set: the WPF "install all"
    # button filters on IsApplicable and passes the codes via -GuiInstallCodes,
    # which bypasses partitionViewDrivers entirely. It must project the same
    # predicate or the GUI keeps downgrading what the CLI now refuses.
    $export = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\app\export.go') -Raw
    if ($export -match 'IsApplicable:\s*ad\.CompareStatus\s*!=\s*model\.StatusNotApplicable') {
        throw 'export.go redefines IsApplicable loosely; it must project model.InAutomaticInstallSet'
    }
    if ($export -notmatch 'IsApplicable:\s*model\.InAutomaticInstallSet') {
        throw 'export.go must project model.InAutomaticInstallSet onto IsApplicable'
    }
    $xaml = Get-Content -LiteralPath (Join-Path $repoRoot 'wpf\window.xaml') -Raw
    if ($xaml -match '安装全部可安装') {
        throw 'WPF install-all button still says "安装全部可安装"; the set no longer contains every applicable driver'
    }

Assert-Step 'a run reports only what the recheck confirmed' {
    # An installer can exit 0 without changing the machine. On 82JQ a drill for
    # DRV202102040007 logged "Install success" plus "Recheck: unchanged" while the
    # machine gained nothing, because the verdict came from the exit code alone.
    # The recheck's binding must gate the summary, and the labels that carry no
    # evidence must not be counted as successes.
    $install = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\app\install.go') -Raw
    # The pure binding verdicts live beside install.go, not inside it, so the
    # gate follows them rather than pinning a file that a later split would move.
    $bindingPath = Join-Path $repoRoot 'internal\app\install_binding.go'
    if (-not (Test-Path -LiteralPath $bindingPath)) {
        throw 'install_binding.go is gone; the binding verdicts lost their single owner'
    }
    $binding = Get-Content -LiteralPath $bindingPath -Raw
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
    # Re-keying it on TestSoftwareVersionedDriver silently reports every correctly
    # installed software-versioned driver as unverified.
    if ($binding -notmatch 'model\.IsMeasuredLocalVersion\(after\)') {
        throw 'installBindingLabel no longer decides comparability from the value it read'
    }
    if ($install -match 'installBindingLabel\([^)]*TestSoftwareVersionedDriver') {
        throw 'installBindingLabel is keyed on the driver family again; a real version read back from the ' +
              'installed-app list (Energy Management 15.11.29.65) is measurable and must carry a verdict'
    }
    $cm2 = [regex]::Match($binding, '(?s)func\s+installBindingLabel\([^)]*\)')
    if ($cm2.Success -and $cm2.Value -match 'bool\s*[,)]') {
        throw 'installBindingLabel takes a comparability flag again; the call site must not decide the verdict'
    }
    $model = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\model\model.go') -Raw
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
    $text = Get-Content -LiteralPath $t -Raw
    foreach ($fn in @('TestInstallBindingLabelHasNoVerdictWithoutComparableVersions',
                      'TestOnlyConfirmedBindingsCountAsSuccess')) {
        if ($text -notmatch ('func\s+' + [regex]::Escape($fn))) {
            throw "$fn is gone; the recheck-to-summary gate is unguarded"
        }
    }
}

Assert-Step 'one comparison primitive decides every version question' {
    # ord(a,b) has four values, not three: Undecided is not Less. Folding them
    # together is how an unmeasurable value becomes a fact — nil reads as older
    # than everything, so an unparseable version would be reported as behind the
    # list and handed to the automatic set.
    $version = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\compare\version.go') -Raw
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
    $binding = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\app\install_binding.go') -Raw
    if ($binding -match '==\s*packageVersion|==\s*after\b|==\s*before\b') {
        throw 'the post-install recheck is comparing raw version strings again; the WLAN rows carry ' +
              '"Intel_22.10.0.7/Realtek8852AE_6001.0.10.336/Mediatek_3.0.1.1314" in Version, which no ' +
              'local version can equal, so those drivers could never be confirmed bound'
    }
    if ($binding -notmatch 'compare\.Order\(') {
        throw 'the post-install recheck no longer uses the one comparator'
    }
    $matching = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\compare\matching.go') -Raw
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
    # "-QuietInstall" and its binary contains no such literal while carrying
    # "/VERYSILENT", the Inno Setup switch. Trusting the column launched a window
    # that the operator dismissed, and the machine gained nothing. Lenovo's own
    # tool dispatches on installer family, so the formula must too.
    $family = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\install\installer_family.go') -Raw
    if ($family -notmatch 'func\s+planSilentInstall') {
        throw 'planSilentInstall is gone; the silent dispatch has no single owner'
    }
    # The column may still say what the payload is. It may never say which flag
    # silences it.
    if ($family -notmatch 'vendorParameter') {
        throw 'vendorParameter is gone; the remaining use of the vendor column is no longer visible in one place'
    }
    if ($family -match 'strings\.Fields\(') {
        throw 'the vendor column is being split into command-line arguments again; it is a hint about the ' +
              'payload, never a silent switch'
    }
    # Every family the table names must carry that family''s own documented flags,
    # or an unproven package will be launched with something invented.
    # Scope to the table function. Several methods in this file name the same
    # families, so matching case arms in the whole file would read String()'s
    # "return Inno Setup" instead of the switch that sends the flags.
    $table = [regex]::Match($family, '(?s)func\s+\(f\s+InstallerFamily\)\s+silentArgs\(\).*?\{(.*?)\n\}')
    if (-not $table.Success) {
        throw 'could not locate the silent-args table'
    }
    $body = $table.Groups[1].Value
    $cases = @{
        'FamilyInnoPayload'   = '/VERYSILENT'
        'FamilyNSIS'          = '/S'
        'FamilySevenZipSFX'   = '-s'
        'FamilyInstallShield' = '/s'
        'FamilyWiXBurn'       = '/quiet'
    }
    foreach ($key in $cases.Keys) {
        $cm = [regex]::Match($body, "(?s)case\s+$key\s*:(.*?)(?=case\s+Family|default\s*:)")
        if (-not $cm.Success) {
            throw "$key has no case in the silent-args table"
        }
        if ($cm.Groups[1].Value -notmatch [regex]::Escape($cases[$key])) {
            throw "$key no longer sends its own documented switch ($($cases[$key]))"
        }
    }
    if ($body -notmatch '(?s)default\s*:\s*\n\s*return\s+nil') {
        throw 'an unproven family must produce no flags at all'
    }
    $t = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\install\install_test.go') -Raw
    foreach ($fn in @('TestSilentInstallerArgsUsesProvenFamilyNotVendorColumn',
                      'TestSilentInstallerArgsCoversEveryProvenFamily',
                      'TestSilentInstallerArgsSkipsFlagForINFWrapper',
                      'TestInstallEXEUnprovenFamilyIsTerminal',
                      'TestInstallEXERunsProvenPackageWithoutVendorColumn')) {
        if ($t -notmatch ('func\s+' + [regex]::Escape($fn))) {
            throw "$fn is gone; the formula is unguarded"
        }
    }
}

Assert-Step 'the provisioning placeholder never reaches a decision' {
    # "Provisioned" is a fact about HKLM\...\Provisioning\Results, not a version.
    # It reached a decision twice: as a local version in the recheck, and as
    # StatusUpToDate in CompareDriverStatus, which is fact-grade and therefore
    # treated as measured. A placeholder must be undetermined everywhere.
    $matching = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\compare\matching.go') -Raw
    $cm = [regex]::Match($matching, '(?s)if\s+local\s*==\s*model\.LocalVersionProvisioned\s*\{(.*?)\n\t\}')
    if (-not $cm.Success) {
        throw 'CompareDriverStatus no longer handles the provisioning placeholder'
    }
    if ($cm.Groups[1].Value -notmatch 'return\s+model\.StatusUnknown') {
        throw 'the provisioning placeholder is judged again; it proves neither presence nor absence, ' +
              'so only an undetermined status is honest'
    }
    $t = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\app\evidence_chain_test.go') -Raw
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
    $sel = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\compare\selection.go') -Raw
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
    $text = Get-Content -LiteralPath $t -Raw
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
    $model = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\model\model.go') -Raw
    $inv = Join-Path $repoRoot 'internal\model\evidence_basis_test.go'
    if (-not (Test-Path -LiteralPath $inv)) {
        throw 'evidence_basis_test.go is gone; the fact/inference pairing is unguarded'
    }
    $text = Get-Content -LiteralPath $inv -Raw
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

Assert-Step 'software-versioned compare falls back to the measured device version' {
    # Not installed is the only status the automatic set acts on, so what
    # produces it has to rest on measurement. A software-versioned driver whose
    # name is missing from InstalledApps used to return an empty local version,
    # which CompareDriverStatus turned into "not installed" while the device was
    # in fact running a vendor package. On 82JQ that put a redundant reinstall
    # of ACPI\VPC2004 in front of the user. The measured device version must win
    # whenever the software lookup finds nothing.
    $matching = Get-Content -LiteralPath (Join-Path $repoRoot 'internal\compare\matching.go') -Raw
    if ($matching -notmatch 'func\s+deviceVersionFrom') {
        throw 'deviceVersionFrom is gone; the device-measured version has no single extraction point'
    }
    $m = [regex]::Match($matching, '(?s)if\s+TestSoftwareVersionedDriver\(driver\.DriverName\)\s*\{(.*?)\n\t\}')
    if (-not $m.Success) {
        throw 'could not locate the TestSoftwareVersionedDriver branch in matching.go'
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
    $chain = Get-Content -LiteralPath $chainFile -Raw
    foreach ($step in @('ParseQuickFix', 'filterDriverRows', 'SelectLatestDrivers',
            'TestDriverApplicable', 'ResolveLocalDriverVersion', 'CompareDriverStatus',
            'partitionViewDrivers', 'WriteHistoryRecord', 'diffDeviceSnapshots')) {
        if ($chain -notmatch [regex]::Escape($step)) {
            throw "evidence chain no longer exercises $step; a link was dropped from the chain"
        }
    }
    foreach ($fixture in @('quickfix_real_82jq.json', 'local_devices_82jq.json', 'local_software_82jq.json')) {
        $p = Join-Path $repoRoot "internal\app\testdata\$fixture"
        if (-not (Test-Path -LiteralPath $p)) {
            throw "evidence fixture missing: $fixture"
        }
        if ((Get-Item -LiteralPath $p).Length -lt 2048) {
            throw "evidence fixture $fixture is too small to be real recorded data"
        }
    }
}

Assert-Step 'recorded fixtures carry no live credentials or device serials' {
    # These fixtures are committed, so a download token or a machine serial in
    # one is a leak that no test would catch. The token is refreshed per fetch
    # anyway, and the instance tail is boot-specific, so redacting costs the
    # chain nothing.
    foreach ($fixture in @('quickfix_real_82jq.json', 'local_devices_82jq.json', 'local_software_82jq.json')) {
        $p = Join-Path $repoRoot "internal\app\testdata\$fixture"
        $text = Get-Content -LiteralPath $p -Raw
        if ($text -match '\?token=[A-Za-z0-9_\-]{20,}') {
            throw "$fixture contains an unredacted download token; refresh via the redaction step before committing"
        }
        if ($text -match 'PF2SBWJA') {
            throw "$fixture contains the machine serial; fixtures must carry hardware ids only"
        }
        # PnpDeviceID is the ledger join key, so the instance tail has to stay
        # recognisable, but it must be the placeholder rather than a real one.
        if ($fixture -eq 'local_devices_82jq.json' -and $text -match '\\\d+&\w+&\d+&\d+\\?"') {
            throw 'local_devices_82jq.json carries a real device instance id'
        }
    }
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
        if ($path -match '^(\.trellis/tasks/08-26-go-migration/|\.github/|scripts/)') {
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
