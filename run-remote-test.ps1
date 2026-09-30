param(
    [int]$Port = 18419,
    [string]$Upstream = ''
)

$ErrorActionPreference = 'Stop'
if ([string]::IsNullOrWhiteSpace($Upstream)) {
    $Upstream = Read-Host '请输入 CPAMP 管理地址（例如 https://cpamp.example.com）'
}
$parsedUpstream = $null
if (-not [Uri]::TryCreate($Upstream, [UriKind]::Absolute, [ref]$parsedUpstream) -or
    $parsedUpstream.Scheme -notin @('http', 'https') -or
    [string]::IsNullOrWhiteSpace($parsedUpstream.Host) -or
    -not [string]::IsNullOrEmpty($parsedUpstream.UserInfo)) {
    throw 'CPAMP 地址必须是有效的 HTTP/HTTPS 地址，且不能在 URL 内嵌密钥。'
}
$Upstream = $Upstream.Trim().TrimEnd('/')
$ProjectRoot = $PSScriptRoot
$SecretsDir = Join-Path $ProjectRoot 'secrets'
$env:HTTP_ADDR = "127.0.0.1:$Port"
$env:CPAMP_BASE_URL = $Upstream
$env:CPAMP_ADMIN_KEY_FILE = Join-Path $SecretsDir 'remote_test_cpamp_admin_key.txt'
$env:VIEWER_PUBLIC_ACCESS = 'true'
$env:VIEWER_SESSION_SECRET_FILE = Join-Path $SecretsDir 'remote_test_viewer_session_secret.txt'
$env:VIEWER_SECURE_COOKIES = 'false'
$env:HEALTHCHECK_URL = "http://127.0.0.1:$Port/health"

foreach ($secretPath in @(
    $env:CPAMP_ADMIN_KEY_FILE,
    $env:VIEWER_SESSION_SECRET_FILE
)) {
    if (-not (Test-Path -LiteralPath $secretPath)) {
        throw "缺少 secret 文件：$secretPath"
    }
}

& (Join-Path $ProjectRoot 'cpamp-viewer-remote-test.exe')
