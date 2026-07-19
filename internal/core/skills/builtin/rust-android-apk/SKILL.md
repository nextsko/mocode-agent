---
name: rust-android-apk
description: Build a Rust application into an Android APK and run it on an emulator or device. Use whenever the user wants Rust code to run on Android — packaging Rust as an APK, cross-compiling Rust for Android, Tauri v2 mobile/Android setup, `tauri android init` or `tauri android build` troubleshooting, Android emulator/AVD setup without Android Studio, NDK/Gradle errors during Rust-Android builds, or signing Rust-built Android APKs. Covers the full path: toolchain detection → emulator provisioning → project init → Rust core + frontend → cross-compile → packaging → signing → deploy → verify. Distilled from a verified end-to-end Tauri v2 build on Windows.
---

# Rust → Android APK (via Tauri v2)

## Decision: which approach

| Approach | Use when | Notes |
|---|---|---|
| **Tauri v2** (default) | You want a UI (WebView) + Rust backend, or a general-purpose app. | Officially supported, one-command packaging, Rust is first-class. **Recommended unless you have a specific reason not to.** |
| `cargo-apk` + `android-activity` | You want a *pure native* app (no WebView, you draw everything yourself via `winit`/OpenGL/Vulkan). | Raw, more glue, you bring your own rendering. Good for games or fully-custom UI. |
| Slint mobile | Your UI is Slint. | You still handle packaging; Slint gives you the UI layer only. |
| `cargo-mobile2` | You want the lower-level mobile tooling Tauri wraps. | Tauri already uses its best parts. Rarely the right top-level choice. |

**Default to Tauri v2.** The rest of this skill is Tauri v2-centric; the environment-detection and emulator sections apply to all approaches.

## The critical environment constraints (get these wrong and you lose hours)

These are non-obvious and version-sensitive. Verify them in Phase 0 before touching anything.

1. **JDK must be ≤ 21.** Tauri v2 bundles Gradle 8.14.3, which is incompatible with JDK 25/26. JDK 21 (LTS) is the recommended version. If `java -version` shows 25+, install JDK 21 and point `JAVA_HOME` at it. This is the #1 cause of opaque `tauri android init` / build failures.
2. **Both `ANDROID_HOME` and `NDK_HOME` must be set**, in the *same shell* that runs Tauri. Tauri CLI ≥2.9 can auto-detect from default locations, but don't rely on it — set them explicitly. A missing `NDK_HOME` produces "NDK_HOME environment variable isn't set" even when the NDK is installed.
3. **Rust Android targets must be installed:** `rustup target add aarch64-linux-android armv7-linux-androideabi i686-linux-android x86_64-linux-android`. At minimum the ABI you target (x86_64 for emulator, aarch64 for real phones).
4. **NDK side-by-side layout.** Tauri/Gradle want the NDK under `<sdk>/ndk/<version>/`. Check `NDK_HOME` actually points there. If you have only one NDK installed, `NDK_HOME` usually doesn't need a version pin in `build.gradle.kts`; if you have multiple, pin `ndkVersion` to match.
5. **Windows symlink privilege.** `tauri android build` symlinks the compiled `.so` into `jniLibs/`. On Windows this needs either Developer Mode enabled *or* admin. Without either, the build fails at the symlink step *after* successfully cross-compiling the `.so`. See references/windows-symlink-workaround.md for the proven bypass.

## Phase 0 — detect the environment (do this first, yourself)

Run these and record exact paths/versions. Every later step references these locked values.

```bash
java -version          # MUST be ≤ 21
echo "$JAVA_HOME"      # if empty or wrong, set it
echo "$ANDROID_HOME"   # SDK root
echo "$NDK_HOME"       # must point to <sdk>/ndk/<version>
ls "$ANDROID_HOME/ndk"             # which NDK versions installed
ls "$ANDROID_HOME/platforms"       # which API levels available
ls "$ANDROID_HOME/build-tools"     # build-tools versions
ls "$ANDROID_HOME/emulator" 2>/dev/null   # emulator package installed?
ls "$ANDROID_HOME/cmdline-tools"          # note: may NOT be under latest/
rustup target list --installed | grep android   # which rust targets
rustc --version; cargo --version
node --version; npm --version
```

**Common detection gotchas:**
- `cmdline-tools` may live at `<sdk>/cmdline-tools/bin/` rather than `<sdk>/cmdline-tools/latest/bin/`. If `sdkmanager` complains "Could not determine SDK root" or "move this package into its expected location `<sdk>\cmdline-tools\latest\`", invoke it with `--sdk_root=<sdk>` explicitly.
- A physical device may be attached alongside the emulator. **Always pass `-s emulator-5554` (or the specific device id) to `adb`** to disambiguate, or adb errors with "more than one device/emulator".

## Phase 1 — provision the emulator (no Android Studio required)

All CLI. See references/emulator-headless-setup.md for the full script.

```bash
# accept licenses + install the three needed packages
yes | sdkmanager --sdk_root="$ANDROID_HOME" --licenses
sdkmanager --sdk_root="$ANDROID_HOME" \
  "platform-tools" "emulator" "system-images;android-34;google_apis;x86_64"

# create the AVD (echo "no" skips the custom-hardware-profile prompt)
echo "no" | avdmanager create avd --force \
  --name tauri_api34 \
  --package "system-images;android-34;google_apis;x86_64" \
  --device pixel_6

# boot headless in the background
emulator -avd tauri_api34 -no-window -no-audio -no-snapshot \
         -no-boot-anim -gpu swiftshader_indirect -port 5554 &

# wait for full boot
adb -s emulator-5554 wait-for-device
for i in $(seq 1 90); do
  [ "$(adb -s emulator-5554 shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = "1" ] && break
  sleep 2
done
adb -s emulator-5554 shell getprop sys.boot_completed   # must print 1
```

**Why API 34 / x86_64 / google_apis:** matches the typical installed platform, and `x86_64` runs fast on an x86 host via WHPX/HAXM. Use `google_apis` (not `google_apis_playstore`) to avoid the heavier Play Store image. See references/emulator-headless-setup.md for the AVD `image.sysdir` path bug and other boot pitfalls.

## Phase 2 — scaffold the Tauri v2 project

```bash
npm create tauri-app@latest <name> -- --template vanilla-ts --manager npm --yes
cd <name> && npm install && npm install -D @tauri-apps/cli@latest

# CRITICAL: set the identifier BEFORE android init (changing it after requires
# deleting gen/android and re-initing — Tauri refuses to mutate an existing project)
# in src-tauri/tauri.conf.json: "identifier": "com.example.myapp"

npx tauri android init    # source env (JAVA_HOME/NDK_HOME) in THIS shell
```

**Identifier-first rule:** set `identifier` in `tauri.conf.json` *before* `tauri android init`. The identifier becomes the Android package name (`applicationId`/`namespace`) and the Java package directory structure. If you init with the default then change it, Tauri errors: *"Did you update the package name? Save your changes, delete the gen/android folder and run tauri android init to recreate."* Follow that advice — delete `src-tauri/gen/android/` and re-init.

## Phase 3 — implement (Rust core + frontend)

- **Rust commands** go in `src-tauri/src/lib.rs` as `#[tauri::command]` functions, registered in the `tauri::Builder::default().invoke_handler(generate_handler![...])` chain. The mobile entry point is `#[cfg_attr(mobile, tauri::mobile_entry_point)] pub fn run()`.
- **Frontend** calls them via `import { invoke } from "@tauri-apps/api/core"` (v2 path — not `@tauri-apps/api/tauri` from v1).
- Verify independently: `cargo check` in `src-tauri/` (host compile, fast) and `npm run build` (Vite). Both green before attempting an Android build.

## Phase 4 — build the APK

The happy path (on a machine *with* symlink privilege / Developer Mode):

```bash
npx tauri android build -t x86_64 --apk     # release by default; add -d for debug
```

On Windows *without* Developer Mode/admin, this fails at the symlink step. The `.so` is already cross-compiled by then — bypass the symlink and drive Gradle directly. See **references/windows-symlink-workaround.md** for the proven script; the essence:

```bash
# 1. let tauri compile the .so (tolerate the symlink failure)
npx tauri android build -t x86_64 --apk || true
# 2. copy the .so into jniLibs (NOT symlink)
cp src-tauri/target/x86_64-linux-android/<profile>/lib*.so \
   src-tauri/gen/android/app/src/main/jniLibs/x86_64/
# 3. run gradle, excluding the rust-build tasks that do the symlink
cd src-tauri/gen/android
./gradlew :app:assembleX86_64Debug -x rustBuildX86_64Debug -x rustBuildUniversalDebug
```

ABI name mapping (Rust triple → Android ABI → Gradle task fragment → jniLibs dir):
`x86_64-linux-android` → `x86_64` → `X86_64` → `x86_64/`
`aarch64-linux-android` → `arm64-v8a` → `Arm64` → `arm64-v8a/`

**Don't run two gradle/cargo builds concurrently** — they share `target/` and `build/` and corrupt each other. Serialize per-ABI builds.

APK output: `src-tauri/gen/android/app/build/outputs/apk/<abi>/<profile>/app-<abi>-<profile>.apk`.

## Phase 5 — sign release APKs

Debug builds are auto-signed with a debug key (fine for emulator). Release builds for distribution need a real keystore. See references/signing-and-release.md for full detail; the essence:

```bash
keytool -genkeypair -v -keystore .keystore/release.jks -keyalg RSA -keysize 2048 \
  -validity 10000 -alias release -dname "CN=..." -storepass *** -keypass ***
```

Then wire it into `src-tauri/gen/android/app/build.gradle.kts` via a `signingConfigs` block that reads a `key.properties` file (gitignored). AGP signs release APKs with the **v2 APK Signature Scheme** by default — verify with `apksigner verify --print-certs <apk>`, *not* `jarsigner` (jarsigner reports "unsigned" because there's no JAR/v1 manifest; that's expected, not a defect).

**Put `.keystore/` and `key.properties` in `.gitignore`.** Always. The keystore is the identity of your app; leaking it lets someone sign malicious updates.

## Phase 6 — deploy and verify on-device

```bash
adb -s emulator-5554 install -r <path-to-apk>
adb -s emulator-5554 shell monkey -p <package.id> -c android.intent.category.LAUNCHER 1
sleep 5
adb -s emulator-5554 exec-out screencap -p > launch.png
# prove the Rust bridge works — dump the UI and read the #out node text
export MSYS_NO_PATHCONV=1
adb -s emulator-5554 shell uiautomator dump /sdcard/ui.xml
adb -s emulator-5554 shell cat /sdcard/ui.xml > ui.xml
# grep ui.xml for the resource-id="out" node's text= attribute
```

**Read artifacts directly, don't trust eyeballing a screenshot.** The `uiautomator` XML dump gives machine-readable ground truth: find the WebView's output node by `resource-id` and read its `text=` attribute. This is how you *prove* the Rust command ran on-device (the text can only have been produced by the native `.so`).

**Note:** an aarch64 APK will not *execute* on an x86_64 emulator (no native-lib translation in this setup). Build it, verify it's packaged and signed, but prove runtime with the x86_64 build on the emulator. Real-device verification needs a real arm64 device.

## Troubleshooting decision tree

- `tauri android init` fails → JDK version? (must be ≤21) `NDK_HOME` set? (same shell) → references/common-pitfalls.md
- `tauri android build` fails at "symbolic link" → Windows symlink privilege → references/windows-symlink-workaround.md
- Gradle distribution download times out → network blocking `services.gradle.org`/`github.com` → references/common-pitfalls.md (see "Gradle distribution download times out")
- AVD won't boot ("Broken AVD system path") → `image.sysdir.1` path bug → references/emulator-headless-setup.md#image-sysdir-bug
- adb "more than one device" → physical device attached → use `-s emulator-5554`
- APK size differs across rebuilds → AGP stored vs deflated `.so` → clear intermediates before assemble → references/common-pitfalls.md (see "APK size nondeterministic across rebuilds")
- `gradlew clean` deletes the release `.so` → don't clean blindly; re-cross-compile after → references/common-pitfalls.md
- Git Bash mangles `/sdcard/...` paths → `export MSYS_NO_PATHCONV=1` → references/common-pitfalls.md

## References (read on demand)

- [references/emulator-headless-setup.md](references/emulator-headless-setup.md) — full emulator/AVD provisioning script, the `image.sysdir` bug, boot verification, WHPX/HAXM notes.
- [references/windows-symlink-workaround.md](references/windows-symlink-workaround.md) — the scripted bypass for the `tauri android build` symlink failure, with the reproducible-build idempotency fix.
- [references/signing-and-release.md](references/signing-and-release.md) — keystore generation, `build.gradle.kts` signingConfig, multi-ABI release builds, `apksigner` verification.
- [references/common-pitfalls.md](references/common-pitfalls.md) — the full 踩坑 catalog: 症状 → 根因 → 解决, with Gradle mirror, nondeterministic APK size, path mangling, concurrency corruption, and more.

## Integration

- **multi-agent-orchestration** — the delivery pattern this skill was distilled from; use it to drive an end-to-end Rust→Android build with named specialist subagents.
- **verification-before-completion** — the philosophy behind reading artifacts directly rather than trusting build success.
