#Requires -Version 5.1
[CmdletBinding()]
param(
  [string]$Version = "latest",
  [string]$InstallDir = "$env:LOCALAPPDATA\jack-ketch\bin"
)

$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$Repo = "anshiq/jack-ketch"
$Binaries = @("jack-ketch.exe", "jackketchd.exe", "jack-ketch-shim.exe")

if ($env:OS -ne "Windows_NT") {
  throw "This installer is for Windows. Use scripts/install.sh on Linux or macOS."
}

$arch = switch ($env:PROCESSOR_ARCHITECTURE.ToUpperInvariant()) {
  "AMD64" { "amd64" }
  "ARM64" { "arm64" }
  default { throw "Unsupported processor architecture: $env:PROCESSOR_ARCHITECTURE" }
}

if ($Version -eq "latest") {
  $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers @{ "User-Agent" = "jack-ketch-installer" }
  $Version = $release.tag_name
}
$Version = $Version.TrimStart("v")

$asset = "jack-ketch_${Version}_windows_${arch}.zip"
$base = "https://github.com/$Repo/releases/download/v$Version"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("jack-ketch-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tmp | Out-Null

try {
  $zip = Join-Path $tmp $asset
  $sums = Join-Path $tmp "checksums.txt"

  Write-Host "Downloading $asset..."
  Invoke-WebRequest -Uri "$base/$asset" -OutFile $zip -UseBasicParsing
  Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile $sums -UseBasicParsing

  $line = Get-Content $sums | Where-Object { (($_ -split '\s+')[1]) -eq $asset } | Select-Object -First 1
  if (-not $line) { throw "Checksum for $asset not found in checksums.txt" }
  $expected = ($line -split '\s+')[0]
  $actual = (Get-FileHash -Path $zip -Algorithm SHA256).Hash
  if ($actual -ne $expected.ToUpperInvariant()) { throw "Checksum mismatch for $asset" }

  $extract = Join-Path $tmp "extract"
  Expand-Archive -Path $zip -DestinationPath $extract -Force

  New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
  foreach ($bin in $Binaries) {
    Copy-Item -Path (Join-Path $extract $bin) -Destination (Join-Path $InstallDir $bin) -Force
  }

  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  $parts = @($userPath -split ';' | Where-Object { $_ })
  if ($parts -notcontains $InstallDir) {
    [Environment]::SetEnvironmentVariable("Path", (@($parts + $InstallDir) -join ';'), "User")
  }
  if (($env:Path -split ';') -notcontains $InstallDir) {
    $env:Path = "$InstallDir;$env:Path"
  }

  Write-Host "Installed jack-ketch $Version to $InstallDir"
  & (Join-Path $InstallDir "jack-ketch.exe") --version
} finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
