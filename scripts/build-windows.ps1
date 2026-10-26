# 构建 Windows 便携版、NSIS 安装包和 WiX MSI。
# 依赖：wails、makensis、WiX Toolset v3 的 candle.exe / light.exe 均在 PATH 中。
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Version,

    [ValidateSet('amd64')]
    [string]$Architecture = 'amd64'
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Version = $Version.Trim().TrimStart('v')
if ($Version -notmatch '^\d+\.\d+\.\d+$') {
    throw "Version must be numeric semver like 0.2.0 (got '$Version')."
}

$repo = Split-Path -Parent $PSScriptRoot
$bin = Join-Path $repo 'build\bin'
$projectFile = Join-Path $repo 'wails.json'
$wxs = Join-Path $repo 'build\windows\msi\NanoAPI.wxs'
$portable = Join-Path $bin "NanoAPI-$Version-x64.exe"
$installer = Join-Path $bin "NanoAPI-$Version-x64-installer.exe"
$msi = Join-Path $bin "NanoAPI-$Version-x64.msi"

foreach ($command in @('wails', 'makensis', 'candle.exe', 'light.exe')) {
    if (-not (Get-Command $command -ErrorAction SilentlyContinue)) {
        throw "Required tool '$command' is not on PATH. See README.md -> 构建与发布."
    }
}

$originalProject = Get-Content -LiteralPath $projectFile -Raw
try {
    # wails.json 的 info.productVersion 会进入 exe 资源与 NSIS 安装包元数据。
    $project = $originalProject | ConvertFrom-Json
    if (-not $project.PSObject.Properties['info']) {
        $project | Add-Member -NotePropertyName info -NotePropertyValue ([pscustomobject]@{}) -Force
    }
    $info = @{
        companyName    = 'NanoAPI'
        productName    = 'NanoAPI'
        productVersion = $Version
        comments       = 'Lightweight API testing tool'
    }
    foreach ($entry in $info.GetEnumerator()) {
        $project.info | Add-Member -NotePropertyName $entry.Key -NotePropertyValue $entry.Value -Force
    }
    $json = $project | ConvertTo-Json -Depth 20
    [IO.File]::WriteAllText($projectFile, $json, [Text.UTF8Encoding]::new($false))

    Push-Location -LiteralPath $repo
    try {
        & wails build `
            -platform "windows/$Architecture" `
            -clean `
            -s `
            -skipbindings `
            -webview2 browser `
            -nsis `
            -ldflags "-X main.appVersion=$Version"
        if ($LASTEXITCODE -ne 0) {
            throw "wails build failed with exit code $LASTEXITCODE"
        }
    }
    finally {
        Pop-Location
    }
}
finally {
    [IO.File]::WriteAllText($projectFile, $originalProject, [Text.UTF8Encoding]::new($false))
}

$appExe = Join-Path $bin 'NanoAPI.exe'
if (-not (Test-Path -LiteralPath $appExe)) {
    throw "Wails did not produce $appExe"
}

$builtInstaller = Get-ChildItem -LiteralPath $bin -Filter '*-installer.exe' -File |
    Select-Object -First 1
if (-not $builtInstaller) {
    throw 'NSIS installer was not produced. Ensure makensis is available to wails build -nsis.'
}
if ($builtInstaller.FullName -ne $installer) {
    Move-Item -LiteralPath $builtInstaller.FullName -Destination $installer -Force
}

Copy-Item -LiteralPath $appExe -Destination $portable -Force

$sourceDir = (Resolve-Path -LiteralPath $bin).Path
$wixobj = Join-Path $bin 'NanoAPI.wixobj'
try {
    & candle.exe -nologo "-dSourceDir=$sourceDir" "-dVersion=$Version" -out $wixobj $wxs
    if ($LASTEXITCODE -ne 0) {
        throw "candle.exe failed with exit code $LASTEXITCODE"
    }

    & light.exe -nologo -out $msi $wixobj
    if ($LASTEXITCODE -ne 0) {
        throw "light.exe failed with exit code $LASTEXITCODE"
    }
}
finally {
    Remove-Item -LiteralPath $wixobj -Force -ErrorAction SilentlyContinue
}

foreach ($artifact in @($portable, $installer, $msi)) {
    if (-not (Test-Path -LiteralPath $artifact)) {
        throw "Missing build artifact: $artifact"
    }
}

Write-Host ''
Write-Host 'Windows packages:'
Write-Host "  portable:  $portable"
Write-Host "  installer: $installer"
Write-Host "  msi:       $msi"
