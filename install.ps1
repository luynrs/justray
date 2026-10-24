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

$dir = (New-Item -ItemType Directory -Force -Path $dir).FullName
$tmp = Join-Path $dir (".justray." + [guid]::NewGuid().ToString("N"))
$restart = $false

New-Item -ItemType Directory -Path $tmp | Out-Null

try {
	Write-Host "Fetching release..."

	$checksums = Join-Path $tmp "checksums.txt"
	download "$base/checksums.txt" $checksums

	$lines = @(
		Get-Content -LiteralPath $checksums |
			Where-Object {
				$_ -match "^[0-9A-Fa-f]{64}\s+\*?justray[-_][^/\\\s]+[-_]windows[-_]$arch\.zip$"
			}
	)

	if ($lines.Count -ne 1) {
		throw "Expected exactly one release for windows_$arch"
	}

	$hash, $archive = $lines[0].Trim() -split '\s+', 2
	$archive = $archive.TrimStart("*")

	$version = $archive -replace "^justray[-_](.+)[-_]windows[-_]$arch\.zip$", '$1'
	Write-Host "Found v$version for windows/$arch"

	Write-Host "Downloading $archive..."

	$zip = Join-Path $tmp $archive
	download "$base/$archive" $zip

	Write-Host "Verifying checksum..."
	if ((Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash -ne $hash) {
		throw "Checksum mismatch"
	}

	Write-Host "Verified checksum"

	Write-Host "Extracting archive..."
	$out = Join-Path $tmp "out"
	Expand-Archive -LiteralPath $zip -DestinationPath $out -Force

	foreach ($exe in "justray.exe", "justrayd.exe") {
		if (-not (Test-Path -LiteralPath (Join-Path $out $exe) -PathType Leaf)) {
			throw "Archive is missing $exe"
		}
	}

	Copy-Item -LiteralPath "$out\justray.exe" -Destination "$out\jray.exe"
	$binaries = "justrayd.exe", "justray.exe", "jray.exe"
	foreach ($exe in $binaries) {
		if (Test-Path -LiteralPath "$dir\$exe" -PathType Container) {
			throw "$dir\$exe is a directory"
		}
	}

	& "$out\justray.exe" stop > $null
	switch ($LASTEXITCODE) {
		0 { $restart = $true; Write-Host "Stopped daemon" }
		2 {}
		default { throw "Failed to stop daemon" }
	}

	Write-Host "Installing..."

	# Windows allows renaming running binaries away, but forbids overwriting them in place
	$backup = [guid]::NewGuid().ToString("N")
	try {
		foreach ($exe in $binaries) {
			if (Test-Path -LiteralPath "$dir\$exe") {
				Move-Item -LiteralPath "$dir\$exe" -Destination "$dir\$exe.old.$backup"
			}
			Move-Item -LiteralPath "$out\$exe" -Destination "$dir\$exe"
		}
	} catch {
		foreach ($exe in $binaries) {
			if (-not (Test-Path -LiteralPath "$out\$exe")) {
				Remove-Item -LiteralPath "$dir\$exe" -Force -ErrorAction SilentlyContinue
			}
			if (Test-Path -LiteralPath "$dir\$exe.old.$backup") {
				Move-Item -LiteralPath "$dir\$exe.old.$backup" -Destination "$dir\$exe"
			}
		}
		throw
	}

	Get-ChildItem -LiteralPath $dir -File -Filter *.exe.old.* -ErrorAction SilentlyContinue | Where-Object Name -Match '^(justray|jray|justrayd)\.exe\.old\.[0-9a-f]{32}$' | Remove-Item -Force -ErrorAction SilentlyContinue

	foreach ($scope in "User", "Process") {
		$currentPath = [string][Environment]::GetEnvironmentVariable("Path", $scope)
		if (($currentPath -split ";") -notcontains $dir) {
			[Environment]::SetEnvironmentVariable("Path", "$($currentPath.TrimEnd(";"));$dir".TrimStart(";"), $scope)
		}
	}

	if ($restart) {
		$restart = $false
		Write-Host "Restarting daemon..."
		Start-Process "$dir\justrayd.exe" -WindowStyle Hidden
		$timer = [Diagnostics.Stopwatch]::StartNew()
		while ($true) {
			& "$dir\justray.exe" status --json > $null 2>$null
			if ($LASTEXITCODE -eq 0) { break }
			if ($timer.Elapsed.TotalSeconds -ge 30) { throw "Failed to restore daemon" }
			Start-Sleep -Milliseconds 50
		}
		Write-Host "Restarted daemon"
	}
	Write-Host "Installed to $dir"
	Write-Host "`nRun jray in a new terminal window."
}
finally {
	Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue

	if ($restart -and (Test-Path -LiteralPath "$dir\justrayd.exe")) {
		Write-Host "Restarting daemon..."
		Start-Process "$dir\justrayd.exe" -WindowStyle Hidden
	}
}
