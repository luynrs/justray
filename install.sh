#!/bin/sh

set -efu

repo="https://github.com/luynrs/justray"
version="${JUSTRAY_VERSION:-latest}"

log() {
	printf '%s\n' "$1"
}

fail() {
	printf '%s\n' "$1" >&2
	exit 1
}

case "$(uname -s)" in
	Linux) os=linux ;;
	Darwin) os=macos ;;
	*) fail "Unsupported OS: $(uname -s)" ;;
esac

case "$(uname -m)" in
	x86_64|amd64) arch=amd64 ;;
	arm64|aarch64) arch=arm64 ;;
	*) fail "Unsupported arch: $(uname -m)" ;;
esac

case "$version" in
	latest) base="$repo/releases/latest/download" ;;
	*) base="$repo/releases/download/$version" ;;
esac

dir="${JUSTRAY_DIRECTORY:-$HOME/.local/bin}"
if [ -z "${JUSTRAY_DIRECTORY:-}" ]; then
	for candidate in "$HOME/.local/bin" "$HOME/bin" /usr/local/bin; do
		case ":$PATH:" in
			*":$candidate:"*) ;;
			*) continue ;;
		esac
		[ "$candidate" = /usr/local/bin ] || mkdir -p "$candidate" 2>/dev/null || continue
		[ -d "$candidate" ] && [ -w "$candidate" ] || continue
		dir="$candidate"
		break
	done
fi

mkdir -p "$dir" 2>/dev/null || fail "Cannot create directory $dir"
[ -w "$dir" ] || fail "Cannot write to $dir"
dir=$(cd "$dir" && pwd -P)

tmp=$(mktemp -d "$dir/.justray.XXXXXXXX") || fail "Cannot create temporary directory in $dir"
restart=0
installed=""
binaries="justrayd justray jray"

start_daemon() {
	log "Restarting daemon..."
	nohup "$dir/justrayd" >/dev/null 2>&1 </dev/null &
	deadline=$(($(date +%s) + 30))
	until [ -n "$("$dir/justray" status --json 2>/dev/null)" ]; do
		[ "$(date +%s)" -lt "$deadline" ] || return 1
		sleep 1
	done
}

restore() {
	if [ "$restart" -eq 1 ] && [ -x "$dir/justray" ]; then
		"$dir/justray" stop >/dev/null 2>&1 || [ "$?" -eq 2 ] || return 1
	fi
	for binary in $installed; do
		rm -f "$dir/$binary" || return 1
	done
	for binary in $binaries; do
		if [ -e "$tmp/backup/$binary" ] || [ -L "$tmp/backup/$binary" ]; then
			mv -f "$tmp/backup/$binary" "$dir/$binary" || return 1
		fi
	done
	if [ "$restart" -eq 1 ] && [ -x "$dir/justrayd" ]; then
		start_daemon || return 1
	fi
}

cleanup() {
	code=$?
	trap - EXIT HUP INT TERM
	if [ "$code" -ne 0 ] && [ -d "$tmp/backup" ]; then
		log "Restoring previous installation..."
		if ! restore; then
			printf 'Rollback failed; backups kept in %s\n' "$tmp" >&2
			exit 1
		fi
	fi
	rm -rf "$tmp"
	exit "$code"
}

trap cleanup EXIT
trap 'exit 1' HUP INT TERM

log "Fetching release..."

checksums="$tmp/checksums.txt"
curl -fsSL --retry 3 "$base/checksums.txt" -o "$checksums" 2>/dev/null || fail "Failed to fetch release metadata"

line=$(
	awk -v platform="${os}-${arch}" '
		length($1) == 64 && $1 ~ /^[[:xdigit:]]+$/ {
			sub(/^\*/, "", $2)
			if ($2 ~ ("^justray-[^/[:space:]]+-" platform "\\.tar\\.gz$")) print tolower($1), $2
		}
	' "$checksums"
)

# shellcheck disable=SC2086
set -- $line
[ "$#" -eq 2 ] || fail "Expected exactly one release for ${os}/${arch}"

expected="$1"
archive="$2"

version=${archive#justray-}
version=${version%-"$os-$arch.tar.gz"}
# The checksum filename pins the archive to the same release, even if latest changes.
base="$repo/releases/download/v$version"
log "Found v$version for ${os}/${arch}"

log "Downloading $archive..."
curl -fsSL --retry 3 "$base/$archive" -o "$tmp/$archive" || fail "Failed to download $archive"

log "Verifying checksum..."

if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum < "$tmp/$archive" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
	actual=$(shasum -a 256 < "$tmp/$archive" | awk '{print $1}')
else
	fail "sha256sum or shasum is required"
fi

[ "$actual" = "$expected" ] || fail "Checksum mismatch"

mkdir -p "$tmp/out"
tar -xzf "$tmp/$archive" -C "$tmp/out" 2>/dev/null || fail "Failed to extract archive"

for binary in justray justrayd; do
	[ -f "$tmp/out/$binary" ] || fail "Archive is missing $binary"
done
chmod 755 "$tmp/out/justray" "$tmp/out/justrayd"
ln -sf justray "$tmp/out/jray"

for binary in $binaries; do
	[ ! -d "$dir/$binary" ] || fail "$dir/$binary is a directory"
done

mkdir "$tmp/backup"
if "$tmp/out/justray" stop >/dev/null; then
	restart=1
	log "Stopped daemon"
else
	[ "$?" -eq 2 ] || fail "Failed to stop daemon"
fi

log "Installing..."

for binary in $binaries; do
	if [ -e "$dir/$binary" ] || [ -L "$dir/$binary" ]; then
		mv "$dir/$binary" "$tmp/backup/$binary" || fail "Failed to back up $binary"
	fi
	mv -f "$tmp/out/$binary" "$dir/$binary" || fail "Failed to install $binary"
	installed="$installed $binary"
done

if [ "$restart" -eq 1 ]; then
	start_daemon || fail "Failed to restore daemon"
fi

log "Installed to $dir"

case ":$PATH:" in
	*":$dir:"*)
		printf '\nRun jray in a new terminal window.\n'
		;;
	*)
		printf '\nAdd %s to your PATH, then run jray.\n' "$dir"
		;;
esac
