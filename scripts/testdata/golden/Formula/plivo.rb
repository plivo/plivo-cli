class Plivo < Formula
  desc "Command-line interface for the Plivo API"
  homepage "https://github.com/plivo/plivo-cli"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/plivo/plivo-cli/releases/download/v0.3.0/plivo_darwin_arm64"
      sha256 "9c5677ed285a7d094fb20596e53112a0c0fe4829cabf91a4ab0464bbe22b8ff7"
    end
    on_intel do
      url "https://github.com/plivo/plivo-cli/releases/download/v0.3.0/plivo_darwin_amd64"
      sha256 "dc80596c6d6585bb860aeb9c122478fcb1934a8405750eb0613e3e078e79bc9c"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/plivo/plivo-cli/releases/download/v0.3.0/plivo_linux_arm64"
      sha256 "fa4a28c532848685ff12b6f0488c23172c4df81296a119542044ebedf064339d"
    end
    on_intel do
      url "https://github.com/plivo/plivo-cli/releases/download/v0.3.0/plivo_linux_amd64"
      sha256 "b9bf7b995f1f196aea4bd1695b9b937b97c844853645d8e9f345aca87450ec51"
    end
  end

  def install
    bin.install Dir["plivo_*"].first => "plivo"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/plivo --version")
  end
end
