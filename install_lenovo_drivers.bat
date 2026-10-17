@echo off
setlocal EnableExtensions
cd /d "%~dp0"

rem The engine is rebuilt only when the source tree is newer than it; see
rem scripts\lib\ensure-engine.bat. A missing engine is built the same way.
call "%~dp0scripts\lib\ensure-engine.bat" "%~dp0."
if errorlevel 1 exit /b %errorlevel%

"bin\lenovo-driver.exe" %*
exit /b %errorlevel%
