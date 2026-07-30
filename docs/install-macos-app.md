# Install the macOS app

`UDL.app` is distributed as a universal `arm64`/`x86_64` ZIP with a free
ad-hoc code signature. It is not signed by an Apple-identified developer and
is not notarized. macOS will therefore block the first ordinary launch and
show an unidentified-developer warning.

1. Download `UDL-vX.Y.Z-macOS-universal.zip` and `SHA256SUMS` from the same
   GitHub release.
2. Compare the ZIP's `shasum -a 256` value with its entry in `SHA256SUMS`.
3. Unzip it and move `UDL-vX.Y.Z.app` to `/Applications` or
   `~/Applications`.
4. Try to open it once.
5. Open **System Settings → Privacy & Security**, scroll to **Security**, and
   choose **Open Anyway** for UDL. Confirm only if the download and checksum
   are the ones you expected.

Apple documents this exception flow at:
<https://support.apple.com/guide/mac-help/open-a-mac-app-from-an-unknown-developer-mh40616/mac>

After that one-time approval, the app can be opened normally and found through
Spotlight. Installing the app does not install the `udl` shell command; the
Homebrew/CLI package remains separate. The app still requires the external
download tools reported by Doctor.
