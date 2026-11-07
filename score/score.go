// Package score rates how worth keeping each sentence is, from 0 to 1.
//
// The built-in Heuristic is deliberately simple: it penalises filler words,
// talk about the talk ("can you hear me", "next slide"), slow stretches and
// fragments. For better cuts, write scores yourself or with any tool you like
// into the CSV that `trim score` produces, and pass it back with -scores.
package score

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/vinodhalaharvi/trim/subs"
)

var (
	wordRe  = regexp.MustCompile(`[\p{L}\p{N}']+`)
	fillers = []string{"um", "uh", "erm", "er", "ah", "uhm", "hmm", "mm"}
	phrases = []string{"you know", "i mean", "sort of", "kind of", "basically", "anyway", "let me just", "where was i"}
	meta    = []string{"hear me", "slide", "slides", "mic", "microphone", "screen", "one second", "bear with me",
		"switch windows", "over time", "my notes", "can you see", "is this on", "water"}
)

// Heuristic scores a sentence from its words and timing alone.
func Heuristic(s subs.Sentence) float64 {
	text := strings.ToLower(s.Text)
	words := wordRe.FindAllString(text, -1)
	n := float64(len(words))
	if n == 0 {
		return 0.02
	}
	d := math.Max(s.Duration(), 0.1)
	filler := 0.0
	for _, w := range words {
		for _, f := range fillers {
			if w == f {
				filler++
			}
		}
	}
	padded := " " + strings.Join(words, " ") + " "
	for _, p := range phrases {
		filler += float64(strings.Count(padded, " "+p+" "))
	}
	metaHits := 0.0
	for _, m := range meta {
		if strings.Contains(padded, " "+m+" ") {
			metaHits++
		}
	}
	pace := n / d // words per second; conversational speech is ~2–3
	score := 0.6
	score -= 2.0 * filler / n
	score -= 0.35 * math.Min(metaHits, 2)
	if pace < 1.2 {
		score -= 0.25 * (1.2 - pace) / 1.2
	}
	if n < 4 {
		score -= 0.25
	}
	if strings.HasSuffix(strings.TrimSpace(s.Text), "?") {
		score += 0.05
	}
	if n >= 8 && filler == 0 && metaHits == 0 {
		score += 0.15
	}
	return math.Max(0.02, math.Min(1, score))
}

// WriteCSV writes one row per sentence: index, start, end, score, text.
func WriteCSV(w io.Writer, ss []subs.Sentence, scores []float64) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"index", "start", "end", "score", "text"}); err != nil {
		return err
	}
	for i, s := range ss {
		if err := cw.Write([]string{strconv.Itoa(i + 1), subs.FormatTime(s.Start), subs.FormatTime(s.End),
			strconv.FormatFloat(scores[i], 'f', 2, 64), s.Text}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// ReadCSV reads scores written by WriteCSV (and possibly edited). Rows are
// matched to sentences by index; the score column may be any non-negative
// number and is rescaled so the highest is 1.
func ReadCSV(path string, n int) ([]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out := make([]float64, n)
	for i := range out {
		out[i] = -1
	}
	top := 0.0
	for k, row := range rows {
		if k == 0 && len(row) > 0 && strings.EqualFold(strings.TrimSpace(row[0]), "index") {
			continue
		}
		if len(row) < 4 {
			return nil, fmt.Errorf("%s line %d: expected index,start,end,score,text", path, k+1)
		}
		idx, err1 := strconv.Atoi(strings.TrimSpace(row[0]))
		sc, err2 := strconv.ParseFloat(strings.TrimSpace(row[3]), 64)
		if err1 != nil || err2 != nil || idx < 1 || idx > n || sc < 0 {
			return nil, fmt.Errorf("%s line %d: bad index or score", path, k+1)
		}
		out[idx-1] = sc
		top = math.Max(top, sc)
	}
	for i, v := range out {
		if v < 0 {
			return nil, fmt.Errorf("%s: no score for sentence %d; re-run `trim score` after changing subtitles", path, i+1)
		}
		if top > 0 {
			out[i] = v / top
		}
	}
	return out, nil
}
