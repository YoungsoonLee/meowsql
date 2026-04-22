# typed: false
# frozen_string_literal: true

# This formula is maintained in a separate Homebrew tap repo:
#   https://github.com/YoungsoonLee/homebrew-meowsql
#
# To update after a new release:
#   1. Download the release tarballs and run `sha256sum` on each.
#   2. Replace the sha256 values below.
#   3. Bump the version string.

class Meowsql < Formula
  desc "SQL performance tuning agent — paste a slow query, get a 10x speedup"
  homepage "https://github.com/YoungsoonLee/meowsql"
  version "0.1.0"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/YoungsoonLee/meowsql/releases/download/v#{version}/meowsql_darwin_arm64.tar.gz"
      sha256 "REPLACE_WITH_SHA256_DARWIN_ARM64"
    end
    on_intel do
      url "https://github.com/YoungsoonLee/meowsql/releases/download/v#{version}/meowsql_darwin_amd64.tar.gz"
      sha256 "REPLACE_WITH_SHA256_DARWIN_AMD64"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/YoungsoonLee/meowsql/releases/download/v#{version}/meowsql_linux_arm64.tar.gz"
      sha256 "REPLACE_WITH_SHA256_LINUX_ARM64"
    end
    on_intel do
      url "https://github.com/YoungsoonLee/meowsql/releases/download/v#{version}/meowsql_linux_amd64.tar.gz"
      sha256 "REPLACE_WITH_SHA256_LINUX_AMD64"
    end
  end

  def install
    bin.install "meowsql"
  end

  test do
    assert_match "meowsql", shell_output("#{bin}/meowsql --help")
  end
end
