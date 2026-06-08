param(
  [string]$HostName = "47.254.14.112",
  [string]$User = "codex",
  [string]$IdentityFile = "$env:USERPROFILE\.ssh\id_ed25519",
  [string]$RemoteDir = "/opt/mailbox/app",
  [string]$Service = "mailbox",
  [string]$Package = "deploy\gptbox-runtime-deploy.tar.gz",
  [switch]$SkipCheck,
  [switch]$SkipPackage,
  [switch]$NoBuildCache
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$packagePath = if ([System.IO.Path]::IsPathRooted($Package)) {
  $Package
} else {
  Join-Path $repoRoot $Package
}
$packagePath = [System.IO.Path]::GetFullPath($packagePath)
$remote = "$User@$HostName"
$remotePackage = "$RemoteDir/$(Split-Path -Leaf $packagePath)"
$remoteUploadPackage = "$remotePackage.uploading"
$sshArgs = @("-o", "BatchMode=yes", "-o", "ConnectTimeout=12", "-i", $IdentityFile)

function Invoke-Step {
  param(
    [string]$Label,
    [scriptblock]$Command
  )

  Write-Host "==> $Label"
  & $Command
  if ($LASTEXITCODE -ne 0) {
    throw "$Label failed with exit code $LASTEXITCODE."
  }
}

function Invoke-Remote {
  param([string]$Command)

  ssh @sshArgs $remote $Command
  if ($LASTEXITCODE -ne 0) {
    throw "Remote command failed with exit code $LASTEXITCODE."
  }
}

Set-Location $repoRoot

if (!$SkipPackage) {
  $packageArgs = @()
  if ($SkipCheck) {
    $packageArgs += "-SkipCheck"
  }
  Invoke-Step "build runtime package" {
    powershell -ExecutionPolicy Bypass -File (Join-Path $repoRoot "deploy\build-runtime-package.ps1") @packageArgs
  }
}

if (!(Test-Path -LiteralPath $packagePath -PathType Leaf)) {
  throw "Runtime package is missing: $packagePath"
}

Invoke-Step "verify package entries" {
  $entries = @(tar -tzf $packagePath)
  if ($LASTEXITCODE -ne 0) {
    throw "Unable to inspect package: $packagePath"
  }
  $forbiddenEntries = @($entries | Where-Object {
    $_ -match '(^|/)\.env$' -or
    $_ -match '(^|/)node_modules(/|$)' -or
    $_ -match '\.tar\.gz$' -or
    $_ -match '\.log$' -or
    $_ -match '\.png$'
  })
  if ($forbiddenEntries.Count -gt 0) {
    throw "Package contains forbidden entries:`n$($forbiddenEntries -join [Environment]::NewLine)"
  }
}

Invoke-Step "check remote target" {
  Invoke-Remote "test -d '$RemoteDir' && test -f '$RemoteDir/.env'"
}

Invoke-Step "upload package" {
  scp -i $IdentityFile $packagePath "${remote}:$remoteUploadPackage"
}

$buildCommand = if ($NoBuildCache) {
  "docker compose build --no-cache '$Service'; docker compose up -d '$Service'"
} else {
  "docker compose up -d --build '$Service'"
}
$deployCommand = @"
set -e
cd '$RemoteDir'
timestamp=`$(date +%Y%m%d%H%M%S)
if [ -f gptbox-runtime-deploy.tar.gz ]; then cp -f gptbox-runtime-deploy.tar.gz gptbox-runtime-deploy.tar.gz.bak-`$timestamp; fi
mv -f '$(Split-Path -Leaf $packagePath).uploading' '$(Split-Path -Leaf $packagePath)'
tar -xzf '$(Split-Path -Leaf $packagePath)'
$buildCommand
for i in 1 2 3 4 5 6 7 8 9 10; do
  if curl -fsS http://127.0.0.1:8787/api/health; then
    echo
    docker logs --tail 80 '$Service'
    exit 0
  fi
  sleep 2
done
docker ps -a --filter name='$Service'
docker logs --tail 120 '$Service'
exit 1
"@

Invoke-Step "deploy and smoke check" {
  Invoke-Remote $deployCommand
}

Write-Host "Aliyun deployment completed: ${remote}:$RemoteDir"
