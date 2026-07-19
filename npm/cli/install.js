#!/usr/bin/env node
// install.js — downloads the mocode binary for the current platform at postinstall time

"use strict";

const https = require("https");
const fs = require("fs");
const path = require("path");
const os = require("os");
const { execFileSync } = require("child_process");

const VERSION = require("./package.json").version;
const REPO = "nextsko/mocode-agent";
const BASE_URL = `https://github.com/${REPO}/releases/download/v${VERSION}`;

// platform → { file, ext }
const PLATFORM_MAP = {
  "linux-x64":   { archive: `mocode_${VERSION}_linux_amd64.tar.gz`,   ext: ""    },
  "linux-arm64": { archive: `mocode_${VERSION}_linux_arm64.tar.gz`,   ext: ""    },
  "darwin-x64":  { archive: `mocode_${VERSION}_darwin_amd64.tar.gz`,  ext: ""    },
  "darwin-arm64":{ archive: `mocode_${VERSION}_darwin_arm64.tar.gz`,  ext: ""    },
  "win32-x64":   { archive: `mocode_${VERSION}_windows_amd64.zip`,    ext: ".exe" },
};

const key = `${process.platform}-${os.arch()}`;
const info = PLATFORM_MAP[key];
if (!info) {
  console.error(`[mocode] Unsupported platform: ${key}. Download manually from https://github.com/${REPO}/releases`);
  process.exit(0); // non-fatal: allow install to complete
}

const destDir  = path.join(__dirname, "bin");
const destBin  = path.join(destDir, `mocode${info.ext}`);
const tmpFile  = path.join(os.tmpdir(), info.archive);

fs.mkdirSync(destDir, { recursive: true });

// If binary already exists (e.g. re-install), skip download
if (fs.existsSync(destBin)) {
  console.log(`[mocode] Binary already present at ${destBin}, skipping download.`);
  process.exit(0);
}

const url = `${BASE_URL}/${info.archive}`;
console.log(`[mocode] Downloading ${url}`);

function download(url, dest, cb) {
  const file = fs.createWriteStream(dest);
  const req = (url.startsWith("https") ? https : require("http")).get(url, (res) => {
    if (res.statusCode === 301 || res.statusCode === 302) {
      file.close();
      fs.unlinkSync(dest);
      return download(res.headers.location, dest, cb);
    }
    if (res.statusCode !== 200) {
      file.close();
      fs.unlinkSync(dest);
      return cb(new Error(`HTTP ${res.statusCode} for ${url}`));
    }
    res.pipe(file);
    file.on("finish", () => file.close(cb));
  });
  req.on("error", (err) => {
    fs.unlinkSync(dest);
    cb(err);
  });
}

download(url, tmpFile, (err) => {
  if (err) {
    console.error(`[mocode] Download failed: ${err.message}`);
    process.exit(0); // non-fatal
  }

  try {
    if (info.archive.endsWith(".zip")) {
      // Use built-in PowerShell on Windows or unzip on other platforms
      if (process.platform === "win32") {
        execFileSync("powershell", [
          "-NoProfile", "-Command",
          `Expand-Archive -Path '${tmpFile}' -DestinationPath '${destDir}' -Force`,
        ]);
      } else {
        execFileSync("unzip", ["-o", tmpFile, "mocode", "-d", destDir]);
      }
    } else {
      execFileSync("tar", ["-xzf", tmpFile, "-C", destDir, `mocode${info.ext}`]);
    }
    fs.unlinkSync(tmpFile);

    // Ensure executable bit on POSIX
    if (process.platform !== "win32") {
      fs.chmodSync(destBin, 0o755);
    }
    console.log(`[mocode] Installed to ${destBin}`);
  } catch (e) {
    console.error(`[mocode] Extraction failed: ${e.message}`);
    process.exit(0);
  }
});
