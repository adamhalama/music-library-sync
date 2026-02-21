class Udl < Formula
  desc "Sync local music downloads using configurable Spotify/SoundCloud sources"
  homepage "https://github.com/jaa/update-downloads"
  version "0.1.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/jaa/update-downloads/releases/download/v#{version}/udl-v#{version}-darwin-arm64"
      sha256 "REPLACE_WITH_SHA256_DARWIN_ARM64"
    else
      url "https://github.com/jaa/update-downloads/releases/download/v#{version}/udl-v#{version}-darwin-amd64"
      sha256 "REPLACE_WITH_SHA256_DARWIN_AMD64"
    end
  end

  on_linux do
    url "https://github.com/jaa/update-downloads/releases/download/v#{version}/udl-v#{version}-linux-amd64"
    sha256 "REPLACE_WITH_SHA256_LINUX_AMD64"
  end

  def install
    bin.install cached_download => "udl"
  end

  test do
    assert_match "udl version", shell_output("#{bin}/udl version")
  end
end
