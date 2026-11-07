// Package subs reads SRT and WebVTT subtitles and groups cues into sentences.
package subs

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Cue is one timed subtitle.
type Cue struct {
	Start, End float64 // seconds
	Text       string
}

// Sentence is a run of cues that reads as one thought.
type Sentence struct {
	Start, End float64
	Text       string
	Cues       []Cue
}

func (s Sentence) Duration() float64 { return s.End - s.Start }

var (
	timing = regexp.MustCompile(`^\s*((?:\d+:)?\d{1,2}:\d{2}[.,]\d{1,3})\s*-->\s*((?:\d+:)?\d{1,2}:\d{2}[.,]\d{1,3})`)
	tags   = regexp.MustCompile(`<[^>]*>|\{\\[^}]*\}`)
	spaces = regexp.MustCompile(`\s+`)
)

// ParseTime reads 01:02:03,456 or 02:03.456.
func ParseTime(s string) (float64, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	parts := strings.Split(s, ":")
	total := 0.0
	for _, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return 0, fmt.Errorf("bad time %q", s)
		}
		total = total*60 + v
	}
	return total, nil
}

// Read parses SRT or WebVTT; the format is detected from the content.
func Read(r io.Reader) ([]Cue, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var cues []Cue
	var cur *Cue
	flush := func() {
		if cur != nil {
			cur.Text = strings.TrimSpace(spaces.ReplaceAllString(tags.ReplaceAllString(cur.Text, ""), " "))
			if cur.Text != "" && cur.End > cur.Start {
				cues = append(cues, *cur)
			}
			cur = nil
		}
	}
	for sc.Scan() {
		line := strings.TrimPrefix(sc.Text(), "\ufeff")
		if m := timing.FindStringSubmatch(line); m != nil {
			flush()
			a, err1 := ParseTime(m[1])
			b, err2 := ParseTime(m[2])
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("bad cue timing: %q", line)
			}
			cur = &Cue{Start: a, End: b}
			continue
		}
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if cur != nil {
			cur.Text += " " + line
		}
	}
	flush()
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(cues) == 0 {
		return nil, fmt.Errorf("no subtitle cues found (expected SRT or WebVTT)")
	}
	return cues, nil
}

// ReadFile reads a subtitle file.
func ReadFile(path string) ([]Cue, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cues, err := Read(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cues, nil
}

// Sentences groups cues: a sentence ends at end punctuation, at a pause of
// more than maxGap seconds, or once it has run maxLen seconds.
func Sentences(cues []Cue, maxGap, maxLen float64) []Sentence {
	var out []Sentence
	var cur *Sentence
	for i, c := range cues {
		if cur == nil {
			cur = &Sentence{Start: c.Start}
		}
		cur.Cues = append(cur.Cues, c)
		cur.End = c.End
		if cur.Text != "" {
			cur.Text += " "
		}
		cur.Text += c.Text
		last := i == len(cues)-1
		gap := 0.0
		if !last {
			gap = cues[i+1].Start - c.End
		}
		if last || endsSentence(c.Text) || gap > maxGap || cur.End-cur.Start >= maxLen {
			out = append(out, *cur)
			cur = nil
		}
	}
	return out
}

func endsSentence(t string) bool {
	t = strings.TrimRight(strings.TrimSpace(t), `"'”’)]`)
	return strings.HasSuffix(t, ".") || strings.HasSuffix(t, "?") || strings.HasSuffix(t, "!") || strings.HasSuffix(t, "…")
}

// FormatTime writes SRT time: 00:01:02,345.
func FormatTime(t float64) string {
	if t < 0 {
		t = 0
	}
	ms := int(t*1000 + 0.5)
	return fmt.Sprintf("%02d:%02d:%02d,%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

// WriteSRT writes cues as SRT.
func WriteSRT(w io.Writer, cues []Cue) error {
	for i, c := range cues {
		if _, err := fmt.Fprintf(w, "%d\n%s --> %s\n%s\n\n", i+1, FormatTime(c.Start), FormatTime(c.End), c.Text); err != nil {
			return err
		}
	}
	return nil
}
