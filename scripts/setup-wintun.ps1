param(
    [ValidateSet("amd64", "arm64", "x86", "arm")]
    [string]$Architecture = "amd64"
)

$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$url = "https://www.wintun.net/builds/wintun-0.14.1.zip"
$expectedSha256 = "07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51"
$projectRoot = Split-Path -Parent $PSScriptRoot
$tempRoot = Join-Path ([IO.Path]::GetTempPath()) ("oberon-wintun-" + [Guid]::NewGuid().ToString("N"))
$zipPath = Join-Path $tempRoot "wintun.zip"

try {
    New-Item -ItemType Directory -Path $tempRoot | Out-Null
    Invoke-WebRequest -Uri $url -OutFile $zipPath -UseBasicParsing

    $actualSha256 = (Get-FileHash -LiteralPath $zipPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualSha256 -ne $expectedSha256) {
        throw "Wintun archive SHA-256 mismatch. Expected $expectedSha256, got $actualSha256."
    }

    $extractRoot = Join-Path $tempRoot "extract"
    Expand-Archive -LiteralPath $zipPath -DestinationPath $extractRoot
    $sourceDll = Join-Path $extractRoot ("wintun\bin\{0}\wintun.dll" -f $Architecture)
    if (-not (Test-Path -LiteralPath $sourceDll -PathType Leaf)) {
        throw "The Wintun archive does not contain the requested $Architecture DLL."
    }

    $destinationDll = Join-Path $projectRoot "wintun.dll"
    Copy-Item -LiteralPath $sourceDll -Destination $destinationDll -Force
    Write-Host "Installed matching Wintun $Architecture DLL beside the Oberon executable: $destinationDll"
}
finally {
    if (Test-Path -LiteralPath $tempRoot) {
        Remove-Item -LiteralPath $tempRoot -Recurse -Force
    }
}
