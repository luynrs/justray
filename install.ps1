#Requires -Version 5.1

$ErrorActionPreference = "Stop"
try { [Console]::OutputEncoding = [Text.Encoding]::UTF8 } catch {}

$repo = "https://github.com/luynrs/justray"
$version = if ($env:JUSTRAY_VERSION) { $env:JUSTRAY_VERSION } else { "latest" }
$dir = if ($env:JUSTRAY_INSTALL_DIR) {
	$env:JUSTRAY_INSTALL_DIR
} else {
	Join-Path $env:LOCALAPPDATA "justray"
}

$isTty = [Environment]::UserInteractive -and -not [Console]::IsOutputRedirected
$ProgressPreference = if ($isTty -and $PSVersionTable.PSVersion.Major -ge 6) { "Continue" } else { "SilentlyContinue" }

function clear_line() {
	if ($isTty) {
		try {
			[Console]::SetCursorPosition(0, [Console]::CursorTop)
			Write-Host (" " * [Console]::BufferWidth) -NoNewline
			[Console]::SetCursorPosition(0, [Console]::CursorTop)
		} catch {}
	}
}

function step($message) {
	clear_line
	Write-Host "• $message" -NoNewline:$isTty
}

function done($message) {
	clear_line
	Write-Host "✓ $message"
}

function fail($message) {
	clear_line
	throw "✗ $message"
}

$nativeArch = if ($env:PROCESSOR_ARCHITEW6432) {
	$env:PROCESSOR_ARCHITEW6432
} else {
	$env:PROCESSOR_ARCHITECTURE
}

$arch = switch ($nativeArch) {
	"AMD64" { "amd64" }
	"ARM64" { "arm64" }
	default { fail "Unsupported architecture: $nativeArch" }
}

if ($version -match '^\d') {
	$version = "v$version"
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
			if ($i -eq 3) { throw $_ }
			step "Retrying download ($($i + 1)/3)..."
			Start-Sleep -Seconds (1 * $i)
		}
	}
}

$dir = (New-Item -ItemType Directory -Force -Path $dir).FullName
$tmp = Join-Path $dir (".justray." + [guid]::NewGuid().ToString("N"))
$restart = $false

New-Item -ItemType Directory -Path $tmp | Out-Null

try {
	step "Fetching release..."

	$checksums = Join-Path $tmp "checksums.txt"
	download "$base/checksums.txt" $checksums

	$lines = @(
		Get-Content -LiteralPath $checksums |
			Where-Object {
				$_ -match "^[0-9A-Fa-f]{64}\s+\*?justray_[^/\\\s]+_windows_$arch\.zip$"
			}
	)

	if ($lines.Count -ne 1) {
		throw "Expected exactly one release for windows_$arch"
	}

	$hash, $archive = $lines[0].Trim() -split '\s+', 2
	$archive = $archive.TrimStart("*")

	$version = $archive -replace "^justray_(.+)_windows_$arch\.zip$", '$1'
	done "Found v$version for windows/$arch"

	step "Downloading $archive..."

	$zip = Join-Path $tmp $archive
	download "$base/$archive" $zip
	done "Downloaded archive"

	step "Verifying checksum..."
	if ((Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash -ne $hash) {
		throw "Checksum mismatch"
	}

	done "Verified checksum"

	step "Extracting archive..."
	$out = Join-Path $tmp "out"
	Expand-Archive -LiteralPath $zip -DestinationPath $out -Force

	foreach ($exe in "justray.exe", "justrayd.exe") {
		if (-not (Test-Path -LiteralPath (Join-Path $out $exe) -PathType Leaf)) {
			throw "Archive is missing $exe"
		}
	}

	Copy-Item -LiteralPath "$out\justray.exe" -Destination "$out\jray.exe"
	foreach ($exe in "justray.exe", "justrayd.exe", "jray.exe") {
		if (Test-Path -LiteralPath "$dir\$exe" -PathType Container) {
			throw "$dir\$exe is a directory"
		}
	}
	done "Extracted archive"

	step "Stopping daemon..."

	$restart = (& "$out\justray.exe" stop) -match 'Daemon stopped'
	if ($LASTEXITCODE) {
		throw "Failed to stop daemon"
	}
	done $(if ($restart) { "Daemon stopped" } else { "Daemon is not running" })

	step "Installing..."

	# Windows allows renaming running binaries away, but forbids overwriting them in place
	$backup = [guid]::NewGuid().ToString("N")
	try {
		foreach ($exe in "justrayd.exe", "justray.exe", "jray.exe") {
			if (Test-Path -LiteralPath "$dir\$exe") {
				Move-Item -LiteralPath "$dir\$exe" -Destination "$dir\$exe.old.$backup"
			}
			Move-Item -LiteralPath "$out\$exe" -Destination "$dir\$exe"
		}
	} catch {
		foreach ($exe in "justrayd.exe", "justray.exe", "jray.exe") {
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

	$userPath = [string][Environment]::GetEnvironmentVariable("Path", "User")

	if (($userPath -split ";") -notcontains $dir) {
		$userPath = "$($userPath.TrimEnd(";"));$dir".TrimStart(";")
		[Environment]::SetEnvironmentVariable("Path", $userPath, "User")
	}

	if (($env:Path -split ";") -notcontains $dir) {
		$env:Path = "$($env:Path.TrimEnd(";"));$dir"
	}

	if ($restart) {
		$restart = $false
		step "Restarting daemon..."
		Start-Process "$dir\justrayd.exe" -WindowStyle Hidden
		for ($attempt = 0; $attempt -lt 45; $attempt++) {
			$status = & "$dir\justray.exe" status --json 2>$null
			if ($status) { break }
			Start-Sleep -Seconds 1
		}
		& "$dir\justray.exe" status > $null
		if ($LASTEXITCODE) { throw "Failed to restore daemon" }
		done "Daemon ready"
	}
	done "Installed to $dir"
	Write-Host "`nRun jray in a new terminal window."
}
catch {
	fail $_.Exception.Message
}
finally {
	Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue

	if ($restart -and (Test-Path -LiteralPath "$dir\justrayd.exe")) {
		Write-Host "• Restarting daemon..."
		Start-Process "$dir\justrayd.exe" -WindowStyle Hidden
	}
}
