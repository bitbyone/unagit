# The release workflow (.github/workflows/release.yml) writes this file
# whole when a vX.Y-pre.N tag is pushed; change it there. It is the
# nightly channel - test builds - and Homebrew refuses to build if the
# tag no longer points at the revision.
class UnagitNightly < Formula
  desc "Universal Navigator Around GIT (nightly test builds)"
  homepage "https://github.com/bitbyone/unagit"
  url "https://github.com/bitbyone/unagit.git",
      tag:      "v0.5-pre.2",
      revision: "ad399df9c3d655794ebe306bb1030647e19625a0"
  version "0.5-pre.2"
  license "MIT"

  conflicts_with "unagit", because: "both install the unagit command"

  depends_on "go" => :build

  def install
    ldflags = "-X github.com/tobola/unagit/internal/version.Version=#{version}"
    system "go", "build", *std_go_args(ldflags:, output: bin/"unagit"), "./cmd/unagit"
  end

  test do
    assert_equal version.to_s, shell_output("#{bin}/unagit --version").strip
  end
end
