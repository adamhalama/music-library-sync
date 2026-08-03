# macOS application release

CI and release builds select Xcode 16.4 (16F6) explicitly from the GitHub
`macos-15` runner. Do not rely on the runner's changing default Xcode path.

The native `UDL.app` complements the existing Homebrew/CLI artifacts. The
first app distribution format is a checksummed ZIP; existing architecture
tarballs and the Homebrew formula remain supported and are not replaced.

The app is distributed with a free ad-hoc signature. It is not signed with an
Apple Developer ID and is not notarized. The signature seals the nested code
and lets the build verify its own bundle structure, but it does not identify
the author to macOS. A recipient must explicitly approve the app on first
launch as described in `docs/install-macos-app.md`.

## Local development

### Command Line Tools only

Full Xcode is not required for the normal local app loop. The checked-in
fallback reproduces the direct `swiftc` bundle assembly used during
implementation:

```sh
make app-dev
open dist/dev/UDL-Dev.app
```

`make app-dev` first rebuilds `bin/udl`, compiles the current-architecture
Swift frontend with strict Swift 6 concurrency, creates a concrete development
plist with display name `UDL Dev` and bundle ID `com.jaa.udl.dev`, embeds the
backend, ad-hoc signs inside-out, verifies the result, and writes
`dist/dev/UDL-Dev.app`.

Use `make app-dev-open` as the combined build-and-open command.

To build, install, register with Spotlight, and open the development app:

```sh
make app-dev-install
```

The command displays build progress, can be canceled with Control-C, preserves
the previously installed app if an update fails, and is safe to run repeatedly.
Pass `--no-open` directly to `packaging/dev/install_macos_app.sh` when the app
should not launch afterward. Set `UDL_APP_INSTALL_DIR` to override the default
`~/Applications` destination.

Re-run `make app-dev-install` after Go or Swift changes.
Because a Finder/Spotlight launch does not receive Xcode scheme environment
variables, the installed copy always uses its embedded backend.

### Full Xcode

When full Xcode is installed:

1. Run `make build` to create `bin/udl`.
2. Open `macos/UDL.xcodeproj` and use the shared `UDL` scheme.
3. The scheme uses `bin/udl` through `UDL_DEVELOPMENT_BINARY`; the build phase
   also embeds it at `Contents/Resources/udl`.

The app is deliberately not sandboxed: it launches the embedded backend and
configured download tools, reads user-selected music directories, and performs
explicit Music automation. Hardened runtime remains enabled. The only
entitlement is Apple Events automation; Downloads access does not require a
sandbox entitlement in this unsandboxed design.

## Free distribution build

Build the universal backend with:

```sh
VERSION=1.2.3 COMMIT="$(git rev-parse HEAD)" \
  OUTPUT=dist/udl-universal \
  bash packaging/release/build_universal_backend.sh
```

Build the universal Xcode app without account-based signing:

```sh
xcodebuild \
  -project macos/UDL.xcodeproj \
  -scheme UDL \
  -configuration Release \
  -destination 'generic/platform=macOS' \
  -derivedDataPath dist/AppDerivedData \
  ARCHS='arm64 x86_64' \
  ONLY_ACTIVE_ARCH=NO \
  CODE_SIGNING_ALLOWED=NO \
  MARKETING_VERSION=1.2.3 \
  UDL_EMBEDDED_BINARY="$PWD/dist/udl-universal" \
  build
```

Then package it:

```sh
VERSION=1.2.3 \
UDL_BIN=dist/udl-universal \
APP_BUNDLE_PATH=dist/AppDerivedData/Build/Products/Release/UDL.app \
  bash packaging/release/build_macos_bundle.sh
```

Signing remains inside-out: the script ad-hoc signs the embedded `udl` binary
first with the pseudo-identity `-`, signs the outer app last with hardened
runtime and the checked-in entitlements, verifies the nested signatures,
smokes the embedded backend, creates the ZIP, and emits a SHA-256 sidecar. No
Apple account, certificate, private key, or notarization credential is used.

GitHub Actions performs the same process and publishes the universal ZIP
beside the existing CLI/Homebrew artifacts. The repository must never describe
this artifact as Apple-verified, identified, or notarized.

## Rollback

GitHub releases are immutable in intent. If an app artifact is bad, mark that
release as affected, remove the app ZIP from distribution, keep the CLI
artifacts available, fix forward in a new patch tag, and rotate any credential
that may have been exposed. Do not silently replace an artifact under the same
version.
