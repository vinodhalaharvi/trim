package score

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/vinodhalaharvi/trim/subs"
)

func TestHeuristicOrders(t *testing.T) {
	good := Heuristic(subs.Sentence{Start: 0, End: 5, Text: "Distributivity is optimal substructure, stated as an equation."})
	filler := Heuristic(subs.Sentence{Start: 0, End: 5, Text: "Um, so, you know, um, where was I?"})
	meta := Heuristic(subs.Sentence{Start: 0, End: 5, Text: "Can everyone at the back hear me on this mic?"})
	if !(good > filler && good > meta) {
		t.Fatalf("good %.2f, filler %.2f, meta %.2f", good, filler, meta)
	}
}

func TestCSVRoundTrip(t *testing.T) {
	ss := []subs.Sentence{{Start: 1, End: 2, Text: "one, with a comma"}, {Start: 3, End: 4, Text: "two"}}
	var b bytes.Buffer
	if err := WriteCSV(&b, ss, []float64{0.5, 0.25}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "s.csv")
	os.WriteFile(p, b.Bytes(), 0o644)
	got, err := ReadCSV(p, 2)
	if err != nil || got[0] != 1 || got[1] != 0.5 {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := ReadCSV(p, 3); err == nil {
		t.Fatal("missing sentence not reported")
	}
}
