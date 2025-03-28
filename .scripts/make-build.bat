@echo off
REM Building the binary that is going to be released
set GOOS=windows
set GOARCH=amd64
go build -o bin\nebula-on-premise-windows.exe cmd\nebula-on-premise-windows/main.go
