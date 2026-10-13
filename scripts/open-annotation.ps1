# 文件用途：从本机快捷方式启动独立标注程序并打开页面；复用匹配样本的现有服务。
param(
    [string]$Preview = 'D:\app\MemorySpike\private\codex-preview-20261013-v2\preview.json',
    [string]$Binary = 'D:\app\MemorySpike\bin\memory-annotator.exe',
    [int]$Port = 8787
)
$ErrorActionPreference = 'Stop'
$annotationURL = "http://127.0.0.1:$Port"
$annotationDirectory = Split-Path -Parent $Preview
$annotationOutput = Join-Path $annotationDirectory 'annotation-review.json'

# Read-AnnotationService 只探测本机，不修改任何标注；连接失败返回空值供启动逻辑判断。
function Read-AnnotationService {
    try { return Invoke-RestMethod -Uri "$annotationURL/api/state" -TimeoutSec 2 }
    catch { return $null }
}

try {
    $annotationExpected = (Get-Content -LiteralPath $Preview -Raw -Encoding UTF8 | ConvertFrom-Json).sample_sha256
    $annotationService = Read-AnnotationService
    if (-not $annotationService) {
        if (-not (Test-Path -LiteralPath $Binary)) { throw '找不到标注程序，请让 Codex 重新构建并启动。' }
        $annotationArguments = '-preview "{0}" -out "{1}" -port {2}' -f $Preview, $annotationOutput, $Port
        Start-Process -FilePath $Binary -ArgumentList $annotationArguments -WindowStyle Hidden -RedirectStandardOutput (Join-Path $annotationDirectory 'annotation-server.log') -RedirectStandardError (Join-Path $annotationDirectory 'annotation-server-error.log') | Out-Null
        for ($annotationAttempt = 0; $annotationAttempt -lt 20; $annotationAttempt++) {
            $annotationService = Read-AnnotationService
            if ($annotationService) { break }
            Start-Sleep -Milliseconds 200
        }
    }
    if (-not $annotationService) { throw '标注服务未能启动。保存过的文件仍在，请让 Codex 查看 annotation-server-error.log。' }
    if ($annotationService.state.annotation.sample_sha256 -ne $annotationExpected) { throw '这个端口正在显示另一份样本。请让 Codex 检查，避免标错数据。' }
    Start-Process $annotationURL
} catch {
    # 隐藏启动窗口时仍显示可操作的错误，不把私人聊天内容放入提示。
    $annotationShell = New-Object -ComObject WScript.Shell
    $null = $annotationShell.Popup($_.Exception.Message, 0, '记忆标注未打开', 16)
    exit 1
}
