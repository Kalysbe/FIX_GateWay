@echo off
setlocal
chcp 65001 >nul
if not defined GATEWAY_NODE_DIR set "GATEWAY_NODE_DIR=%LOCALAPPDATA%\Programs\nodejs"
set "PATH=%GATEWAY_NODE_DIR%;C:\Program Files\Docker\Docker\resources\bin;%PATH%"
if not defined PM2_HOME set "PM2_HOME=%USERPROFILE%\.pm2"
cd /d "%~dp0.."
if exist "%GATEWAY_NODE_DIR%\node.exe" (
  "%GATEWAY_NODE_DIR%\node.exe" "%~dp0pm2-windows-boot.cjs"
) else (
  node "%~dp0pm2-windows-boot.cjs"
)
