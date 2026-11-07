# trim

Cut a talk, podcast or video to an exact length, keeping the parts most worth keeping.

```sh
brew install vinodhalaharvi/tap/trim

trim -to 60s talk.mp4                 # the best minute, as talk-60s.mp4
trim -to 90s -suggest 3 talk.mp4      # three different 90-second cuts
trim -to 2m -keep 4:05 podcast.mp3    # always keep what is said at 4:05
trim serve talk.mp4                   # pick the length in your browser
```

trim works sentence by sentence, so it never cuts anyone off mid-word. Each output gets subtitles re-timed to match (`talk-60s.srt`).

## What it needs

- **ffmpeg**, which Homebrew installs with trim.
- **Subtitles** for the talk, as `.srt` or `.vtt`. trim looks for one beside the video with the same name (`talk.srt` for `talk.mp4`). No subtitles yet? Any transcriber works, for example [whisper](https://github.com/openai/whisper):

  ```sh
  whisper talk.mp4 --output_format srt
  ```

## The editor

```sh
trim serve talk.mp4
```

Opens a local page where you drag the length and the cut updates instantly. Click a sentence on the timeline to always keep it; shift-click or drag across the timeline to keep a whole range. **Play the cut** previews just the kept parts with captions, **Suggest 3** shows different alternatives, and **Export** writes the file beside the original. Nothing leaves your computer.

## Choosing what is worth keeping

Every sentence gets a score from 0 to 1. Out of the box, trim uses a simple rule of thumb: it marks down filler words ("um", "you know"), talk about the talk ("can everyone hear me", "next slide"), slow stretches and fragments. That removes the obvious dead weight but knows nothing about what you said.

For better cuts, give trim your own scores:

```sh
trim score talk.srt > scores.csv     # one row per sentence, with the default score
# edit the score column by hand, or fill it with any tool you like
trim -to 60s -scores scores.csv talk.mp4
```

Scores can be any non-negative numbers; trim rescales them. A good source is a language model: give it `scores.csv` and ask it to rate each sentence for how well it stands on its own.

## Flags

| Flag | Meaning |
|---|---|
| `-to LENGTH` | target length: `60s`, `1m30s`, `90` or `1:30` (required) |
| `-keep TIMES` | always keep the sentences at these times: `0:12,4:05-5:10` |
| `-suggest N` | write N different cuts: `talk-90s-1.mp4`, `talk-90s-2.mp4`, … |
| `-scores FILE` | sentence scores from `trim score` |
| `-o FILE` | output file (default `NAME-60s.EXT` beside the input) |
| `-dry-run` | print the kept sentences without writing any media |

Video is re-encoded with H.264 so every cut lands exactly; cut points are snapped to video frames so sound and picture stay in sync, and the audio fades for 30 ms at each cut so there are no clicks. Audio-only files (`.mp3`, `.m4a`, `.wav`) work too.

## How it works

Choosing sentences to fill a length is a knapsack problem. trim solves it exactly with a dynamic program from [dyna](https://github.com/vinodhalaharvi/dyna), indexed by length, so one solve answers every length at once; that is why dragging the length in the editor is instant. A 30-minute talk solves in about 0.3 seconds. The details are in [docs/how-it-works.md](docs/how-it-works.md).

## License

MIT
