#!/usr/bin/env node
"use strict";

// Resolve the platform-specific binary from the optional dependency.
// Pattern: @fromsko/mocode-cli-{os}-{arch}/{binary}
// Mirrors how @biomejs/biome and @anthropic-ai/claude-code work.

const { platform, arch, env } = process;
const { spawnSync } = require("child_process");
const path = require("path");

const PLATFORMS = {
  win32:  { x64: "@fromsko/mocode-cli-win32-x64/mocode.exe" },
  darwin: { x64: "@fromsko/mocode-cli-darwin-x64/mocode", arm64: "@fromsko/mocode-cli-darwin-arm64/mocode" },
  linux:  { x64: "@fromsko/mocode-cli-linux-x64/mocode",  arm64: "@fromsko/mocode-cli-linux-arm64/mocode"  },
};

// Allow manual override via env
const override = env.MOCODE_BINARY;

function resolveBin() {
  if (override) return override;
  const byArch = PLATFORMS[platform];
  if (!byArch) return null;
  const pkg = byArch[arch];
  if (!pkg) return null;
  try {
    return require.resolve(pkg);
  } catch {
    return null;
  }
}

const bin = resolveBin();

if (!bin) {
  const supported = Object.entries(PLATFORMS)
    .flatMap(([os, archs]) => Object.keys(archs).map(a => `${os}-${a}`))
    .join(", ");
  console.error(
    `[mocode] No binary found for ${platform}-${arch}.\n` +
    `Supported: ${supported}\n` +
    `Or set MOCODE_BINARY=/path/to/mocode to use a custom binary.`
  );
  process.exit(1);
}

const result = spawnSync(bin, process.argv.slice(2), {
  stdio: "inherit",
  env: process.env,
  windowsHide: false,
});

if (result.error) {
  console.error(`[mocode] Failed to run ${bin}: ${result.error.message}`);
  process.exit(1);
}

process.exit(result.status ?? 0);
