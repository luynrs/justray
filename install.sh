#!/bin/sh

set -efu

repo="https://github.com/luynrs/justray"
version="${JUSTRAY_VERSION:-latest}"

step() {
	printf '• %s' "$1"
	if [ ! -t 1 ]; then printf '\n'; fi
}

pass() {
	if [ -t 1 ]; then printf '\r\033[K'; fi
	printf '✓ %s\n' "$1"
}

fail() {
	if [ -t 1 ]; then printf '\r\033[K'; fi
	printf '✗ %s\n' "$1" >&2
	exit 1
}

case "$(uname -s)" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) fail "Unsupported OS: $(uname -s)" ;;
esac

case "$(uname -m)" in
	x86_64|amd64) arch=amd64 ;;
	arm64|aarch64) arch=arm64 ;;
	*) fail "Unsupported arch: $(uname -m)" ;;
esac

case "$version" in
	latest) base="$repo/releases/latest/download" ;;
	[0-9]*) base="$repo/releases/download/v$version" ;;
	*) base="$repo/releases/download/$version" ;;
esac

if [ -n "${JUSTRAY_INSTALL_DIR:-}" ]; then
	dir="$JUSTRAY_INSTALL_DIR"
else
	dir="$HOME/.local/bin"
	for candidate in "$HOME/.local/bin" "$HOME/bin" /usr/local/bin; do
		case ":$PATH:" in
			*":$candidate:"*)
				dir="$candidate"
				break
				;;
		esac
	done
fi

mkdir -p "$dir" 2>/dev/null || fail "Cannot create directory $dir"
[ -w "$dir" ] || fail "Cannot write to $dir"
dir=$(cd "$dir" && pwd -P)

tmp=$(mktemp -d "$dir/.justray.XXXXXXXX") || fail "Cannot create temporary directory in $dir"
restart=0

cleanup() {
	rm -rf "$tmp"

	if [ "$restart" -eq 1 ] && [ -x "$dir/justrayd" ]; then
		nohup "$dir/justrayd" >/dev/null 2>&1 </dev/null &
	fi
}

trap cleanup EXIT
trap 'exit 1' HUP INT TERM

step "Fetching release..."

checksums="$tmp/checksums.txt"
curl -fsSL --retry 3 "$base/checksums.txt" -o "$checksums" 2>/dev/null || fail "Failed to fetch release metadata"

line=$(
	awk -v platform="${os}_${arch}" '
		length($1) == 64 && $1 ~ /^[[:xdigit:]]+$/ {
			sub(/^\*/, "", $2)
			if ($2 ~ ("^justray_[^/[:space:]]+_" platform "\\.tar\\.gz$")) print tolower($1), $2
		}
	' "$checksums"
)

# shellcheck disable=SC2086
set -- $line
[ "$#" -eq 2 ] || fail "Expected exactly one release for ${os}_${arch}"

expected="$1"
archive="$2"

version=${archive#justray_}
version=${version%_"${os}_${arch}.tar.gz"}
pass "Found v$version for ${os}/${arch}"

step "Downloading $archive..."

curl -fsSL --retry 3 "$base/$archive" -o "$tmp/$archive" 2>/dev/null || fail "Failed to download $archive"

if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum < "$tmp/$archive" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
	actual=$(shasum -a 256 < "$tmp/$archive" | awk '{print $1}')
else
	fail "sha256sum or shasum is required"
fi

[ "$actual" = "$expected" ] || fail "Checksum mismatch"

pass "Verified checksum"

mkdir -p "$tmp/out"
tar -xzf "$tmp/$archive" -C "$tmp/out" 2>/dev/null || fail "Failed to extract archive"

for binary in justray justrayd; do
	[ -f "$tmp/out/$binary" ] || fail "Archive is missing $binary"
done
chmod 755 "$tmp/out/justray" "$tmp/out/justrayd"
ln -sf justray "$tmp/out/jray"

for binary in justray justrayd jray; do
	[ ! -d "$dir/$binary" ] || fail "$dir/$binary is a directory"
done

step "Installing..."

stopped=$("$tmp/out/justray" stop) || fail "Failed to stop daemon"
case "$stopped" in
	*"Daemon stopped"*) restart=1 ;;
esac

for binary in justrayd justray jray; do
	mv -f "$tmp/out/$binary" "$dir/$binary" || fail "Failed to install $binary"
done

pass "Installed to $dir"

case ":$PATH:" in
	*":$dir:"*)
		printf '\nRun jray in a new terminal window.\n'
		;;
	*)
		printf '\nAdd %s to your PATH, then run jray.\n' "$dir"
		;;
esac
