#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 5 ]]; then
  echo "usage: $0 <version> <release-repo> <darwin-amd64-sha> <darwin-arm64-sha> <output-path>" >&2
  exit 2
fi

version="$1"
release_repo="$2"
darwin_amd64_sha="$3"
darwin_arm64_sha="$4"
output_path="$5"

mkdir -p "$(dirname "$output_path")"
cat >"$output_path" <<EOF
class Udl < Formula
  desc "Set up and sync local music libraries from SoundCloud and Spotify sources"
  homepage "https://github.com/${release_repo}"
  version "${version}"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/${release_repo}/releases/download/v#{version}/udl-v#{version}-darwin-arm64.tar.gz"
      sha256 "${darwin_arm64_sha}"
    else
      url "https://github.com/${release_repo}/releases/download/v#{version}/udl-v#{version}-darwin-amd64.tar.gz"
      sha256 "${darwin_amd64_sha}"
    end
  end

  def install
    libexec.install Dir["*"]
    chmod "+x", libexec/"udl"
    chmod "+x", libexec/"tools/scdl"
    chmod "+x", libexec/"tools/yt-dlp"
    bin.write_env_script libexec/"udl", PATH: "#{libexec}/tools:\$PATH"
  end

  test do
    assert_match "udl version", shell_output("#{bin}/udl version")
    output = shell_output("#{bin}/udl doctor")
    assert_match "no sources configured yet", output
  end
end
EOF
