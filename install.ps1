#Requires -Version 5.1

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
try { [Console]::OutputEncoding = [Text.Encoding]::UTF8 } catch {}

$repo = "https://github.com/luynrs/justray"
$version = if ($env:JUSTRAY_VERSION) { $env:JUSTRAY_VERSION } else { "latest" }
$dir = if ($env:JUSTRAY_INSTALL_DIR) {
	$env:JUSTRAY_INSTALL_DIR
} else {
	Join-Path $env:LOCALAPPDATA "justray"
}

$isTty = [Environment]::UserInteractive -and -not [Console]::IsOutputRedirected

function clear_line() {
	if ($isTty) {
		try {
			[Console]::SetCursorPosition(0, [Console]::CursorTop)
			Write-Host (" " * [Console]::BufferWidth) -NoNewline
			[Console]::SetCursorPosition(0, [Console]::CursorTop)
		} catch {}
	}
}

function step($msg) {
	if ($isTty) {
		clear_line
		Write-Host "• $msg" -NoNewline
	} else {
		Write-Host "• $msg"
	}
}

function done($msg) {
	clear_line
	Write-Host "✓ $msg"
}

function fail($msg) {
	clear_line
	if ($msg.Length -gt 0) {
		$msg = $msg.Substring(0, 1).ToUpper() + $msg.Substring(1)
	}
	[Console]::Error.WriteLine("✗ $msg")
	if ($isTty -and -not [Console]::IsInputRedirected) {
		Write-Host "`nPress Enter to exit..." -NoNewline
		try { [Console]::ReadLine() | Out-Null } catch {}
	}
	exit 1
}

$nativeArch = if ($env:PROCESSOR_ARCHITEW6432) {
	$env:PROCESSOR_ARCHITEW6432
} else {
	$env:PROCESSOR_ARCHITECTURE
}

$arch = switch ($nativeArch) {
	"AMD64" { "amd64" }
	"ARM64" { "arm64" }
	default { fail "unsupported arch: $nativeArch" }
}

$tag = switch -Regex ($version) {
	'^latest$' { 'latest' }
	'^\d'      { "v$version" }
	default    { $version }
}

$base = if ($tag -eq "latest") {
	"$repo/releases/latest/download"
} else {
	"$repo/releases/download/$tag"
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
			Start-Sleep -Seconds (1 * $i)
		}
	}
}

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("justray-" + [guid]::NewGuid())
$restart = $false

New-Item -ItemType Directory -Path $tmp | Out-Null

try {
	step "Fetching release..."

	$checksums = Join-Path $tmp "checksums.txt"
	try {
		download "$base/checksums.txt" $checksums
	} catch {
		fail "failed to fetch release metadata"
	}

	$lines = @(
		Get-Content $checksums |
			Where-Object {
				$_ -match "^[0-9A-Fa-f]{64}\s+\*?justray_.*_windows_$arch\.zip$"
			}
	)

	if ($lines.Count -ne 1) {
		fail "expected exactly one release for windows_$arch"
	}

	$hash, $archive = $lines[0].Trim() -split '\s+', 2
	$archive = $archive.TrimStart("*")

	$tag = if ($archive -match "^justray_(.+)_[^_]+_[^_]+\.zip$") { $Matches[1] } else { $version }
	done "Found v$tag for windows/$arch"

	step "Downloading $archive..."

	$zip = Join-Path $tmp $archive
	try {
		download "$base/$archive" $zip
	} catch {
		fail "failed to download $archive"
	}

	if ((Get-FileHash $zip -Algorithm SHA256).Hash.ToLowerInvariant() -ne $hash.ToLowerInvariant()) {
		fail "checksum mismatch"
	}

	done "Verified checksum"

	$out = Join-Path $tmp "out"
	try {
		Expand-Archive $zip -DestinationPath $out -Force
	} catch {
		fail "failed to extract archive"
	}

	foreach ($exe in "justray.exe", "justrayd.exe") {
		if (-not (Test-Path (Join-Path $out $exe) -PathType Leaf)) {
			fail "archive is missing $exe"
		}
	}

	step "Installing..."

	New-Item -ItemType Directory -Force -Path $dir | Out-Null

	$restart = (Get-Process justrayd -ErrorAction SilentlyContinue) -ne $null
	if (Test-Path "$dir\justray.exe") {
		try { & "$dir\justray.exe" stop *>$null } catch {}
	}
	Stop-Process -Name justrayd, justray, jray -ErrorAction SilentlyContinue

	# Windows allows renaming running binaries away, but forbids overwriting them in place
	Get-ChildItem $dir -Filter *.old* -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue

	function install_file($src, $dst) {
		if (Test-Path $dst) {
			Move-Item $dst ("$dst.old." + [guid]::NewGuid().ToString("N")) -Force -ErrorAction SilentlyContinue
		}
		Copy-Item $src $dst -Force
	}

	install_file "$out\justray.exe" "$dir\justray.exe"
	install_file "$out\justray.exe" "$dir\jray.exe"
	install_file "$out\justrayd.exe" "$dir\justrayd.exe"

	$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
	if ($null -eq $userPath) {
		$userPath = ""
	}

	if (($userPath -split ";") -notcontains $dir) {
		$userPath = "$($userPath.TrimEnd(";"));$dir".TrimStart(";")
		[Environment]::SetEnvironmentVariable("Path", $userPath, "User")
	}

	if (($env:Path -split ";") -notcontains $dir) {
		$env:Path = "$($env:Path.TrimEnd(";"));$dir"
	}

	done "Installed to $dir"
	Write-Host "`nTo get started, run jray in a new terminal window"
}
finally {
	Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
	Get-ChildItem $dir -Filter *.old* -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue

	if ($restart -and (Test-Path "$dir\justrayd.exe")) {
		Start-Process "$dir\justrayd.exe" -WindowStyle Hidden
	}
}
