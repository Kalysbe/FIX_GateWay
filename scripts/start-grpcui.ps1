param(
    [string]$Target = 'localhost:8443',
    [int]$Port = 8090,
    [string]$ServerName = 'localhost'
)

$ErrorActionPreference = 'Stop'
$projectDir = Split-Path $PSScriptRoot -Parent
$grpcuiExe = Join-Path $env:LOCALAPPDATA 'grpcui/grpcui.exe'
if (-not (Test-Path $grpcuiExe)) {
    throw "grpcui is missing at $grpcuiExe. Install the Windows x86_64 release from https://github.com/fullstorydev/grpcui/releases."
}
$certDir = Join-Path $projectDir 'backend/certs'
foreach ($name in @('ca.crt', 'bridge.crt', 'bridge.key')) {
    if (-not (Test-Path (Join-Path $certDir $name))) {
        throw "Missing $certDir/$name. See docs/realtime-grpc.md."
    }
}
& $grpcuiExe -bind 127.0.0.1 -port $Port -open-browser=false `
    -cacert "$certDir/ca.crt" -cert "$certDir/bridge.crt" -key "$certDir/bridge.key" `
    -servername $ServerName -use-reflection=false `
    -import-path "$projectDir/backend/proto" -proto realtime/v1/realtime.proto `
    -max-msg-sz 8388608 -max-time 30 `
    -default-header 'authorization: Bearer <JWT>' $Target
exit $LASTEXITCODE
