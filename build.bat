@echo off
REM site2pdf build script
REM Output: dist\site2pdf.exe

set OUTPUT=dist\site2pdf.exe

if not exist dist mkdir dist

echo Building site2pdf...
go build -o %OUTPUT% ./cmd/site2pdf
if %ERRORLEVEL% neq 0 (
    echo Build FAILED
    exit /b 1
)

echo Build OK: %OUTPUT%
