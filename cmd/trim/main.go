// Command trim cuts a talk, podcast or video to an exact length, keeping the
// sentences most worth keeping.
package main

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/vinodhalaharvi/trim/engine"
	"github.com/vinodhalaharvi/trim/media"
	"github.com/vinodhalaharvi/trim/score"
	"github.com/vinodhalaharvi/trim/subs"
)

var version = "dev"

const usage = `trim cuts a talk, podcast or video to an exact length, keeping the best parts.

Usage:
  trim -to LENGTH [flags] video [subtitles]   write the cut
  trim serve video [subtitles]                edit in your browser
  trim score subtitles > scores.csv           write editable sentence scores
  trim version

It needs subtitles (.srt or .vtt). By default it looks for one next to the
video with the same name. No subtitles yet? Any transcriber works, e.g.
  whisper talk.mp4 --output_format srt

Flags:
  -to LENGTH      target length: 60s, 1m30s, 90 or 1:30 (required)
  -keep TIMES     always keep the sentences at these times: 0:12,4:05-5:10
  -suggest N      write N different cuts (FILE-1, FILE-2, …)
  -scores FILE    sentence scores from 'trim score', edited by you or any tool
  -o FILE         output file (default: NAME-60s.EXT beside the input)
  -dry-run        print the cut without writing media (no ffmpeg needed)

Examples:
  trim -to 60s talk.mp4
  trim -to 90s -suggest 3 talk.mp4
  trim -to 2m -keep 4:05 -scores mine.csv podcast.mp3 podcast.srt
`

func main() {
	debug.SetGCPercent(400)
	args := os.Args[1:]
	var err error
	switch {
	case len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help":
		fmt.Print(usage)
		return
	case args[0] == "version" || args[0] == "--version":
		fmt.Println("trim", version)
		return
	case args[0] == "serve":
		err = serve(args[1:])
	case args[0] == "score":
		err = writeScores(args[1:])
	default:
		err = cut(args)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "trim:", err)
		os.Exit(1)
	}
}

func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// parseLength reads 60s, 1m30s, 90, 1:30 or 1:02:03.
func parseLength(s string) (float64, error) {
	if strings.Contains(s, ":") {
		return subs.ParseTime(s)
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("can't read length %q: use 60s, 1m30s, 90 or 1:30", s)
	}
	return d.Seconds(), nil
}

func fmtLen(s float64) string {
	t := int(math.Round(s))
	if t >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", t/3600, t/60%60, t%60)
	}
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}

// input is everything trim knows about one talk.
type input struct {
	Media, Subs string
	Info        media.Info
	Sentences   []subs.Sentence
	Talk        *engine.Talk
}

func findSubs(mediaPath string) (string, error) {
	base := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath))
	for _, ext := range []string{".srt", ".vtt", ".en.srt", ".en.vtt"} {
		if _, err := os.Stat(base + ext); err == nil {
			return base + ext, nil
		}
	}
	return "", fmt.Errorf("no subtitles found for %s; pass a .srt or .vtt file after it, or make one with a transcriber such as whisper", mediaPath)
}

func load(mediaPath, subsPath, scoresPath string, needMedia bool) (*input, error) {
	in := &input{Media: mediaPath, Subs: subsPath}
	var err error
	if in.Subs == "" {
		if in.Subs, err = findSubs(mediaPath); err != nil {
			return nil, err
		}
	}
	cues, err := subs.ReadFile(in.Subs)
	if err != nil {
		return nil, err
	}
	in.Sentences = subs.Sentences(cues, 1.0, 25)
	scores := make([]float64, len(in.Sentences))
	if scoresPath != "" {
		if scores, err = score.ReadCSV(scoresPath, len(in.Sentences)); err != nil {
			return nil, err
		}
	} else {
		for i, s := range in.Sentences {
			scores[i] = score.Heuristic(s)
		}
	}
	if needMedia {
		if in.Info, err = media.Probe(mediaPath); err != nil {
			return nil, err
		}
	} else if _, err := os.Stat(mediaPath); err != nil {
		return nil, err
	}
	in.Talk = &engine.Talk{Length: in.Info.Length}
	for i, s := range in.Sentences {
		in.Talk.Sentences = append(in.Talk.Sentences, engine.Sentence{Start: s.Start, End: s.End, Text: s.Text, Score: scores[i]})
	}
	return in, nil
}

// pinsAt maps -keep times and ranges to sentence indices.
func pinsAt(spec string, ss []subs.Sentence) (map[int]bool, error) {
	pins := map[int]bool{}
	if strings.TrimSpace(spec) == "" {
		return pins, nil
	}
	for _, part := range strings.Split(spec, ",") {
		a, b, isRange := strings.Cut(strings.TrimSpace(part), "-")
		from, err := parseLength(a)
		if err != nil {
			return nil, fmt.Errorf("-keep: %v", err)
		}
		to := from
		if isRange {
			if to, err = parseLength(b); err != nil {
				return nil, fmt.Errorf("-keep: %v", err)
			}
		}
		found := false
		for i, s := range ss {
			if s.End > from && s.Start <= to {
				pins[i], found = true, true
			}
		}
		if !found {
			return nil, fmt.Errorf("-keep: no sentence at %s", part)
		}
	}
	return pins, nil
}

func cut(args []string) error {
	fs := flag.NewFlagSet("trim", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	to := fs.String("to", "", "")
	keep := fs.String("keep", "", "")
	suggest := fs.Int("suggest", 1, "")
	scores := fs.String("scores", "", "")
	out := fs.String("o", "", "")
	dry := fs.Bool("dry-run", false, "")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 || len(pos) > 2 {
		return errors.New("give a video or audio file, and optionally its subtitles: trim -to 60s talk.mp4")
	}
	if *to == "" {
		return errors.New("say how long the cut should be, e.g. -to 60s")
	}
	target, err := parseLength(*to)
	if err != nil {
		return err
	}
	subsPath := ""
	if len(pos) == 2 {
		subsPath = pos[1]
	}
	in, err := load(pos[0], subsPath, *scores, !*dry)
	if err != nil {
		return err
	}
	pins, err := pinsAt(*keep, in.Sentences)
	if err != nil {
		return err
	}
	if pl := in.Talk.PinnedLength(pins); pl > target {
		return fmt.Errorf("the sentences you asked to keep run %s, longer than %s", fmtLen(pl), fmtLen(target))
	}
	total := in.Talk.MakeCut(allOf(len(in.Sentences))).Length
	fmt.Printf("%s · %d sentences from %s\n", fmtLen(total), len(in.Sentences), filepath.Base(in.Subs))
	sol := in.Talk.Solve(target, pins)
	cuts := sol.Best(target, max(1, *suggest))
	if len(cuts) == 0 {
		return fmt.Errorf("no cut fits in %s; try a longer length", fmtLen(target))
	}
	talkAvg := in.Talk.TalkInterest()
	for k, c := range cuts {
		name := outputName(*out, in.Media, target, k, *suggest)
		fmt.Printf("%s  %s  %d of %d sentences · %.0f%% interest (whole talk %.0f%%)\n",
			name, fmtLen(c.Length), len(c.Sentences), len(in.Sentences), 100*c.Interest, 100*talkAvg)
		if *dry {
			for _, i := range c.Sentences {
				s := in.Sentences[i]
				fmt.Printf("    %7s  %s\n", fmtLen(s.Start), s.Text)
			}
			continue
		}
		if err := media.Render(in.Media, name, in.Info, c.Spans); err != nil {
			return err
		}
		srt := strings.TrimSuffix(name, filepath.Ext(name)) + ".srt"
		if err := media.WriteSRTFile(srt, media.Retime(in.Sentences, c.Sentences, media.Snap(c.Spans, in.Info.FPS))); err != nil {
			return err
		}
	}
	return nil
}

func allOf(n int) []int {
	a := make([]int, n)
	for i := range a {
		a[i] = i
	}
	return a
}

func outputName(out, mediaPath string, target float64, k, suggest int) string {
	if out == "" {
		ext := filepath.Ext(mediaPath)
		out = strings.TrimSuffix(mediaPath, ext) + "-" + strconv.Itoa(int(math.Round(target))) + "s" + ext
	}
	if suggest > 1 {
		ext := filepath.Ext(out)
		out = strings.TrimSuffix(out, ext) + "-" + strconv.Itoa(k+1) + ext
	}
	return out
}

func writeScores(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: trim score subtitles.srt > scores.csv")
	}
	cues, err := subs.ReadFile(args[0])
	if err != nil {
		return err
	}
	ss := subs.Sentences(cues, 1.0, 25)
	sc := make([]float64, len(ss))
	for i, s := range ss {
		sc[i] = score.Heuristic(s)
	}
	return score.WriteCSV(os.Stdout, ss, sc)
}
