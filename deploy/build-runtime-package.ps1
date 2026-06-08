param(
  [string]$Output = "deploy\gptbox-runtime-deploy.tar.gz",
  [switch]$SkipCheck,
  [switch]$Clean,
  [switch]$KeepRootArchive,
  [string]$Goos = "linux",
  [string]$Goarch = "amd64"
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$outputPath = if ([System.IO.Path]::IsPathRooted($Output)) {
  $Output
} else {
  Join-Path $repoRoot $Output
}
$outputPath = [System.IO.Path]::GetFullPath($outputPath)
$outputParent = Split-Path -Parent $outputPath
$stagingRoot = Join-Path $repoRoot "deploy\.runtime-package-work"
$stagingPath = Join-Path $stagingRoot "package"
$serverBinary = Join-Path $repoRoot "build\gptbox-server-$Goos-$Goarch"
$requiredDistIndex = Join-Path $repoRoot "dist\index.html"
$rootArchive = Join-Path $repoRoot "gptbox-runtime-deploy.tar.gz"
$allowedRoots = @("dist", "build", "Dockerfile.runtime", "docker-compose.yml")

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

function Assert-NonEmptyFile {
  param([string]$Path)

  if (!(Test-Path -LiteralPath $Path -PathType Leaf)) {
    throw "Required file is missing: $Path"
  }

  if ((Get-Item -LiteralPath $Path).Length -le 0) {
    throw "Required file is empty: $Path"
  }
}

function Copy-PackageItem {
  param([string]$Name)

  $source = Join-Path $repoRoot $Name
  $destination = Join-Path $stagingPath $Name
  if (!(Test-Path -LiteralPath $source)) {
    throw "Package whitelist item is missing: $Name"
  }

  Copy-Item -LiteralPath $source -Destination $destination -Recurse -Force
}

function Copy-BuildOutput {
  $destination = Join-Path $stagingPath "build"
  New-Item -ItemType Directory -Path $destination -Force | Out-Null
  Copy-Item -LiteralPath $serverBinary -Destination (Join-Path $destination (Split-Path -Leaf $serverBinary)) -Force
}

function Test-ForbiddenArchiveEntry {
  param([string]$Entry)

  $normalized = $Entry -replace "\\", "/"
  return (
    $normalized -match '(^|/)\.env$' -or
    $normalized -match '(^|/)\.env\.local$' -or
    $normalized -match '\.log$' -or
    $normalized -match '\.png$' -or
    $normalized -match '\.tar\.gz$' -or
    $normalized -match '(^|/)node_modules(/|$)'
  )
}

function Test-AllowedArchiveEntry {
  param([string]$Entry)

  $rootEntry = ($Entry -replace "\\", "/").TrimStart("./")
  if ([string]::IsNullOrWhiteSpace($rootEntry)) {
    return $true
  }

  $rootName = ($rootEntry -split "/")[0]
  return $allowedRoots -contains $rootName
}

function Test-IsInsideRepository {
  param([string]$Path)

  $resolvedRoot = [System.IO.Path]::GetFullPath($repoRoot)
  $fullPath = [System.IO.Path]::GetFullPath($Path)
  return $fullPath.StartsWith($resolvedRoot, [System.StringComparison]::OrdinalIgnoreCase)
}

function Remove-GeneratedPath {
  param(
    [string]$Path,
    [switch]$Recurse
  )

  $resolvedRoot = [System.IO.Path]::GetFullPath($repoRoot)
  $fullPath = [System.IO.Path]::GetFullPath($Path)
  if (!$fullPath.StartsWith($resolvedRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing to remove a generated path outside the repository: $fullPath"
  }

  if (Test-Path -LiteralPath $fullPath) {
    if ($Recurse) {
      Remove-Item -LiteralPath $fullPath -Recurse -Force
    } else {
      Remove-Item -LiteralPath $fullPath -Force
    }
  }
}

function Remove-GeneratedPathIfInRepository {
  param(
    [string]$Path,
    [switch]$Recurse
  )

  if (Test-IsInsideRepository $Path) {
    Remove-GeneratedPath -Path $Path -Recurse:$Recurse
  }
}

Set-Location $repoRoot

if ($Clean) {
  Remove-GeneratedPath (Join-Path $repoRoot "dist") -Recurse
  Remove-GeneratedPath $serverBinary
  Remove-GeneratedPathIfInRepository $outputPath
  Remove-GeneratedPath $stagingRoot -Recurse
  if (!$KeepRootArchive -and $rootArchive -ne $outputPath) {
    Remove-GeneratedPath $rootArchive
  }
}

if ($SkipCheck) {
  Invoke-Step "npm run build" { npm run build }
} else {
  Invoke-Step "npm run check" { npm run check }
}

Invoke-Step "go build gptbox server for $Goos/$Goarch" {
  Push-Location (Join-Path $repoRoot "server")
  try {
    $previousCgo = $env:CGO_ENABLED
    $previousGoos = $env:GOOS
    $previousGoarch = $env:GOARCH
    $env:CGO_ENABLED = "0"
    $env:GOOS = $Goos
    $env:GOARCH = $Goarch
    go build -o "..\build\gptbox-server-$Goos-$Goarch" .
  } finally {
    $env:CGO_ENABLED = $previousCgo
    $env:GOOS = $previousGoos
    $env:GOARCH = $previousGoarch
    Pop-Location
  }
}

Assert-NonEmptyFile $requiredDistIndex
Assert-NonEmptyFile $serverBinary

New-Item -ItemType Directory -Path $stagingPath -Force | Out-Null
Get-ChildItem -LiteralPath $stagingPath -Force | Remove-Item -Recurse -Force
New-Item -ItemType Directory -Path $outputParent -Force | Out-Null

foreach ($item in $allowedRoots) {
  if ($item -eq "build") {
    Copy-BuildOutput
    continue
  }
  Copy-PackageItem $item
}

Invoke-Step "create runtime archive" {
  tar -czf $outputPath -C $stagingPath @allowedRoots
}

$archiveEntries = @(tar -tzf $outputPath)
if ($LASTEXITCODE -ne 0) {
  throw "Unable to inspect archive: $outputPath"
}

$unexpectedEntries = @($archiveEntries | Where-Object { !(Test-AllowedArchiveEntry $_) })
if ($unexpectedEntries.Count -gt 0) {
  throw "Archive contains entries outside the package whitelist:`n$($unexpectedEntries -join [Environment]::NewLine)"
}

$forbiddenEntries = @($archiveEntries | Where-Object { Test-ForbiddenArchiveEntry $_ })
if ($forbiddenEntries.Count -gt 0) {
  throw "Archive contains forbidden entries:`n$($forbiddenEntries -join [Environment]::NewLine)"
}

Write-Host "Runtime package created: $outputPath"

Remove-GeneratedPath $stagingRoot -Recurse
