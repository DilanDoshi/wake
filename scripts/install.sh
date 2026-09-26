#!/bin/sh
# Installs wake: the latest release for this machine, checked against the
# release's checksums.txt, into ~/.local/bin.
#
#   curl -fsSL https://raw.githubusercontent.com/DilanDoshi/wake/main/scripts/install.sh | sh
#
# WAKE_INSTALL_DIR installs somewhere else. WAKE_RELEASES reads releases from
# another host with GitHub's layout (a mirror, or a test server).
set -eu

releases="${WAKE_RELEASES:-https://github.com/DilanDoshi/wake}"
dir="${WAKE_INSTALL_DIR:-$HOME/.local/bin}"
claude_setup="https://code.claude.com/docs/en/setup"

say() { printf '%s\n' "$*"; }
fail() {
	printf 'wake install: %s\n' "$*" >&2
	exit 1
}

for tool in curl tar awk; do
	command -v "$tool" >/dev/null 2>&1 || fail "$tool is needed and is not installed"
done

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
darwin | linux) ;;
*) fail "wake has builds for macOS and Linux, not $os" ;;
esac
arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) fail "wake has builds for amd64 and arm64, not $arch" ;;
esac

# GitHub answers /releases/latest with a redirect to the newest tag.
tag=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$releases/releases/latest") ||
	fail "could not reach $releases"
tag=${tag##*/}
case "$tag" in
v*) ;;
*) fail "could not find the latest release at $releases" ;;
esac
version=${tag#v}
asset="wake_${version}_${os}_${arch}.tar.gz"

# Staged inside the install directory, so the last step is a rename on one
# filesystem: a running wake is replaced whole, never copied over.
mkdir -p "$dir"
tmp=$(mktemp -d "$dir/.wake-install.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
curl -fsSL -o "$tmp/$asset" "$releases/releases/download/$tag/$asset" ||
	fail "release $tag has no build for ${os}_${arch}"
curl -fsSL -o "$tmp/checksums.txt" "$releases/releases/download/$tag/checksums.txt" ||
	fail "could not download the checksums for $tag"

want=$(awk -v a="$asset" '$2 == a { print $1 }' "$tmp/checksums.txt")
if command -v shasum >/dev/null 2>&1; then
	got=$(shasum -a 256 "$tmp/$asset" | awk '{ print $1 }')
else
	got=$(sha256sum "$tmp/$asset" | awk '{ print $1 }')
fi
[ -n "$want" ] && [ "$got" = "$want" ] ||
	fail "$asset does not match its checksum in $tag; nothing was installed"

tar -xzf "$tmp/$asset" -C "$tmp" wake
chmod 755 "$tmp/wake"
mv -f "$tmp/wake" "$dir/wake"
say "Installed wake $version to $dir/wake."

# PATH: the directory is usually there already (Claude Code's own installer
# uses ~/.local/bin). When it is not, offer to add it - only when there is a
# terminal to answer, since this script's stdin is the pipe from curl.
case ":$PATH:" in
*":$dir:"*) on_path=yes ;;
*) on_path=no ;;
esac
if [ "$on_path" = no ]; then
	# The file each shell reads at startup: a macOS terminal starts bash as a
	# login shell, which reads ~/.bash_profile and not ~/.bashrc.
	case "${SHELL:-}" in
	*/zsh) rc="$HOME/.zshrc" ;;
	*/bash) [ "$os" = darwin ] && rc="$HOME/.bash_profile" || rc="$HOME/.bashrc" ;;
	*/fish) rc="" ;;
	*) rc="$HOME/.profile" ;;
	esac
	line="export PATH=\"$dir:\$PATH\""
	if [ -z "$rc" ]; then
		say "$dir is not on your PATH. To run wake by name, run: fish_add_path $dir"
	elif [ -f "$rc" ] && grep -qxF "$line" "$rc"; then
		say "$dir is added to your PATH in $rc; open a new terminal to pick it up."
	elif { : </dev/tty; } 2>/dev/null; then
		printf 'Add %s to your PATH in %s? [Y/n] ' "$dir" "$rc" >/dev/tty
		read -r answer </dev/tty || answer=n
		case "$answer" in
		"" | y | Y | yes | Yes)
			printf '\n%s\n' "$line" >>"$rc"
			say "Added. Open a new terminal to pick it up."
			;;
		*) say "To run wake by name, add this line to $rc: $line" ;;
		esac
	else
		say "$dir is not on your PATH. To run wake by name, add this line to $rc, then open a new terminal:"
		say "  $line"
	fi
fi

if ! command -v claude >/dev/null 2>&1; then
	say "Every Wake agent is a Claude Code session, and claude is not on your PATH yet."
	say "Install Claude Code ($claude_setup) and sign in before you run wake."
fi
say "Run wake inside a project to start your first fleet."
