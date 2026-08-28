#!/bin/sh

set -eu

repo="https://github.com/luynrs/justray"
version="${JUSTRAY_VERSION:-latest}"

die() {
	echo "justray: $*" >&2
	exit 1
}

case "$(uname -s)" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) die "unsupported OS: $(uname -s)" ;;
esac

case "$(uname -m)" in
	x86_64|amd64) arch=amd64 ;;
	arm64|aarch64) arch=arm64 ;;
	*) die "unsupported arch: $(uname -m)" ;;
esac

if [ "$version" = latest ]; then
	base="$repo/releases/latest/download"
else
	base="$repo/releases/download/$version"
fi

if [ -n "${JUSTRAY_INSTALL_DIR:-}" ]; then
	dir="$JUSTRAY_INSTALL_DIR"
else
	dir=

	for candidate in "$HOME/.local/bin" "$HOME/bin" /usr/local/bin; do
		case ":$PATH:" in
			*":$candidate:"*)
				dir="$candidate"
				break
				;;
		esac
	done

	[ -n "$dir" ] || die "no suitable install directory found in PATH"
fi

sudo=
if ! mkdir -p "$dir" 2>/dev/null || [ ! -w "$dir" ]; then
	command -v sudo >/dev/null 2>&1 || die "cannot write to $dir"
	sudo=sudo
	$sudo mkdir -p "$dir"
fi

tmp=$(mktemp -d)
restart_daemon=0

cleanup() {
	rm -rf "$tmp"

	if [ "$restart_daemon" -eq 1 ] && [ -x "$dir/justrayd" ]; then
		nohup "$dir/justrayd" >/dev/null 2>&1 </dev/null &
	fi
}

trap cleanup EXIT
trap 'exit 1' HUP INT TERM

echo "justray: fetching release"

checksums="$tmp/checksums.txt"
curl -fsSL --retry 3 "$base/checksums.txt" -o "$checksums"

line=$(
	awk -v suffix="_${os}_${arch}.tar.gz" '
		$2 ~ ("justray_.*" suffix "$") { print $1, $2 }
	' "$checksums"
)

set -- $line
[ "$#" -eq 2 ] || die "expected exactly one release for ${os}_${arch}"

expected="$1"
archive="$2"

echo "justray: downloading $archive"

curl -fsSL --retry 3 "$base/$archive" -o "$tmp/$archive"

if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
	actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
else
	die "sha256sum or shasum is required"
fi

[ "$actual" = "$expected" ] || die "checksum mismatch"

mkdir "$tmp/out"
tar -xzf "$tmp/$archive" -C "$tmp/out"

[ -f "$tmp/out/justray" ] || die "archive is missing justray"
[ -f "$tmp/out/justrayd" ] || die "archive is missing justrayd"

if command -v pgrep >/dev/null 2>&1 &&
   pgrep -u "$(id -u)" -x justrayd >/dev/null 2>&1; then
	if [ -x "$dir/justray" ]; then
		"$dir/justray" down
	elif command -v jray >/dev/null 2>&1; then
		jray down
	fi
	pkill -TERM -u "$(id -u)" -x justrayd || true
	while pgrep -u "$(id -u)" -x justrayd >/dev/null 2>&1; do
		sleep 0.1 2>/dev/null || sleep 1
	done
	restart_daemon=1
fi

$sudo install -m 755 "$tmp/out/justrayd" "$dir/justrayd"
$sudo install -m 755 "$tmp/out/justray" "$dir/justray"
$sudo ln -sf justray "$dir/jray"

echo "justray: installed justray, jray, justrayd to $dir"
