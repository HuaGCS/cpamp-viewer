$ErrorActionPreference = 'Stop'
$env:HTTP_ADDR = '127.0.0.1:18417'
$env:CPAMP_BASE_URL = 'http://127.0.0.1:18318'
$env:CPAMP_ADMIN_KEY = 'cpamp-test-admin-key'
$env:VIEWER_PUBLIC_ACCESS = 'true'
$viewerSessionBytes = New-Object byte[] 48
$viewerRandom = [Security.Cryptography.RandomNumberGenerator]::Create()
try { $viewerRandom.GetBytes($viewerSessionBytes) } finally { $viewerRandom.Dispose() }
$env:VIEWER_SESSION_SECRET = [Convert]::ToBase64String($viewerSessionBytes)
$env:VIEWER_SECURE_COOKIES = 'false'
$env:HEALTHCHECK_URL = 'http://127.0.0.1:18417/health'
& (Join-Path $PSScriptRoot 'cpamp-viewer.exe')
