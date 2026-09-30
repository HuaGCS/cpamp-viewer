param(
    [ValidateRange(1, 65535)]
    [int]$Port = 18418,
    [string]$Upstream = '',
    [string]$BindAddress = '127.0.0.1'
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
$VersionPath = Join-Path $ProjectRoot 'VERSION'
if (-not (Test-Path -LiteralPath $VersionPath)) {
    throw "缺少版本文件 $VersionPath。"
}
$Version = (Get-Content -LiteralPath $VersionPath -Raw).Trim()
if ($Version -notmatch '^\d+\.\d+\.\d+(?:[.-][0-9A-Za-z.-]+)?$') {
    throw "VERSION 内容无效：$Version"
}
$ContainerName = "cpamp-viewer-$($Version.Replace('.', '-'))-remote-test"
$Image = "cpamp-viewer:$Version"
$OfflineImage = Join-Path $ProjectRoot "cpamp-viewer_${Version}_linux_amd64.tar.gz"
$OfflineChecksum = "${OfflineImage}.sha256"

New-Item -ItemType Directory -Force -Path $SecretsDir | Out-Null

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw '未找到 docker 命令，请先启动 Docker Desktop。'
}

function Read-SecretText([string]$Prompt) {
    $secure = Read-Host $Prompt -AsSecureString
    $ptr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
    try {
        return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr)
    }
    finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr)
    }
}

$adminKey = Read-SecretText "请输入 $Upstream 的 Manager Server 管理员密钥"
if ([string]::IsNullOrWhiteSpace($adminKey)) {
    throw '管理员密钥不能为空。'
}

$adminKeyPath = Join-Path $SecretsDir 'remote_test_cpamp_admin_key.txt'
$sessionSecretPath = Join-Path $SecretsDir 'remote_test_viewer_session_secret.txt'

[IO.File]::WriteAllText($adminKeyPath, $adminKey, [Text.UTF8Encoding]::new($false))
$sessionBytes = New-Object byte[] 48
[Security.Cryptography.RandomNumberGenerator]::Fill($sessionBytes)
[IO.File]::WriteAllText($sessionSecretPath, [Convert]::ToBase64String($sessionBytes), [Text.UTF8Encoding]::new($false))

$adminKey = $null

if (Test-Path $OfflineImage) {
    if (-not (Test-Path $OfflineChecksum)) {
        throw "找到离线镜像包，但缺少校验文件 $OfflineChecksum。"
    }
    $expectedHash = ((Get-Content -LiteralPath $OfflineChecksum -Raw).Trim() -split '\s+')[0].ToLowerInvariant()
    $actualHash = (Get-FileHash -Algorithm SHA256 -Path $OfflineImage).Hash.ToLowerInvariant()
    if ($expectedHash -ne $actualHash) {
        throw '离线镜像 SHA-256 校验失败，请重新获取完整交付包。'
    }
    Write-Host '正在加载 CPAMP Viewer 离线镜像……'
    & docker load -i $OfflineImage | Out-Host
    if ($LASTEXITCODE -ne 0) {
        throw '离线镜像加载失败。'
    }
}
else {
    & docker image inspect $Image *> $null
    if ($LASTEXITCODE -ne 0) {
        throw "未找到镜像 $Image，也未找到离线镜像包 $OfflineImage。"
    }
}

& docker image inspect $Image *> $null
if ($LASTEXITCODE -ne 0) {
    throw "镜像 $Image 加载后仍不可用。"
}
$imageVersion = (& docker image inspect $Image --format '{{ index .Config.Labels "org.opencontainers.image.version" }}').Trim()
if ($LASTEXITCODE -ne 0 -or $imageVersion -ne $Version) {
    throw "镜像版本标签不匹配，期望 $Version，实际 $imageVersion。"
}

& docker container inspect $ContainerName *> $null
if ($LASTEXITCODE -eq 0) {
    & docker rm -f $ContainerName | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "无法删除旧测试容器 $ContainerName。"
    }
}

$containerId = & docker run -d `
    --name $ContainerName `
    --restart unless-stopped `
    --read-only `
    --tmpfs '/tmp:size=16m,mode=1777' `
    --cap-drop ALL `
    --security-opt 'no-new-privileges:true' `
    -p "${BindAddress}:${Port}:18417" `
    -e 'HTTP_ADDR=0.0.0.0:18417' `
    -e "CPAMP_BASE_URL=$Upstream" `
    -e 'CPAMP_ADMIN_KEY_FILE=/run/secrets/cpamp_admin_key' `
    -e 'VIEWER_PUBLIC_ACCESS=true' `
    -e 'VIEWER_SESSION_SECRET_FILE=/run/secrets/viewer_session_secret' `
    -e 'VIEWER_SECURE_COOKIES=false' `
    --mount "type=bind,src=$adminKeyPath,dst=/run/secrets/cpamp_admin_key,readonly" `
    --mount "type=bind,src=$sessionSecretPath,dst=/run/secrets/viewer_session_secret,readonly" `
    $Image
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($containerId)) {
    throw 'Viewer 容器启动失败。'
}

Write-Host '正在等待 Viewer 和远程 CPAMP 健康检查……'
$healthy = $false
for ($i = 0; $i -lt 30; $i++) {
    Start-Sleep -Seconds 1
    try {
        $health = Invoke-RestMethod -Uri "http://127.0.0.1:$Port/health" -TimeoutSec 3
        if ($health.status -eq 'ok' -and $health.upstream -eq 'ok') {
            $healthy = $true
            break
        }
    }
    catch {}
}

if (-not $healthy) {
    Write-Host 'Viewer 未通过健康检查，最近日志：' -ForegroundColor Yellow
    & docker logs --tail 50 $ContainerName
    exit 1
}

Write-Host ''
Write-Host 'CPAMP Viewer 已启动。' -ForegroundColor Green
Write-Host "地址：http://127.0.0.1:$Port/management.html#/"
if ($BindAddress -eq '0.0.0.0') {
    Write-Host "局域网地址：http://本机IP:$Port/management.html#/"
}
Write-Host '公开只读模式：打开地址即可查看，无需登录密码。'
Write-Host "停止：docker rm -f $ContainerName"
