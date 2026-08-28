#Requires -Version 5.1

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$repo = "https://github.com/luynrs/justray"
$version = if ($env:JUSTRAY_VERSION) { $env:JUSTRAY_VERSION } else { "latest" }
$dir = if ($env:JUSTRAY_INSTALL_DIR) {
	$env:JUSTRAY_INSTALL_DIR
} else {
	Join-Path $env:LOCALAPPDATA "justray"
}

$nativeArch = if ($env:PROCESSOR_ARCHITEW6432) {
	$env:PROCESSOR_ARCHITEW6432
} else {
	$env:PROCESSOR_ARCHITECTURE
}

$arch = switch ($nativeArch) {
	"AMD64" { "amd64" }
	"ARM64" { "arm64" }
	default { throw "justray: unsupported arch: $nativeArch" }
}

$base = if ($version -eq "latest") {
	"$repo/releases/latest/download"
} else {
	"$repo/releases/download/$version"
}

if ($PSVersionTable.PSVersion.Major -lt 6) {
	[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
}

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("justray-" + [guid]::NewGuid())
$restartDaemon = $false

New-Item -ItemType Directory -Path $tmp | Out-Null

try {
	Write-Host "justray: fetching release"

	$checksums = Join-Path $tmp "checksums.txt"
	Invoke-WebRequest "$base/checksums.txt" -OutFile $checksums -UseBasicParsing

	$lines = @(
		Get-Content $checksums |
			Where-Object {
				$_ -match "^[0-9A-Fa-f]{64}\s+\*?justray_.*_windows_$arch\.zip$"
			}
	)

	if ($lines.Count -ne 1) {
		throw "justray: expected exactly one release for windows_$arch"
	}

	$hash, $archive = $lines[0].Trim() -split '\s+', 2
	$archive = $archive.TrimStart("*")

	Write-Host "justray: downloading $archive"

	$zip = Join-Path $tmp $archive
	Invoke-WebRequest "$base/$archive" -OutFile $zip -UseBasicParsing

	if ((Get-FileHash $zip -Algorithm SHA256).Hash -ne $hash) {
		throw "justray: checksum mismatch"
	}

	$out = Join-Path $tmp "out"
	Expand-Archive $zip -DestinationPath $out -Force

	foreach ($exe in "justray.exe", "justrayd.exe") {
		if (-not (Test-Path (Join-Path $out $exe) -PathType Leaf)) {
			throw "justray: archive is missing $exe"
		}
	}

	New-Item -ItemType Directory -Force -Path $dir | Out-Null

	if (Get-Process justrayd -ErrorAction SilentlyContinue) {
		if (Test-Path "$dir\justray.exe") {
			& "$dir\justray.exe" down
		}
		Stop-Process -Name justrayd -ErrorAction SilentlyContinue
		while (Get-Process justrayd -ErrorAction SilentlyContinue) {
			Start-Sleep -Milliseconds 100
		}
		$restartDaemon = $true
	}

	Copy-Item "$out\justrayd.exe" "$dir\justrayd.exe" -Force
	Copy-Item "$out\justray.exe" "$dir\justray.exe" -Force
	Copy-Item "$out\justray.exe" "$dir\jray.exe" -Force

	$userPath = [Environment]::GetEnvironmentVariable("Path", "User")

	if (($userPath -split ";") -notcontains $dir) {
		$userPath = "$($userPath.TrimEnd(";"));$dir".TrimStart(";")
		[Environment]::SetEnvironmentVariable("Path", $userPath, "User")
	}

	if (($env:Path -split ";") -notcontains $dir) {
		$env:Path = "$($env:Path.TrimEnd(";"));$dir"
	}

	Write-Host "justray: installed justray, jray, justrayd to $dir"
}
finally {
	Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue

	if ($restartDaemon -and -not (Get-Process justrayd -ErrorAction SilentlyContinue) -and (Test-Path "$dir\justrayd.exe")) {
		Start-Process "$dir\justrayd.exe" -WindowStyle Hidden
	}
}
