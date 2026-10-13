# 文件用途：在 Windows 上选择 Go 工具链并将缓存留在 D 盘，统一启动实验命令。
param(
    [ValidateSet('test','fixtures','collect','prepare','run','report','inspect','codex','annotate','discover')]
    [string]$Action
)
$CommandArgs=$args
if (-not $Action) { throw '请指定 test、fixtures、collect、prepare、run、report、inspect、codex、discover 或 annotate。' }
$ErrorActionPreference='Stop'
$spikeGoCommand=Get-Command go -ErrorAction SilentlyContinue
$spikeGo=if ($spikeGoCommand) { $spikeGoCommand.Source } else { 'D:\app\MemorySpike\sdk\go\bin\go.exe' }
if (-not (Test-Path -LiteralPath $spikeGo)) { throw '需要 Go 1.26 或更高版本；当前机器的独立 SDK 位于 D:\app\MemorySpike\sdk\go。' }
$env:GOPATH='D:\app\MemorySpike\gopath'
$env:GOCACHE='D:\app\MemorySpike\cache'
$env:GOMODCACHE='D:\app\MemorySpike\modcache'
$spikeOldTemp=$env:TEMP
$spikeOldTmp=$env:TMP
$spikeTemp='D:\app\MemorySpike\cache\tmp'
New-Item -ItemType Directory -Path $spikeTemp -Force | Out-Null
$env:TEMP=$spikeTemp
$env:TMP=$spikeTemp
Push-Location (Join-Path $PSScriptRoot 'memory_spike')
try {
    if ($Action -eq 'test') {
        & $spikeGo test ./...
        if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }
        & $spikeGo vet ./...
    } else {
        & $spikeGo run "$Action.go" @CommandArgs
    }
    if ($LASTEXITCODE -ne 0) { throw "$Action 未完成，退出码 $LASTEXITCODE；请查看上方准备项或错误。" }
} finally {
    Pop-Location
    $env:TEMP=$spikeOldTemp
    $env:TMP=$spikeOldTmp
}
