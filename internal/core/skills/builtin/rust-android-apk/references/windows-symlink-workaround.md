# Windows Symlink Workaround for `tauri android build`

`tauri android build` cross-compiles the Rust `.so` successfully, then tries to **symlink** it into `src-tauri/gen/android/app/src/main/jniLibs/<abi>/`. On Windows, creating symlinks needs either Developer Mode enabled *or* administrator. Without either, you get:

```
Info symlinking lib "...lib.so" in jniLibs dir "...jniLibs/<abi>"
failed to build Android app: Failed to create a symbolic link ...
Creation symbolic link is not allowed for this system.
You need `SeCreateSymbolicLinkPrivilege` security policy.
```

The good news: by this point the `.so` is already built. Bypass the symlink, drive Gradle directly.

## Two paths forward

### Path A — enable Developer Mode (the clean fix, one-time)

Windows Settings → Update & Security → For developers → Developer Mode: On. This grants symlink privilege to your user. After this, `npx tauri android build` works as documented with no workaround.

Requires admin to toggle the setting once. After that, permanent. **Preferred if you can do it.**

(Registry equivalent — needs admin: set `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\AppModelUnlock\AllowDevelopmentWithoutDevLicense` to `1`. Don't do this by hand; use the settings UI.)

### Path B — the scripted bypass (no admin needed)

This is what you use when you can't enable Developer Mode. It's proven reproducible. Capture it in a `scripts/android-build.sh` so you're not re-deriving it every time.

#### The workaround, by hand

```bash
source scripts/env.sh
cd <project>

# 1. Let tauri cross-compile the .so. It WILL fail at the symlink step.
#    That's expected. Tolerate the non-zero exit.
npx tauri android build -t x86_64 --apk || true

# 2. Verify the .so was actually produced (hard error if not — then it's a real failure).
SO=src-tauri/target/x86_64-linux-android/release/lib<crate>_lib.so
[ -f "$SO" ] || { echo "real failure: .so not built"; exit 1; }

# 3. COPY (not symlink) the .so into jniLibs.
mkdir -p src-tauri/gen/android/app/src/main/jniLibs/x86_64
cp -f "$SO" src-tauri/gen/android/app/src/main/jniLibs/x86_64/

# 4. Run Gradle directly, EXCLUDING the rust-build tasks (they're what do the symlink).
cd src-tauri/gen/android
./gradlew :app:assembleX86_64Release -x rustBuildX86_64Release -x rustBuildUniversalRelease
```

APK lands at `app/build/outputs/apk/x86_64/release/app-x86_64-release.apk`.

#### ABI mapping (memorize this)

| Rust target triple | Android ABI | Gradle task fragment | jniLibs subdir |
|---|---|---|---|
| `x86_64-linux-android` | `x86_64` | `X86_64` | `x86_64/` |
| `aarch64-linux-android` | `arm64-v8a` | `Arm64` | `arm64-v8a/` |
| `armv7-linux-androideabi` | `armeabi-v7a` | `Armv7` | `armeabi-v7a/` |
| `i686-linux-android` | `x86` | `X86` | `x86/` |

Note the mismatch: Rust `aarch64` → Android `arm64-v8a`. Getting this wrong puts the `.so` in a dir Gradle ignores.

#### The `.so` filename

`lib<crate_name_with_underscores>_lib.so`. The crate name is `Cargo.toml`'s `[package].name` with `-` → `_`. For a crate named `rust-android-demo`, the file is `librust_android_demo_lib.so`.

## The reproducible-build script (`scripts/android-build.sh`)

A robust version handles: args (`--release|--debug`, `--abi x86_64|aarch64|all`), the symlink tolerance, the `.so` existence check, the copy, and Gradle invocation. Key implementation points learned the hard way:

1. **`set -euo pipefail`** but wrap the tauri step so its non-zero exit doesn't abort: `npx tauri ... || true`, then a hard `[ -f "$SO" ] || exit 1`.
2. **Clear APK intermediates before each assemble**, or APK size is nondeterministic across runs (AGP keeps a stored copy of the `.so` and on some runs stores vs deflates — producing 124 MB vs 241 MB APKs from identical inputs). Clear at least:
   - `app/build/outputs/apk/<abi-lower>/`
   - `app/build/intermediates/stripped_native_libs/`
   - `app/build/intermediates/packaged_res_for_<debug|release>/`
3. **Serialize per-ABI builds.** Never run two gradle/cargo invocations concurrently — they share `target/` and `app/build/` and corrupt each other (cargo "couldn't create a temp dir", AAPT2 "file not found"). Build x86_64 fully, then aarch64 fully.
4. **`./gradlew --stop` before any `rm -rf app/build`.** A live Gradle daemon holds file locks on intermediates; `rm -rf` fails with "Directory not empty".

After implementing the intermediates-clear, three consecutive script runs produced byte-identical APKs (124,352,555 bytes) — that's how you know the script is reproducible, not lucky.

## Verifying the `.so` is real, not a broken symlink

After the copy, sanity-check it's an ELF binary of the right arch:

```bash
file src-tauri/gen/android/app/src/main/jniLibs/x86_64/lib<crate>_lib.so
# expect: ELF 64-bit LSB shared object, x86-64, ... for Android 24, built by NDK ...
```

If it says "symbolic link to ..." or "broken symbolic link", the copy didn't happen (you symlinked by mistake) — re-do step 3 with `cp -f`.

## Why not just `--apk`?

`npx tauri android build --apk` tells Tauri to also build the APK (not just compile Rust). It still hits the symlink step. There's no Tauri flag to "compile the .so and stop" — the symlink is unconditional in the rust Gradle plugin. Hence the bypass.
