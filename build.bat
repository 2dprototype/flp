@echo off
go build -ldflags="-s -w" -o flp.exe ./cli/flp
go build -ldflags="-s -w -H windowsgui" -o flpgui.exe ./cli/flpgui
pause