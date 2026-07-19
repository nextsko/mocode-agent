# Signing & Release Builds

Debug APKs are auto-signed with a debug key (fine for the emulator). To distribute or install on a real device without "INSTALL_PARSE_FAILED_NO_CERTIFICATES", you need a release keystore.

## Generate the keystore (one-time)

```bash
keytool -genkeypair -v \
  -keystore .keystore/release.jks \
  -keyalg RSA -keysize 2048 \
  -validity 10000 \
  -alias release \
  -dname "CN=Your Name, OU=Dev, O=Your Org, L=City, ST=State, C=Country" \
  -storepass <storepass> \
  -keypass <keypass>
```

- `-validity 10000` (~27 years) — Google Play requires ≥ 25 years.
- RSA 2048 is the floor; fine for most uses.
- **Keep `<storepass>`, `<keypass>`, and the `.jks` file safe.** If you lose the keystore, you can never update your app on Google Play under the same package id. Back it up somewhere durable (not just the dev machine).
- `keytool` ships with the JDK — it's at `$JAVA_HOME/bin/keytool`.

## `.gitignore` the secrets — always

Append to your project `.gitignore`:

```
.keystore/
**/.keystore/
src-tauri/gen/android/key.properties
src-tauri/gen/android/app/key.properties
```

The keystore is your app's identity. Committing it lets anyone sign malicious updates that Android will accept as yours. Never commit it. If you've already committed it, rotate: generate a new keystore and (if shipping via Play) use Play's key upgrade / key rotation flow.

## `key.properties` and the signingConfig

Create `src-tauri/gen/android/app/key.properties` (note: next to `app/build.gradle.kts`, because Gradle's `file(...)` resolves relative to the module dir — this matches where the existing `tauri.properties` lives):

```properties
storeFile=../../../../.keystore/release.jks
storePassword=<storepass>
keyAlias=release
keyPassword=<keypass>
```

The `storeFile` path is relative to `app/` — count the `../` levels to reach the project root where `.keystore/` lives.

Wire it into `src-tauri/gen/android/app/build.gradle.kts` (Kotlin DSL). Match the existing `tauriProperties` pattern if present:

```kotlin
import java.util.Properties

// near the existing tauriProperties val:
val keyProperties = Properties().apply {
    val propFile = file("key.properties")
    if (propFile.exists()) {
        propFile.inputStream().use { load(it) }
    }
}

android {
    // ...
    signingConfigs {
        create("release") {
            // Guard so debug/CI builds without a keystore still configure cleanly
            if (file("key.properties").exists()) {
                storeFile = file(keyProperties.getProperty("storeFile"))
                storePassword = keyProperties.getProperty("storePassword")
                keyAlias = keyProperties.getProperty("keyAlias")
                keyPassword = keyProperties.getProperty("keyPassword")
            }
        }
    }
    buildTypes {
        getByName("release") {
            isMinifyEnabled = true
            signingConfig = signingConfigs.getByName("release")
            proguardFiles(/* existing */)
        }
    }
}
```

The `if (file("key.properties").exists())` guard matters: without it, a fresh clone (which doesn't have `key.properties`) fails to configure the build, breaking CI and new-contributor onboarding.

Verify it parses: `./gradlew :app:tasks` should print `BUILD SUCCESSFUL` and list `assembleRelease`.

## Multi-ABI release builds

For a real-phone-distributable APK, build aarch64 (and optionally x86_64 for Chromebooks/emulators). Per ABI, run the symlink workaround (see windows-symlink-workaround.md) with `-t <abi>` and the matching Gradle task:

```bash
# aarch64 (most real phones)
npx tauri android build -t aarch64 --apk || true
cp src-tauri/target/aarch64-linux-android/release/lib<crate>_lib.so \
   src-tauri/gen/android/app/src/main/jniLibs/arm64-v8a/
cd src-tauri/gen/android
./gradlew :app:assembleArm64Release -x rustBuildArm64Release -x rustBuildUniversalRelease
```

**Serialize** — don't build two ABIs concurrently (shared `target/` corruption). Build one fully, then the next.

Expected sizes (stripped release): ~10–11 MB per ABI for a small Tauri app (vs ~120 MB for an unstripped debug build). If your release APK is ~120 MB, stripping isn't happening — check that `isMinifyEnabled = true` and the strip debug symbols task ran.

## Verify the signature (use apksigner, not jarsigner)

AGP signs release APKs with the **v2 APK Signature Scheme** only — there's no JAR/v1 manifest. `jarsigner -verify` therefore reports "jar unsigned" *even for a correctly-signed release APK*. That's expected; it's not a defect. Verify with `apksigner` instead:

```bash
APKSIGNER="$ANDROID_HOME/build-tools/<version>/apksigner"   # .bat on Windows
"$APKSIGNER" verify --print-certs <path-to-apk>
```

Expected output:
```
Verifies
Verified using v1 scheme (JAR signing): false
Verified using v2 scheme (APK Signature Scheme v2): true
Number of signers: 1
Signer #1 certificate DN: CN=..., ...
Signer #1 key algorithm: RSA   key size: 2048
```

`Verifies` + v2 = true = good. The DN should match what you put in `keytool -dname`.

## Verify the native lib is packaged, in the right ABI

```bash
unzip -l <apk> | grep lib/
```

Expect:
- x86_64 APK: `lib/x86_64/lib<crate>_lib.so`
- arm64 APK: `lib/arm64-v8a/lib<crate>_lib.so`

If the `lib/<abi>/` line is missing, the `.so` didn't make it into jniLibs (or is in the wrong subdir) — re-check the copy step and the ABI mapping table.

## Installing a release APK over a debug build

A release-signed APK and a debug-signed APK have different signatures, so Android refuses to install one over the other: `INSTALL_FAILED_UPDATE_INCOMPATIBLE` / signature mismatch. Uninstall the old one first:

```bash
adb -s emulator-5554 uninstall <package.id>
adb -s emulator-5554 install <release.apk>
```

## Going further: App Bundle (.aab) for Play Store

For Google Play upload you want an `.aab` (App Bundle), not a per-ABI APK — Play uses the bundle to generate per-device APKs. Tauri's Gradle project can build it: `./gradlew :app:bundleRelease` produces `app/build/outputs/bundle/release/app-release.aab`. Sign the bundle with the same keystore (the release signingConfig covers it). Then upload via the Play Console. This skill stops at the APK stage; the Play upload is a separate workflow.
