@echo off
setlocal EnableExtensions DisableDelayedExpansion
pushd "%~dp0" || exit /b 1
if /i "%~1"=="test" goto :test
if /i "%~1"=="check" goto :check
if /i "%~1"=="fmt" goto :fmt
if not "%~1"=="" if /i not "%~1"=="build" goto :usage
:build
go build -trimpath -ldflags="-s -w" -o portable-update.exe .\cmd\portable-updater
goto :done
:test
go test ./...
goto :done
:check
go vet ./...
if errorlevel 1 goto :done
go build ./...
if errorlevel 1 goto :done
go test ./...
goto :done
:fmt
gofmt -w cmd internal
goto :done
:done
set "RESULT=%ERRORLEVEL%"
popd
exit /b %RESULT%
:usage
echo Usage: build [build^|test^|check^|fmt]
popd
exit /b 1
