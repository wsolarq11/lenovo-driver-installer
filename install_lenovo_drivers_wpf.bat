@echo off
setlocal EnableExtensions
cd /d "%~dp0"

rem The WPF shell launches bin\lenovo-driver.exe as its worker, so it has the
rem same staleness exposure as the CLI launcher. Both call one script instead of
rem each deciding when to rebuild: two copies of this rule would drift, and the
rem copy nobody updated would be the one that silently ran old code.
call "%~dp0scripts\lib\ensure-engine.bat" "%~dp0."
if errorlevel 1 exit /b %errorlevel%

powershell.exe -STA -NoProfile -ExecutionPolicy Bypass -File "%~dp0lenovo_driver_wpf.ps1" %*
exit /b %errorlevel%
