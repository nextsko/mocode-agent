#!/usr/bin/env bash
# publish.sh — copy goreleaser dist binaries into platform packages and publish all to npm
# Usage: NPM_TOKEN=xxx bash npm/publish.sh <version>
# Expects goreleaser dist/ to already exist at repo root.

set -euo pipefail

VERSION="${1:-}"
if [[ -z "$VERSION" ]]; then
  echo "Usage: $0 <version>  (e.g. 0.7.0)"
  exit 1
fi

DIST="$(git rev-parse --show-toplevel)/dist"
NPM_DIR="$(git rev-parse --show-toplevel)/npm"

if [[ -n "${NPM_TOKEN:-}" ]]; then
  cat > ~/.npmrc <<- NPMRC
	registry=https://registry.npmjs.org/
	//registry.npmjs.org/:_authToken=${NPM_TOKEN}
	@fromsko:registry=https://registry.npmjs.org/
	always-auth=true
	NPMRC
fi
echo "==> npm whoami: $(npm whoami 2>&1 || echo 'not authenticated')"

# Map: subdir → goreleaser archive content binary name
declare -A BINS=(
  ["mocode-cli-linux-x64"]="mocode_${VERSION}_linux_amd64/mocode"
  ["mocode-cli-linux-arm64"]="mocode_${VERSION}_linux_arm64/mocode"
  ["mocode-cli-darwin-x64"]="mocode_${VERSION}_darwin_amd64/mocode"
  ["mocode-cli-darwin-arm64"]="mocode_${VERSION}_darwin_arm64/mocode"
  ["mocode-cli-win32-x64"]="mocode_${VERSION}_windows_amd64/mocode.exe"
)

TMPDIR_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_ROOT"' EXIT

echo "==> Extracting binaries from $DIST"
for pkg in "${!BINS[@]}"; do
  bin_path="${BINS[$pkg]}"
  archive_base="${bin_path%%/*}"
  bin_name="${bin_path##*/}"

  # Find the archive (tar.gz or zip)
  archive=$(find "$DIST" -name "${archive_base}.*" | head -1)
  if [[ -z "$archive" ]]; then
    echo "ERROR: archive for $pkg not found in $DIST (looked for ${archive_base}.*)"
    exit 1
  fi

  tmpdir="$TMPDIR_ROOT/$archive_base"
  mkdir -p "$tmpdir"

  if [[ "$archive" == *.zip ]]; then
    unzip -q "$archive" -d "$tmpdir"
  else
    tar -xzf "$archive" -C "$tmpdir"
  fi

  dest="$NPM_DIR/$pkg/$bin_name"
  cp "$tmpdir/$bin_name" "$dest"
  chmod +x "$dest"
  echo "  copied $bin_name → $dest"
done

echo "==> Bumping versions to $VERSION"
for dir in "$NPM_DIR"/mocode-cli "$NPM_DIR"/mocode-cli-*/; do
  node -e "
    const fs = require('fs'), p = '$dir/package.json';
    const pkg = JSON.parse(fs.readFileSync(p,'utf8'));
    pkg.version = '$VERSION';
    if (pkg.optionalDependencies) {
      for (const k of Object.keys(pkg.optionalDependencies)) pkg.optionalDependencies[k] = '$VERSION';
    }
    fs.writeFileSync(p, JSON.stringify(pkg, null, 2) + '\n');
  "
done

echo "==> Publishing platform packages first"
for pkg in mocode-cli-linux-x64 mocode-cli-linux-arm64 mocode-cli-darwin-x64 mocode-cli-darwin-arm64 mocode-cli-win32-x64; do
  echo "  npm publish $pkg"
  npm publish "$NPM_DIR/$pkg" --access public
done

echo "==> Publishing main package @fromsko/mocode-cli"
npm publish "$NPM_DIR/mocode-cli" --access public

echo "==> Done! @fromsko/mocode-cli@${VERSION} published."
