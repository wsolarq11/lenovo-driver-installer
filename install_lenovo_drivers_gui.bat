@echo off
setlocal EnableExtensions
cd /d "%~dp0"

rem The GUI shares the CLI's engine binary, so it has the same staleness
rem exposure; both launchers call one script instead of each deciding when
rem to rebuild. See scripts\lib\ensure-engine.bat.
call "%~dp0scripts\lib\ensure-engine.bat" "%~dp0."
if errorlevel 1 exit /b %errorlevel%

rem The window elevates itself once through UAC; the GUI takes no flags
rem (IncludeBios / SkipHashCheck are checkboxes in the window).
"bin\lenovo-driver.exe" -Gui
exit /b %errorlevel%
