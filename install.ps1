#Requires -Version 5.1

$ErrorActionPreference = "Stop"
try { [Console]::OutputEncoding = [Text.Encoding]::UTF8 } catch {}

$repo = "https://github.com/luynrs/justray"
$version = if ($env:JUSTRAY_VERSION) { $env:JUSTRAY_VERSION } else { "latest" }
$dir = if ($env:JUSTRAY_DIRECTORY) {
	$env:JUSTRAY_DIRECTORY
} else {
	Join-Path $env:LOCALAPPDATA "justray"
}

$ProgressPreference = "SilentlyContinue"

$nativeArch = if ($env:PROCESSOR_ARCHITEW6432) {
	$env:PROCESSOR_ARCHITEW6432
} else {
	$env:PROCESSOR_ARCHITECTURE
}

$arch = switch ($nativeArch) {
	"AMD64" { "amd64" }
	"ARM64" { "arm64" }
	default { throw "Unsupported architecture: $nativeArch" }
}

$base = if ($version -eq "latest") {
	"$repo/releases/latest/download"
} else {
	"$repo/releases/download/$version"
}

if ($PSVersionTable.PSVersion.Major -lt 6) {
	[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
}

function download($uri, $out) {
	for ($i = 1; $i -le 3; $i++) {
		try {
			Invoke-WebRequest -Uri $uri -OutFile $out -UseBasicParsing
			return
		} catch {
			if ($i -eq 3) { throw }
			Write-Host "Retrying download ($($i + 1)/3)..."
			Start-Sleep -Seconds $i
		}
	}
}

function startDaemon {
	# Windows PowerShell treats native stderr as an error even with 2>$null.
	$ErrorActionPreference = "Continue"
	Write-Host "Restarting daemon..."
	Start-Process "$dir\justrayd.exe" -WindowStyle Hidden -ErrorAction Stop
	$timer = [Diagnostics.Stopwatch]::StartNew()
	do {
		$status = & "$dir\justray.exe" status --json 2>$null
		if ($status) { return }
		if ($timer.Elapsed.TotalSeconds -ge 30) { throw "Failed to restore daemon" }
		Start-Sleep -Milliseconds 250
	} while ($true)
}

$dir = (New-Item -ItemType Directory -Force -Path $dir).FullName
$tmp = Join-Path $dir (".justray." + [guid]::NewGuid().ToString("N"))
$backup = Join-Path $tmp "backup"
$restart = $false
$keepBackup = $false
$installed = @()
$binaries = "justrayd.exe", "justray.exe", "jray.exe"

New-Item -ItemType Directory -Path $tmp | Out-Null

try {
	Write-Host "Fetching release..."

	$checksums = Join-Path $tmp "checksums.txt"
	download "$base/checksums.txt" $checksums

	$pattern = "^([0-9A-Fa-f]{64})\s+\*?(justray-([^/\\\s]+)-windows-$arch\.zip)$"
	$releases = @(Select-String -LiteralPath $checksums -Pattern $pattern)
	if ($releases.Count -ne 1) {
		throw "Expected exactly one release for windows/$arch"
	}
	$hash, $archive, $version = $releases[0].Matches[0].Groups[1..3].Value
	# The checksum filename pins the archive to the same release, even if latest changes.
	$base = "$repo/releases/download/v$version"
	Write-Host "Found v$version for windows/$arch"

	Write-Host "Downloading $archive..."

	$zip = Join-Path $tmp $archive
	download "$base/$archive" $zip

	Write-Host "Verifying checksum..."
	if ((Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash -ne $hash) {
		throw "Checksum mismatch"
	}

	$out = Join-Path $tmp "out"
	Expand-Archive -LiteralPath $zip -DestinationPath $out -Force

	foreach ($exe in "justray.exe", "justrayd.exe") {
		if (-not (Test-Path -LiteralPath (Join-Path $out $exe) -PathType Leaf)) {
			throw "Archive is missing $exe"
		}
	}

	Copy-Item -LiteralPath "$out\justray.exe" -Destination "$out\jray.exe"
	foreach ($exe in $binaries) {
		if (Test-Path -LiteralPath "$dir\$exe" -PathType Container) {
			throw "$dir\$exe is a directory"
		}
	}

	New-Item -ItemType Directory -Path $backup | Out-Null
	& "$out\justray.exe" stop > $null
	switch ($LASTEXITCODE) {
		0 { $restart = $true; Write-Host "Stopped daemon" }
		2 {}
		default { throw "Failed to stop daemon" }
	}

	Write-Host "Installing..."

	# Windows allows renaming running binaries away, but forbids overwriting them in place
	foreach ($exe in $binaries) {
		if (Test-Path -LiteralPath "$dir\$exe") {
			Move-Item -LiteralPath "$dir\$exe" -Destination "$backup\$exe"
		}
		Move-Item -LiteralPath "$out\$exe" -Destination "$dir\$exe"
		$installed += $exe
	}
	foreach ($scope in "User", "Process") {
		$currentPath = [string][Environment]::GetEnvironmentVariable("Path", $scope)
		if (($currentPath -split ";") -notcontains $dir) {
			[Environment]::SetEnvironmentVariable("Path", "$($currentPath.TrimEnd(";"));$dir".TrimStart(";"), $scope)
		}
	}

	if ($restart) { startDaemon }
	Write-Host "Installed to $dir"
	Write-Host "`nRun jray in a new terminal window."
}
catch {
	$failure = $_
	if (Test-Path -LiteralPath $backup) {
		Write-Host "Restoring previous installation..."
		try {
			if ($restart -and (Test-Path -LiteralPath "$dir\justray.exe")) {
				& "$dir\justray.exe" stop > $null 2>$null
				if ($LASTEXITCODE -notin 0, 2) { throw "Failed to stop new daemon" }
			}
			foreach ($exe in $installed) {
				Move-Item -LiteralPath "$dir\$exe" -Destination "$out\$exe"
			}
			foreach ($exe in $binaries) {
				if (Test-Path -LiteralPath "$backup\$exe") {
					Move-Item -LiteralPath "$backup\$exe" -Destination "$dir\$exe"
				}
			}
			if ($restart -and (Test-Path -LiteralPath "$dir\justrayd.exe")) { startDaemon }
		} catch {
			$keepBackup = $true
			throw "Update failed: $failure. Rollback failed: $_. Backups kept in $tmp"
		}
	}
	throw $failure
}
finally {
	if (-not $keepBackup) {
		Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
	}
}
