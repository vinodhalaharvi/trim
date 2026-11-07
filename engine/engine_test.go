package engine

import (
	"math"
	"math/rand"
	"testing"

	"github.com/vinodhalaharvi/dyna"
)

func randomTalk(r *rand.Rand, n int) *Talk {
	t := &Talk{}
	at := 0.0
	for i := 0; i < n; i++ {
		d := 2 + r.Float64()*12
		at += r.Float64() * 0.8
		t.Sentences = append(t.Sentences, Sentence{Start: at, End: at + d, Score: r.Float64()})
		at += d
	}
	t.Length = at + 1
	return t
}

func TestDynaEqualsBruteForce(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for trial := 0; trial < 40; trial++ {
		talk := randomTalk(r, 1+r.Intn(10))
		pins := map[int]bool{}
		if r.Intn(2) == 0 {
			pins[r.Intn(len(talk.Sentences))] = true
		}
		capacity := 10 + r.Float64()*40
		unit := math.Max(0.1, capacity/1200)
		alg := talk.Algebra(capacity, unit)
		dp, _ := dyna.Dyna(alg, talk.coalgebra(pins), state{0, false})
		bf, trees := dyna.BruteForce(alg, talk.coalgebra(pins), state{0, false})
		if len(dp.NZ) != len(bf.NZ) {
			t.Fatalf("trial %d: %d lengths by DP, %d by brute force", trial, len(dp.NZ), len(bf.NZ))
		}
		for _, b := range bf.NZ {
			if !dyna.RankedScoresEqual(dp.At[b], bf.At[b]) {
				t.Fatalf("trial %d, grade %d: DP and brute force (%d cuts) disagree", trial, b, trees)
			}
		}
	}
}

func TestLaws(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	talk := randomTalk(r, 5)
	labels := []Step{{I: 0, Keep: true}, {I: 1, Keep: true, Cont: true}, {I: 2}}
	res := dyna.CheckLaws(dyna.TopK(K, talk.score), dyna.LawSpec[Step, dyna.Ranked[Step]]{
		Name: "TopK interest", Labels: labels, Arity: func(Step) int { return 1 },
		Gen: dyna.GenRanked(K, Step{End: true}), Eq: dyna.RankedScoresEqual[Step]}, 5000, r)
	for _, l := range res {
		if !l.OK {
			t.Errorf("%s: %s fails", l.Algebra, l.Law)
		}
	}
}

func TestCutsFitAndKeepPins(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	for trial := 0; trial < 30; trial++ {
		talk := randomTalk(r, 20+r.Intn(60))
		pins := map[int]bool{r.Intn(len(talk.Sentences)): true}
		sol := talk.Solve(talk.Length, pins)
		for _, target := range []float64{30, 60, 90, 180} {
			if target < talk.PinnedLength(pins) {
				continue
			}
			cuts := sol.Best(target, 3)
			if len(cuts) == 0 {
				t.Fatalf("no cut for %v s", target)
			}
			for _, c := range cuts {
				if c.Length > target+1e-6 {
					t.Fatalf("cut is %.2f s, longer than %v s", c.Length, target)
				}
				for p := range pins {
					found := false
					for _, i := range c.Sentences {
						found = found || i == p
					}
					if !found {
						t.Fatalf("pinned sentence %d missing", p)
					}
				}
			}
		}
	}
}

func BenchmarkSolveLongTalk(b *testing.B) {
	talk := randomTalk(rand.New(rand.NewSource(4)), 250) // about 30 minutes
	for i := 0; i < b.N; i++ {
		talk.Solve(talk.Length, nil)
	}
}
