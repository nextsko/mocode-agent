# Emulator Headless Setup (no Android Studio)

Full CLI provisioning of an Android emulator, distilled from a verified Windows setup. All commands assume `JAVA_HOME`, `ANDROID_HOME`, `NDK_HOME` are exported (put them in a `scripts/env.sh` you source every time).

## The env script

```bash
# scripts/env.sh — source in EVERY bash command
export JAVA_HOME="/c/path/to/jdk21"
export ANDROID_HOME="/c/Users/<you>/AppData/Local/Android/Sdk"
export ANDROID_SDK_ROOT="$ANDROID_HOME"
export NDK_HOME="$ANDROID_HOME/ndk/<version>"   # e.g. 29.0.13846066
export PATH="$JAVA_HOME/bin:$ANDROID_HOME/platform-tools:$ANDROID_HOME/emulator:$ANDROID_HOME/cmdline-tools/bin:$PATH"
```

If you forget to source this in the exact shell that runs `tauri android init`, you get "NDK_HOME isn't set" even though the NDK is installed. Source it; don't rely on auto-detection.

## Provisioning

```bash
source scripts/env.sh

# 1. licenses (idempotent)
yes | sdkmanager --sdk_root="$ANDROID_HOME" --licenses

# 2. install the three packages you actually need
#    (cmdline-tools may need --sdk_root if not under latest/)
sdkmanager --sdk_root="$ANDROID_HOME" \
  "platform-tools" \
  "emulator" \
  "system-images;android-34;google_apis;x86_64"

# 3. create the AVD — "echo no" answers the custom-hardware-profile prompt
echo "no" | avdmanager --verbose create avd --force \
  --name tauri_api34 \
  --package "system-images;android-34;google_apis;x86_64" \
  --device pixel_6
```

The AVD config lands at `~/.android/avd/tauri_api34.avd/config.ini` and a pointer `.ini` at `~/.android/avd/tauri_api34.ini`.

## Choice of system image

- **API level:** match an installed platform (`ls $ANDROID_HOME/platforms`). API 34 is a safe default.
- **ABI:** `x86_64` for an x86 host (fast via WHPX on Windows / HVF on Mac / KVM on Linux). `aarch64` only if you're on an ARM host (Apple Silicon) — and even then, ARM emulator images are slower.
- **Variant:** `google_apis` is lighter than `google_apis_playstore` and sufficient for app testing. Use Play Store image only if you need Play Services *and* the Play Store app itself.

## Boot headless

```bash
source scripts/env.sh
emulator -avd tauri_api34 \
  -no-window -no-audio -no-snapshot -no-boot-anim \
  -gpu swiftshader_indirect \
  -port 5554 &
```

- `-no-window` / `-no-audio`: headless, no GPU window, no sound.
- `-no-snapshot`: cold boot each time (avoids corrupted-state bugs; slower but reliable).
- `-gpu swiftshader_indirect`: software GPU rendering, works everywhere. Use `-gpu host` (or `auto`) if you have host GPU passthrough and want faster graphics.
- `-port 5554`: fixes the adb id as `emulator-5554` (port + 0). Predictable ids make scripting easier.

Background it (the emulator must keep running). On Windows under Git Bash, the Bash tool's `run_in_background: true` is the clean way.

## Verify boot completed

```bash
source scripts/env.sh
adb -s emulator-5554 wait-for-device
for i in $(seq 1 90); do
  [ "$(adb -s emulator-5554 shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = "1" ] && break
  sleep 2
done
adb -s emulator-5554 shell getprop sys.boot_completed   # must print 1
adb -s emulator-5554 get-state                          # must print device
```

`sys.boot_completed == 1` is the real "ready" signal — `adb wait-for-device` returns as soon as the daemon sees the device, which is *before* the OS has finished booting. Poll the prop.

## image.sysdir bug

**Symptom:** emulator FATAL on boot: `Broken AVD system path. <root>\Sdk\system-images\...` — the path has a doubled `Sdk\Sdk\`.

**Root cause:** `avdmanager` sometimes writes the config line as
`image.sysdir.1=Sdk\system-images\android-34\google_apis\x86_64\`
(spurious leading `Sdk\`), so the emulator resolves `<root>\Sdk\system-images\...` → `<root>\Sdk\Sdk\system-images\...` and can't find the image.

**Fix:** edit `~/.android/avd/tauri_api34.avd/config.ini`, change the line to:
```
image.sysdir.1=system-images\android-34\google_apis\x86_64\
```
(remove the leading `Sdk\`). Re-boot. Should come up in ~10s with WHPX acceleration.

## Acceleration

- **Windows:** WHPX (Windows Hypervisor Platform). Enable via "Windows features" or `Enable-WindowsOptionalFeature -Online -FeatureName Microsoft-HyperV-Hypervisor`. Without it, the emulator falls back to software emulation and is unusably slow.
- **Mac:** HVF (built in on Apple Silicon / recent Intel Macs).
- **Linux:** KVM (`/dev/kvm`).

Check: `emulator -accel-check` reports what's available.

## Multiple devices

If a physical device is also plugged in, `adb` without a target errors: `error: more than one device/emulator`. **Always pass `-s <serial>`** — `adb -s emulator-5554 ...`. `adb devices` lists all attached serials.

## Keeping it alive across a long session

The emulator is a long-running process. If your session compacts or the shell that launched it exits, the process may keep running (it's detached) or may not. On resume, check `adb devices`; if gone, re-run the boot command. A `scripts/emulator-boot.sh` wrapper makes re-launch one command.
