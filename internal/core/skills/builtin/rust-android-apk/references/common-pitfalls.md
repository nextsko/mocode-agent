# Common Pitfalls (踩坑 catalog)

Each entry: **症状 (symptom) → 根因 (root cause) → 解决 (fix).** Indexed by keyword at the bottom.

---

## JDK version too new

**症状:** `tauri android init` fails opaquely, or Gradle fails with a Java version error.

**根因:** Tauri v2 bundles Gradle 8.14.3, which doesn't support JDK 25/26. Newer JDKs break the bundled Gradle.

**解决:** Install JDK 21 (LTS — the recommended version) and point `JAVA_HOME` at it. Verify `java -version` shows 21.x. This is the #1 cause of init/build failures.

---

## NDK_HOME not set (even though NDK is installed)

**症状:** `tauri android init` → "NDK_HOME environment variable isn't set", despite the NDK being installed.

**根因:** Tauri CLI ≥2.9 can auto-detect from default locations, but if your install is non-default or the env var isn't exported in the *exact shell* running Tauri, detection fails.

**解决:** `export NDK_HOME="$ANDROID_HOME/ndk/<version>"` in a `scripts/env.sh` you `source` in the same command as `tauri android init`. Don't rely on auto-detection. Source it in one combined command: `source scripts/env.sh && cd <proj> && npx tauri android init`.

---

## `cmdline-tools` not under `latest/`

**症状:** `sdkmanager` errors: "Could not determine SDK root" or "move this package into its expected location `<sdk>\cmdline-tools\latest\`".

**根因:** sdkmanager expects to live at `<sdk>/cmdline-tools/latest/bin/`. Many installs put it at `<sdk>/cmdline-tools/bin/` (no `latest/`).

**解决:** Two options. (a) Pass `--sdk_root=<sdk>` to every sdkmanager/avdmanager call. (b) Move/rename: create `<sdk>/cmdline-tools/latest/` and move the existing contents into it. Option (a) is non-invasive and works immediately.

---

## `tauri android build` symlink failure (Windows)

**症状:** `Failed to create a symbolic link ... SeCreateSymbolicLinkPrivilege`.

**根因:** Windows requires Developer Mode or admin to create symlinks. Tauri's rust Gradle plugin symlinks the `.so` into jniLibs unconditionally.

**解决:** See windows-symlink-workaround.md — either enable Developer Mode (clean, one-time, needs admin) or use the scripted bypass (copy the `.so`, run gradle with `-x rustBuild*`). The `.so` is already built by the time the symlink fails.

---

## Gradle distribution download times out

**症状:** `Downloading https://services.gradle.org/distributions/gradle-8.14.3-bin.zip failed: timeout`. (`services.gradle.org` 307-redirects to `github.com`.)

**根因:** `services.gradle.org` redirects to GitHub releases, which may be blocked or slow on some networks (common in CN).

**解决:** Point the wrapper at a mirror. Edit `src-tauri/gen/android/gradle/wrapper/gradle-wrapper.properties`:
```
distributionUrl=https\://mirrors.cloud.tencent.com/gradle/gradle-8.14.3-bin.zip
```
(Keep the version matching what Tauri bundles.) Clear any partial download under `~/.gradle/wrapper/dists/gradle-<version>-bin/` and re-run. Other mirrors: Aliyun, Tsinghua TUNA.

---

## AVD won't boot: "Broken AVD system path"

**症状:** Emulator FATAL on boot, path shows doubled `Sdk\Sdk\system-images\...`.

**根因:** `avdmanager` writes `image.sysdir.1=Sdk\system-images\...` (spurious leading `Sdk\`).

**解决:** Edit `~/.android/avd/<name>.avd/config.ini`, change to `image.sysdir.1=system-images\android-34\google_apis\x86_64\`. See emulator-headless-setup.md#image-sysdir-bug.

---

## adb "more than one device/emulator"

**症状:** `adb install ...` → `error: more than one device/emulator`.

**根因:** A physical device is attached alongside the emulator.

**解决:** Always pass `-s <serial>`: `adb -s emulator-5554 ...`. Run `adb devices` to list serials. Hard-code `-s emulator-5554` in deploy scripts.

---

## APK size nondeterministic across rebuilds

**症状:** Same source, same command, but the APK is 124 MB on one run and 241 MB on the next.

**根因:** AGP's incremental packager keeps a previously-stored `.so` zip entry; on some runs it stores uncompressed (`stored`) instead of deflating.

**解决:** In the build script, clear APK intermediates before each assemble:
- `app/build/outputs/apk/<abi>/`
- `app/build/intermediates/stripped_native_libs/`
- `app/build/intermediates/packaged_res_for_<debug|release>/`
After this, three consecutive runs produce byte-identical APKs.

---

## `gradlew clean` deletes the release `.so`

**症状:** After `./gradlew clean`, the release build can't find `lib<crate>_lib.so`.

**根因:** The Tauri rust Gradle plugin registers a clean hook that deletes `target/<abi>/<profile>/lib<crate>_lib.so`.

**解决:** Don't run `gradlew clean` casually. If you do, re-run the full cross-compile (`npx tauri android build -t <abi>`) to regenerate the `.so` before assembling. The build script's per-run intermediate-clear (above) is scoped and avoids this.

---

## Concurrent builds corrupt each other

**症状:** cargo "couldn't create a temp dir", or AAPT2 "file not found", when building two ABIs "in parallel".

**根因:** Two gradle/cargo processes sharing `target/` and `app/build/` race on file creation.

**解决:** Serialize per-ABI builds. Finish one ABI's full build before starting the next. `./gradlew --stop` between builds if you suspect a hung daemon.

---

## `rm -rf app/build` fails "Directory not empty"

**症状:** `rm -rf src-tauri/gen/android/app/build` fails mid-tree.

**根因:** A live Gradle daemon holds file locks on intermediates.

**解决:** `./gradlew --stop` first, then `rm -rf`. Or scope your deletion to specific intermediates rather than the whole tree.

---

## Git Bash mangles `/sdcard/...` paths

**症状:** `adb shell uiautomator dump /sdcard/ui.xml` writes to a weird local path like `C:/Program Files/.../Git/sdcard/...` instead of the device.

**根因:** MSYS path conversion rewrites Unix-style paths to Windows paths.

**解决:** `export MSYS_NO_PATHCONV=1` before the adb calls. Then `/sdcard/ui.xml` is passed literally to the device. Pull via `adb exec-out` or `adb shell cat` rather than `adb pull` with a local Unix path.

---

## Identifier change after `tauri android init`

**症状:** Changing `identifier` in `tauri.conf.json` then re-initing errors: "Did you update the package name? ... delete the gen/android folder and run tauri android init to recreate."

**根因:** Tauri refuses to mutate an existing Android project's package name.

**解决:** Set `identifier` *before* the first `tauri android init`. If you need to change it after, delete `src-tauri/gen/android/` and re-init.

---

## `jarsigner -verify` says "unsigned" for a signed APK

**症状:** `jarsigner -verify release.apk` → "jar unsigned", but the APK installs fine.

**根因:** AGP signs release APKs with the v2 APK Signature Scheme only — no JAR/v1 manifest. jarsigner only checks v1.

**解决:** Use `apksigner verify --print-certs <apk>` instead. Expect `Verifies` + `Verified using v2 scheme: true`. Not a defect; don't "fix" by forcing v1 signing.

---

## aarch64 APK won't run on x86_64 emulator

**症状:** arm64 APK installs but crashes immediately on the x86 emulator.

**根因:** No native-lib translation layer — the arm64 `.so` can't execute on an x86_64 image.

**解决:** This is expected. Build it, verify packaging/signing, but prove *runtime* with the x86_64 build on the emulator. Real-device verification needs a real arm64 device (or an arm64 emulator image, slow).

---

## Keyword index

- **build fails / init fails** → JDK version, NDK_HOME, cmdline-tools layout
- **symlink** → Windows symlink workaround
- **download timeout / network** → Gradle mirror
- **emulator boot** → image.sysdir bug, acceleration
- **adb error** → `-s emulator-5554`, MSYS_NO_PATHCONV
- **APK wrong size** → nondeterministic APK size
- **clean broke it** → gradlew clean deletes .so
- **parallel corruption** → serialize builds
- **signature** → apksigner not jarsigner; identifier change
- **arch mismatch** → ABI mapping, aarch64-on-x86
