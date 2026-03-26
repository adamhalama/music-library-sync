class Udl < Formula
  desc "Set up and sync local music libraries from SoundCloud and Spotify sources"
  homepage "https://github.com/jaa/update-downloads"
  version "0.1.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/jaa/update-downloads/releases/download/v#{version}/udl-v#{version}-darwin-arm64.tar.gz"
      sha256 "REPLACE_WITH_SHA256_DARWIN_ARM64"
    else
      url "https://github.com/jaa/update-downloads/releases/download/v#{version}/udl-v#{version}-darwin-amd64.tar.gz"
      sha256 "REPLACE_WITH_SHA256_DARWIN_AMD64"
    end
  end

  def install
    libexec.install Dir["*"]
    chmod "+x", libexec/"udl"
    chmod "+x", libexec/"tools/scdl"
    chmod "+x", libexec/"tools/yt-dlp"
    (bin/"udl").write_env_script libexec/"udl", PATH: "#{libexec}/tools:$PATH"
  end

  test do
    assert_match "udl version", shell_output("#{bin}/udl version")
    output = shell_output("#{bin}/udl doctor")
    assert_match "no sources configured yet", output
  end
end
