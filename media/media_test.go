package media

import (
	"testing"

	"github.com/vinodhalaharvi/trim/subs"
)

func TestSnapKeepsFrameGrid(t *testing.T) {
	got := Snap([][2]float64{{1.012, 2.049}, {5.5, 5.51}}, 25)
	if len(got) != 1 || got[0] != [2]float64{1.0, 2.04} {
		t.Fatalf("got %v", got)
	}
}

func TestRetime(t *testing.T) {
	ss := []subs.Sentence{
		{Cues: []subs.Cue{{Start: 10, End: 12, Text: "a"}, {Start: 12, End: 14, Text: "b"}}},
		{Cues: []subs.Cue{{Start: 30, End: 33, Text: "c"}}},
	}
	cues := Retime(ss, []int{0, 1}, [][2]float64{{9.88, 14.12}, {29.88, 33.12}})
	want := []float64{0.12, 2.12, 4.24 + 0.12}
	for i, c := range cues {
		if d := c.Start - want[i]; d > 1e-9 || d < -1e-9 {
			t.Fatalf("cue %d starts at %v, want %v", i, c.Start, want[i])
		}
	}
}
