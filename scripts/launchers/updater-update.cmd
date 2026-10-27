@echo off
setlocal EnableExtensions DisableDelayedExpansion
"%~dp0util\portable-update\portable-update.exe"
if errorlevel 1 goto :error
timeout /t 3 /nobreak >nul
exit /b 0
:error
pause
exit /b 1
