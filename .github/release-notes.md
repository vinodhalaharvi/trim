Install with Homebrew:

```sh
brew install vinodhalaharvi/tap/trim
```

- `trim -to 60s talk.mp4` cuts a talk, podcast or video to an exact length, keeping the sentences most worth keeping, with re-timed subtitles.
- `trim serve talk.mp4` opens an editor in your browser: drag the length, keep sentences or ranges, preview the cut, export.
- `trim score talk.srt` writes editable sentence scores, so you can bring your own ratings.

Needs ffmpeg and subtitles (.srt or .vtt). Binaries below are for macOS and Linux; the macOS ones are not notarised, so prefer Homebrew or run `xattr -d com.apple.quarantine trim`.
