@echo off
REM --- Build the binary ---
set GOOS=windows
set GOARCH=amd64
go build -o nebula-on-premise-windows.exe cmd\nebula-on-premise-windows/main.go
if errorlevel 1 exit /b 1

REM --- Get the current date in yyyy.MM.dd format using PowerShell ---
for /f %%a in ('powershell -command "(Get-Date).ToString('yyyy.MM.dd')"') do set current_date=%%a

REM --- Ensure that we are up to date with remote ---
git pull origin main
if errorlevel 1 exit /b 1

REM --- Get the current commit hash (shortened) ---
for /f "delims=" %%a in ('git rev-parse --short HEAD') do set commit_hash=%%a

REM --- Create the tag variable ---
set tag=v%current_date%-sha.%commit_hash%

echo The version tag is: %tag%

REM --- Create and push the tag ---
git tag -a %tag% -m "Release %tag%"
if errorlevel 1 exit /b 1
git push origin %tag%
if errorlevel 1 exit /b 1

echo Tag %tag% created and pushed successfully.
