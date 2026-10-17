@echo off
rem Keep bin\lenovo-driver.exe in step with the source tree.
rem
rem The launchers used to build only when the engine was missing, so a binary
rem left over from an older checkout was run silently forever: a three-month-old
rem exe reported results from three-month-old code and nothing said so.
rem
rem xcopy /L /D lists the source files newer than the destination and copies
rem nothing, which is the exact question. The rebuild happens only when a Go
rem toolchain is present; without one the launcher cannot fix the binary, so it
rem says so instead of pretending the run is current.
rem
rem Call with the repository root, or let it default to two levels up.
setlocal EnableExtensions
set "ROOT=%~1"
if not defined ROOT set "ROOT=%~dp0..\.."
pushd "%ROOT%" >nul 2>nul
if errorlevel 1 (
    echo WARNING: cannot enter repository root "%ROOT%".
    exit /b 1
)
set "ENGINE=bin\lenovo-driver.exe"
set "STALE="
if not exist "%ENGINE%" set "STALE=1"

if not defined STALE (
    for /f "delims=" %%F in ('xcopy /L /D /S /Y "cmd\*.go" "%ENGINE%" 2^>nul ^| findstr /i /e /c:".go"') do set "STALE=1"
)
if not defined STALE (
    for /f "delims=" %%F in ('xcopy /L /D /S /Y "internal\*.go" "%ENGINE%" 2^>nul ^| findstr /i /e /c:".go"') do set "STALE=1"
)
if not defined STALE (
    for /f "delims=" %%F in ('xcopy /L /D /Y "go.mod" "%ENGINE%" 2^>nul ^| findstr /i /e /c:".mod"') do set "STALE=1"
)

if not defined STALE (
    popd
    exit /b 0
)

set "GO_EXE="
for %%G in (go.exe) do if not "%%~$PATH:G"=="" set "GO_EXE=%%~$PATH:G"
if not defined GO_EXE if exist "%USERPROFILE%\.local\go\bin\go.exe" set "GO_EXE=%USERPROFILE%\.local\go\bin\go.exe"
if not defined GO_EXE if defined GOROOT if exist "%GOROOT%\bin\go.exe" set "GO_EXE=%GOROOT%\bin\go.exe"

if not defined GO_EXE (
    echo WARNING: %ENGINE% is older than the source tree, and no Go toolchain was found to rebuild it.
    echo WARNING: this run uses the old binary. Rebuild with: go build -o bin\lenovo-driver.exe .\cmd\lenovo-driver
    popd
    exit /b 0
)

echo Building Go engine from source...
"%GO_EXE%" build -o "%ENGINE%" ".\cmd\lenovo-driver"
set "BUILD_RC=%ERRORLEVEL%"
popd
if not "%BUILD_RC%"=="0" (
    echo ERROR: go build exited %BUILD_RC%.
    exit /b %BUILD_RC%
)
exit /b 0
