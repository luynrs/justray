#!/bin/sh

set -efu

repo="https://github.com/luynrs/justray"
version="${JUSTRAY_VERSION:-latest}"

if [ -t 1 ]; then
	c_clr="\r\033[K"
else
	c_clr=""
fi

step() {
	if [ -n "$c_clr" ]; then
		printf "• %s" "$1"
	else
		printf "• %s\n" "$1"
	fi
}

pass() {
	if [ -n "$c_clr" ]; then
		printf "%b✓ %s\n" "$c_clr" "$1"
	else
		printf "✓ %s\n" "$1"
	fi
}

fail() {
	if [ -n "$c_clr" ]; then
		printf "%b" "$c_clr" >&2
	fi
	first=$(printf %.1s "$1" | tr '[:lower:]' '[:upper:]')
	rest=$(printf %s "$1" | cut -c 2-)
	printf "✗ %s%s\n" "$first" "$rest" >&2
	exit 1
}

case "$(uname -s)" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) fail "unsupported OS: $(uname -s)" ;;
esac

case "$(uname -m)" in
	x86_64|amd64) arch=amd64 ;;
	arm64|aarch64) arch=arm64 ;;
	*) fail "unsupported arch: $(uname -m)" ;;
esac

if [ "$version" = latest ]; then
	base="$repo/releases/latest/download"
else
	base="$repo/releases/download/$version"
fi

if [ -n "${JUSTRAY_INSTALL_DIR:-}" ]; then
	dir="$JUSTRAY_INSTALL_DIR"
else
	dir="$HOME/.local/bin"
	for c in "$HOME/.local/bin" "$HOME/bin" /usr/local/bin; do
		case ":$PATH:" in
			*":$c:"*)
				dir="$c"
				break
				;;
		esac
	done
fi

mkdir -p "$dir" 2>/dev/null || fail "cannot create directory $dir"
[ -w "$dir" ] || fail "cannot write to $dir"
dir=$(cd "$dir" && pwd -P)

tmp=$(mktemp -d)
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
curl -fsSL --retry 3 "$base/checksums.txt" -o "$checksums" 2>/dev/null || fail "failed to fetch release metadata"

line=$(
	awk -v s="_${os}_${arch}.tar.gz" '
		$2 ~ ("justray_.*" s "$") { print $1, $2 }
	' "$checksums"
)

# shellcheck disable=SC2086
set -- $line
[ "$#" -eq 2 ] || fail "expected exactly one release for ${os}_${arch}"

expected="$1"
archive="$2"

tag=$(echo "$archive" | sed -E 's/^justray_(.+)_[^_]+_[^_]+\.tar\.gz$/\1/')
pass "Found v$tag for ${os}/${arch}"

step "Downloading $archive..."

curl -fsSL --retry 3 "$base/$archive" -o "$tmp/$archive" 2>/dev/null || fail "failed to download $archive"

if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "$tmp/$archive" 2>/dev/null | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
	actual=$(shasum -a 256 "$tmp/$archive" 2>/dev/null | awk '{print $1}')
else
	fail "sha256sum or shasum is required"
fi

[ "$actual" = "$expected" ] || fail "checksum mismatch"

pass "Verified checksum"

mkdir -p "$tmp/out"
tar -xzf "$tmp/$archive" -C "$tmp/out" 2>/dev/null || fail "failed to extract archive"

[ -f "$tmp/out/justray" ] || fail "archive is missing justray"
[ -f "$tmp/out/justrayd" ] || fail "archive is missing justrayd"

step "Installing..."

target_pids=""
if command -v pgrep >/dev/null 2>&1; then
	for p in $(pgrep -u "$(id -u)" -x justrayd 2>/dev/null || true); do
		exe=""
		if [ -e "/proc/$p/exe" ]; then
			exe=$(readlink -f "/proc/$p/exe" 2>/dev/null || true)
		elif command -v lsof >/dev/null 2>&1; then
			exe=$(lsof -p "$p" -a -d txt -Fn 2>/dev/null | sed -n 's/^n//p' || true)
		fi
		case "$exe" in
			"$dir/justrayd") target_pids="$target_pids $p" ;;
		esac
	done
fi

if [ -n "$target_pids" ]; then
	restart=1
	for p in $target_pids; do kill -TERM "$p" 2>/dev/null || true; done

	t=0
	while [ "$t" -lt 70 ]; do
		alive=0
		for p in $target_pids; do
			kill -0 "$p" 2>/dev/null && alive=1 && break
		done
		[ "$alive" -eq 0 ] && break
		sleep 0.1 2>/dev/null || sleep 1
		t=$((t + 1))
	done

	for p in $target_pids; do
		kill -KILL "$p" 2>/dev/null || true
	done
fi

install -m 755 "$tmp/out/justrayd" "$dir/justrayd" || fail "failed to install justrayd"
install -m 755 "$tmp/out/justray" "$dir/justray" || fail "failed to install justray"
ln -sf justray "$dir/jray" || fail "failed to link jray"

pass "Installed to $dir"

case ":$PATH:" in
	*":$dir:"*)
		printf "\nTo get started, run jray in a new terminal window\n"
		;;
	*)
		path_display="$dir"
		case "$dir" in
			"$HOME"/*) path_display="\$HOME/${dir#"$HOME"/}" ;;
		esac
		printf "\nTo get started, add %s to your PATH:\n  export PATH=\"%s:\$PATH\"\n\nThen run jray\n" "$dir" "$path_display"
		;;
esac
