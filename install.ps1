[CmdletBinding()]
param(
    [string]$Version = $env:MIMIR_VERSION,
    [string]$BinDir = $env:MIMIR_INSTALL_DIR,
    [string[]]$Harness
)

$ErrorActionPreference = "Stop"

function Test-WritableDirectory([string]$Path) {
    $probe = Join-Path $Path (".mimir-write-test-" + [Guid]::NewGuid().ToString("N"))
    try {
        $stream = [IO.File]::Open($probe, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
        $stream.Dispose()
        Remove-Item -LiteralPath $probe -Force
        return $true
    } catch {
        if (Test-Path -LiteralPath $probe) {
            Remove-Item -LiteralPath $probe -Force -ErrorAction SilentlyContinue
        }
        return $false
    }
}

function Install-Mimir {
    $isWindowsPlatform = $env:OS -eq "Windows_NT"
    if (Get-Variable -Name IsWindows -ErrorAction SilentlyContinue) {
        $isWindowsPlatform = $IsWindows
    }
    if (-not $isWindowsPlatform) {
        throw "unsupported operating system; use install.sh on macOS or Linux"
    }

    $architecture = [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
    switch ($architecture) {
        "X64" { $arch = "amd64" }
        "Arm64" { $arch = "arm64" }
        default { throw "unsupported architecture: $architecture" }
    }

    $repository = if ($env:MIMIR_GITHUB_REPOSITORY) { $env:MIMIR_GITHUB_REPOSITORY } else { "cloudboy-jh/mimir" }
    $apiUrl = if ($env:MIMIR_GITHUB_API_URL) { $env:MIMIR_GITHUB_API_URL.TrimEnd('/') } else { "https://api.github.com" }
    $releasesUrl = if ($env:MIMIR_RELEASES_URL) { $env:MIMIR_RELEASES_URL.TrimEnd('/') } else { "https://github.com/$repository/releases" }

    if ([string]::IsNullOrWhiteSpace($Version)) {
        try {
            $release = Invoke-RestMethod -Uri "$apiUrl/repos/$repository/releases/latest"
        } catch {
            throw "could not resolve the latest stable release: $($_.Exception.Message)"
        }
        if ($release.prerelease -eq $true -or $release.draft -eq $true) {
            throw "GitHub returned a draft or prerelease for the stable channel"
        }
        $tag = [string]$release.tag_name
    } else {
        $tag = $Version.Trim()
    }

    if (-not $tag.StartsWith("v")) {
        $tag = "v$tag"
    }
    $assetVersion = $tag.Substring(1)
    if ([string]::IsNullOrWhiteSpace($assetVersion)) {
        throw "invalid release version: $tag"
    }

    $archiveName = "mimir_${assetVersion}_windows_${arch}.zip"
    $downloadBase = "$releasesUrl/download/$tag"
    $tempRoot = Join-Path ([IO.Path]::GetTempPath()) ("mimir-install-" + [Guid]::NewGuid().ToString("N"))
    $archivePath = Join-Path $tempRoot $archiveName
    $checksumsPath = Join-Path $tempRoot "checksums.txt"
    $extractRoot = Join-Path $tempRoot "extract"

    New-Item -ItemType Directory -Path $extractRoot -Force | Out-Null
    try {
        try {
            Invoke-WebRequest -UseBasicParsing -Uri "$downloadBase/$archiveName" -OutFile $archivePath
        } catch {
            throw "release asset not found: $archiveName"
        }
        try {
            Invoke-WebRequest -UseBasicParsing -Uri "$downloadBase/checksums.txt" -OutFile $checksumsPath
        } catch {
            throw "checksums.txt was not found for the release"
        }

        $pattern = '^([0-9A-Fa-f]{64})\s+\*?' + [Regex]::Escape($archiveName) + '$'
        $checksumMatches = @(Get-Content -LiteralPath $checksumsPath | Where-Object { $_ -match $pattern })
        if ($checksumMatches.Count -ne 1) {
            throw "checksum entry not found for $archiveName"
        }
        $null = $checksumMatches[0] -match $pattern
        $expected = $Matches[1].ToLowerInvariant()
        $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash.ToLowerInvariant()
        if ($actual -ne $expected) {
            throw "checksum mismatch for $archiveName"
        }

        Expand-Archive -LiteralPath $archivePath -DestinationPath $extractRoot -Force
        $binary = Join-Path $extractRoot "mimir_${assetVersion}_windows_${arch}\mimir.exe"
        if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) {
            throw "archive did not contain the expected Mimir binary"
        }

        if ([string]::IsNullOrWhiteSpace($BinDir)) {
            $existing = Get-Command mimir.exe -CommandType Application -ErrorAction SilentlyContinue
            if ($existing) {
                $BinDir = Split-Path -Parent $existing.Source
            }
        }
        if ([string]::IsNullOrWhiteSpace($BinDir)) {
            $userRoot = [IO.Path]::GetFullPath($env:USERPROFILE).TrimEnd('\')
            foreach ($candidate in ($env:Path -split ';')) {
                if ([string]::IsNullOrWhiteSpace($candidate)) { continue }
                try { $full = [IO.Path]::GetFullPath($candidate).TrimEnd('\') } catch { continue }
                $insideUserRoot = $full.Equals($userRoot, [StringComparison]::OrdinalIgnoreCase) -or $full.StartsWith($userRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)
                if ($insideUserRoot -and (Test-Path -LiteralPath $full -PathType Container) -and (Test-WritableDirectory $full)) {
                    $BinDir = $full
                    break
                }
            }
        }
        if ([string]::IsNullOrWhiteSpace($BinDir)) {
            $localRoot = if ($env:LOCALAPPDATA) { $env:LOCALAPPDATA } else { Join-Path $env:USERPROFILE ".local" }
            $BinDir = Join-Path $localRoot "Mimir\bin"
        }
        $BinDir = [IO.Path]::GetFullPath($BinDir)

        $priorSource = $env:MIMIR_INSTALL_SOURCE
        try {
            $env:MIMIR_INSTALL_SOURCE = "release"
            $installArgs = @("install", "--bin-dir", $BinDir)
            foreach ($id in $Harness) {
                $installArgs += @("--harness", $id)
            }
            & $binary @installArgs
            if ($LASTEXITCODE -ne 0) {
                throw "Mimir exited with status $LASTEXITCODE"
            }
        } finally {
            $env:MIMIR_INSTALL_SOURCE = $priorSource
        }

        $pathEntries = @($env:Path -split ';' | ForEach-Object { $_.TrimEnd('\') })
        if ($pathEntries -notcontains $BinDir.TrimEnd('\')) {
            $escaped = $BinDir.Replace("'", "''")
            Write-Output ""
            Write-Output "Mimir was installed outside PATH. Run:"
            Write-Output "  [Environment]::SetEnvironmentVariable('Path', '$escaped;' + [Environment]::GetEnvironmentVariable('Path', 'User'), 'User')"
        }
    } finally {
        if (Test-Path -LiteralPath $tempRoot) {
            Remove-Item -LiteralPath $tempRoot -Recurse -Force
        }
    }
}

try {
    Install-Mimir
} catch {
    throw "mimir install: $($_.Exception.Message)"
}
