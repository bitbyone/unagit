# The release workflow (.github/workflows/release.yml) rewrites the tag and
# the revision below when a vX.Y tag is pushed; Homebrew refuses to build if
# the tag no longer points at that revision. Edit the rest by hand.
class Unagit < Formula
  desc "Universal Navigator Around GIT: repositories, worktrees and merge request reviews"
  homepage "https://github.com/bitbyone/unagit"
  url "https://github.com/bitbyone/unagit.git",
      tag:      "v0.1",
      revision: "2409c3d074271d5f9e6bbcb824ae49aed7f48971"
  license "MIT"
  head "https://github.com/bitbyone/unagit.git", branch: "main"

  depends_on "go" => :build

  def install
    ldflags = "-X github.com/tobola/unagit/internal/version.Version=#{version}"
    system "go", "build", *std_go_args(ldflags:), "./cmd/unagit"
  end

  test do
    assert_equal version.to_s, shell_output("#{bin}/unagit --version").strip
  end
end
