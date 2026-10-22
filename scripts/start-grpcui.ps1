param(
    [string]$Target = 'localhost:8443',
    [int]$Port = 8090,
    [string]$ServerName = 'localhost',
    [switch]$MutualTLS
)

$ErrorActionPreference = 'Stop'
$projectDir = Split-Path $PSScriptRoot -Parent
$grpcuiExe = Join-Path $env:LOCALAPPDATA 'grpcui/grpcui.exe'
if (-not (Test-Path $grpcuiExe)) {
    throw "grpcui is missing at $grpcuiExe. Install the Windows x86_64 release from https://github.com/fullstorydev/grpcui/releases."
}
$certDir = Join-Path $projectDir 'backend/certs'
$requiredFiles = @('ca.crt')
if ($MutualTLS) {
    $requiredFiles += @('bridge.crt', 'bridge.key')
}
foreach ($name in $requiredFiles) {
    if (-not (Test-Path (Join-Path $certDir $name))) {
        throw "Missing $certDir/$name. See docs/realtime-grpc.md."
    }
}
$grpcuiArgs = @(
    '-bind', '127.0.0.1', '-port', $Port, '-open-browser=false',
    '-cacert', "$certDir/ca.crt", '-servername', $ServerName,
    '-use-reflection=false', '-import-path', "$projectDir/backend/proto",
    '-proto', 'realtime/v1/realtime.proto', '-max-msg-sz', '8388608',
    '-max-time', '30', '-default-header', 'authorization: Bearer <JWT>'
)
if ($MutualTLS) {
    $grpcuiArgs += @('-cert', "$certDir/bridge.crt", '-key', "$certDir/bridge.key")
}
$grpcuiArgs += $Target
& $grpcuiExe @grpcuiArgs
exit $LASTEXITCODE
