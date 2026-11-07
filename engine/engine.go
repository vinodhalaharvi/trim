// Package engine chooses which sentences of a talk to keep so the cut fits a
// target length and keeps as much interest as possible.
//
// It is a dynamic program built with github.com/vinodhalaharvi/dyna:
//
//	F X       = End | Keep i X | Skip i X       one decision per sentence
//	state       (sentence, was the previous one kept?)
//	grade       seconds used, adding up as sentences are kept
//	algebra     TopK of interest, lifted by grade
//
// Because answers are indexed by length, one solve answers every target length.
package engine

import (
	"math"
	"sort"

	"github.com/vinodhalaharvi/dyna"
)

// Sentence is one unit the cut can keep or drop.
type Sentence struct {
	Start, End float64
	Text       string
	Score      float64 // 0..1, how worth keeping
}

const (
	// Pad is kept before and after each sentence so words are not clipped.
	Pad = 0.12
	// Flow rewards keeping consecutive sentences, so cuts read less choppy.
	Flow = 0.1
	// K is how many cuts are kept per length while solving.
	K = 6
)

// Step labels one decision.
type Step struct {
	End  bool
	I    int
	Keep bool
	Cont bool // the previous sentence was kept too
}

type state struct {
	I    int
	Prev bool
}

// Talk is the input: sentences in order and the media's length.
type Talk struct {
	Sentences []Sentence
	Length    float64
}

func (t *Talk) cost(i int) float64 { s := t.Sentences[i]; return s.End - s.Start + 2*Pad }

func (t *Talk) value(i int, cont bool) float64 {
	s := t.Sentences[i]
	v := s.Score * (s.End - s.Start)
	if cont {
		v *= 1 + Flow
	}
	return v
}

// Solution is a solved talk: the best cuts at every length up to Cap.
type Solution struct {
	Talk  *Talk
	Pins  map[int]bool
	Cap   float64
	Unit  float64 // seconds per grade
	Root  dyna.Vec[dyna.Ranked[Step]]
	Stats dyna.Stats
}

func (t *Talk) coalgebra(pins map[int]bool) dyna.Coalgebra[state, Step] {
	n := len(t.Sentences)
	return func(s state) []dyna.Node[Step, state] {
		if s.I == n {
			return []dyna.Node[Step, state]{{Label: Step{End: true}}}
		}
		keep := dyna.Node[Step, state]{Label: Step{I: s.I, Keep: true, Cont: s.Prev}, Kids: []state{{s.I + 1, true}}}
		if pins[s.I] {
			return []dyna.Node[Step, state]{keep}
		}
		return []dyna.Node[Step, state]{keep, {Label: Step{I: s.I}, Kids: []state{{s.I + 1, false}}}}
	}
}

// Grades counts seconds in units of unit, rounding each sentence up, so a cut
// whose grade fits the target always fits in real seconds too.
func (t *Talk) grading(capacity, unit float64) dyna.Grading[Step] {
	n := int(math.Floor(capacity/unit+1e-9)) + 1
	g := make([]int, len(t.Sentences))
	for i := range g {
		g[i] = int(math.Ceil(t.cost(i)/unit - 1e-9))
	}
	return dyna.Grading[Step]{N: n, Value: func(b int) float64 { return float64(b) * unit }, Of: func(l Step, kids []int) int {
		switch {
		case l.End:
			return 0
		case !l.Keep:
			return kids[0]
		}
		if b := kids[0] + g[l.I]; b < n {
			return b
		}
		return -1
	}}
}

func (t *Talk) score(l Step, kids []float64, _ float64, _ []float64) float64 {
	switch {
	case l.End:
		return 0
	case !l.Keep:
		return kids[0]
	}
	return kids[0] + t.value(l.I, l.Cont)
}

// Algebra is the lifted TopK algebra for one capacity; exposed for law tests.
func (t *Talk) Algebra(capacity, unit float64) dyna.Algebra[Step, dyna.Vec[dyna.Ranked[Step]]] {
	return dyna.Lift(dyna.TopK(K, t.score), t.grading(capacity, unit))
}

// Solve finds the best cuts at every length up to capacity seconds, keeping
// the pinned sentences. Lengths are resolved to max(0.1 s, capacity/1200).
func (t *Talk) Solve(capacity float64, pins map[int]bool) *Solution {
	unit := math.Max(0.1, capacity/1200)
	root, st := dyna.Dyna(t.Algebra(capacity, unit), t.coalgebra(pins), state{0, false})
	return &Solution{Talk: t, Pins: pins, Cap: capacity, Unit: unit, Root: root, Stats: st}
}

// Cut is one way to shorten the talk.
type Cut struct {
	Sentences []int        `json:"sentences"`
	Spans     [][2]float64 `json:"spans"`    // media time ranges, in order
	Length    float64      `json:"length"`   // seconds of output
	Interest  float64      `json:"interest"` // average score of what is kept, by time
	value     float64
}

// MakeCut turns a list of kept sentences into media spans.
func (t *Talk) MakeCut(kept []int) Cut {
	sort.Ints(kept)
	c := Cut{Sentences: kept}
	var num, den float64
	for _, i := range kept {
		s := t.Sentences[i]
		a, b := math.Max(0, s.Start-Pad), s.End+Pad
		if t.Length > 0 {
			b = math.Min(b, t.Length)
		}
		if n := len(c.Spans); n > 0 && a <= c.Spans[n-1][1]+0.05 {
			c.Spans[n-1][1] = math.Max(c.Spans[n-1][1], b)
		} else {
			c.Spans = append(c.Spans, [2]float64{a, b})
		}
		num += s.Score * (s.End - s.Start)
		den += s.End - s.Start
	}
	for _, sp := range c.Spans {
		c.Length += sp[1] - sp[0]
	}
	if den > 0 {
		c.Interest = num / den
	}
	return c
}

func kept(e *dyna.Entry[Step]) []int {
	var out []int
	for ; e != nil && !e.Label.End; e = e.Kids[0] {
		if e.Label.Keep {
			out = append(out, e.Label.I)
		}
	}
	return out
}

// Best returns up to k cuts no longer than target, best first. After the
// first, each must differ from every earlier one in at least a third of its
// sentences, so suggestions are real alternatives.
func (s *Solution) Best(target float64, k int) []Cut {
	limit := int(math.Floor(target/s.Unit + 1e-9))
	var all []Cut
	for _, b := range s.Root.NZ {
		if b > limit {
			continue
		}
		for _, e := range s.Root.At[b] {
			ids := kept(e)
			if len(ids) == 0 {
				continue // keeping nothing is not a cut
			}
			c := s.Talk.MakeCut(ids)
			c.value = e.Score
			all = append(all, c)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].value > all[j].value })
	var out []Cut
	for _, c := range all {
		ok := true
		for _, o := range out {
			if jaccard(c.Sentences, o.Sentences) > 0.66 {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, c)
			if len(out) == k {
				break
			}
		}
	}
	return out
}

func jaccard(a, b []int) float64 {
	in := map[int]bool{}
	for _, x := range a {
		in[x] = true
	}
	inter := 0
	for _, x := range b {
		if in[x] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 1
	}
	return float64(inter) / float64(union)
}

// MinLength is the shortest target this solution can fill while keeping the
// pinned sentences, in the solver's rounded seconds.
func (s *Solution) MinLength() float64 {
	g := 0.0
	for i := range s.Pins {
		g += math.Ceil(s.Talk.cost(i)/s.Unit-1e-9) * s.Unit
	}
	return g
}

// PinnedLength is how long the pinned sentences alone run.
func (t *Talk) PinnedLength(pins map[int]bool) float64 {
	var ids []int
	for i := range pins {
		ids = append(ids, i)
	}
	return t.MakeCut(ids).Length
}

// TalkInterest is the average score of the whole talk, by time.
func (t *Talk) TalkInterest() float64 {
	all := make([]int, len(t.Sentences))
	for i := range all {
		all[i] = i
	}
	return t.MakeCut(all).Interest
}
