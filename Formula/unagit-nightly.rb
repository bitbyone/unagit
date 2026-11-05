# The release workflow (.github/workflows/release.yml) writes this file
# whole when a vX.Y-pre.N tag is pushed; change it there. It is the
# nightly channel - test builds - and Homebrew refuses to build if the
# tag no longer points at the revision.
class UnagitNightly < Formula
  desc "Universal Navigator Around GIT (nightly test builds)"
  homepage "https://github.com/bitbyone/unagit"
  url "https://github.com/bitbyone/unagit.git",
      tag:      "v0.4-pre.4",
      revision: "f2932b2b0b6233cf7f7703411b35807e4f863619"
  version "0.4-pre.4"
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
