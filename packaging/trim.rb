class Trim < Formula
  desc "Cut a talk, podcast or video to an exact length, keeping the best parts"
  homepage "https://github.com/vinodhalaharvi/trim"
  url "https://github.com/vinodhalaharvi/trim/archive/refs/tags/vVERSION.tar.gz"
  sha256 "SHA256"
  license "MIT"
  head "https://github.com/vinodhalaharvi/trim.git", branch: "main"

  depends_on "go" => :build
  depends_on "ffmpeg"

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.version=#{version}"), "./cmd/trim"
  end

  test do
    (testpath/"t.srt").write "1\n00:00:01,000 --> 00:00:04,000\nHello there, this is a test.\n\n2\n00:00:05,000 --> 00:00:09,000\nUm, so, can everyone hear me?\n"
    assert_match "Hello there", shell_output("#{bin}/trim score #{testpath}/t.srt")
    assert_match version.to_s, shell_output("#{bin}/trim version")
  end
end
