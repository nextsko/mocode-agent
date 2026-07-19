#!/usr/bin/env node
// bin/mocode.js — thin shim that delegates to the platform binary

"use strict";

const path = require("path");
const { spawnSync } = require("child_process");
const os = require("os");

const ext = process.platform === "win32" ? ".exe" : "";
const bin = path.join(__dirname, `mocode${ext}`);

const result = spawnSync(bin, process.argv.slice(2), {
  stdio: "inherit",
  env: process.env,
  windowsHide: false,
});

if (result.error) {
  if (result.error.code === "ENOENT") {
    console.error(
      `[mocode] Binary not found at ${bin}.\n` +
      `Run: npm rebuild @mocode/cli   — to re-download.`
    );
  } else {
    console.error(`[mocode] Error: ${result.error.message}`);
  }
  process.exit(1);
}

process.exit(result.status ?? 0);
