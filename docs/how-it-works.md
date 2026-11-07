# How trim chooses a cut

## The problem

A talk is a list of sentences, each with a length `dᵢ` and a score `sᵢ` between 0 and 1. A cut keeps some of them, in order. Given a target length `T`, trim finds the cut with the most *interest*, `Σ sᵢ·dᵢ` over kept sentences, whose length is at most `T`. Each kept sentence also costs `2 × 0.12 s` of padding so words are not clipped, and keeping two neighbouring sentences earns a 10% bonus so cuts read less choppy.

That is a 0/1 knapsack with a small twist (the bonus depends on the previous decision), solved exactly by dynamic programming.

## The dynamic program

trim is written with [dyna](https://github.com/vinodhalaharvi/dyna), which describes a dynamic program as three pieces:

```
F X     = End | Keep i X | Skip i X           one decision per sentence (the functor)
coalg   (i, prevKept) → [Keep i (i+1, true), Skip i (i+1, false)]
                         only Keep for a sentence you asked to keep
alg     TopK of interest, lifted by length
```

`Dyna` folds the unfolding of the coalgebra with the algebra and memoises on the state, so each `(sentence, previous kept?)` pair is solved once: 2n subproblems for n sentences.

### Indexing by length

The algebra is `Lift(TopK)`: instead of one best answer per subproblem, it keeps the best few answers *for every length*. Lengths are counted in small units (0.1 s, or the talk's length / 1200 if that is larger) and each sentence's length is rounded **up**, so a cut that fits in units always fits in real seconds. Keeping a sentence shifts a whole vector of answers by its length: lengths add, which is the monoid the grading is built on.

Because the root of the computation holds the best cuts at every length, the editor solves once per set of pinned sentences and then answers every position of the length slider by looking it up.

### Why keeping only the best is safe

The dynamic program throws away all but a few answers per subproblem and length. That is correct only if the algebra *distributes* over choosing the best:

```
a ⊗ (b ⊕ c) = (a ⊗ b) ⊕ (a ⊗ c)
```

Here `⊕` keeps the best k of two lists and `⊗` adds a sentence's interest; adding the same amount to every candidate never changes which ones are best, so the law holds. The tests check it on random values with dyna's `CheckLaws`, and compare the dynamic program with brute force (every possible cut, scored one by one) on 40 random talks.

## Suggestions

The best few cuts at nearby lengths are usually the same cut with one sentence swapped. **Suggest 3** keeps the best cut, then only accepts another if it differs in at least a third of its sentences (Jaccard similarity at most 0.66), so the options are real alternatives.

## Rendering

trim merges kept sentences that touch into spans and snaps every cut point inward to the video's frame grid; otherwise audio, which is cut to the sample, would drift against the picture by up to a frame per cut. Each span is opened as its own seeked input, so ffmpeg decodes only what is kept, and the audio fades in and out over 30 ms. Spans are rendered six at a time with uncompressed audio, then joined: the video is copied, and the audio is encoded once at the end so no encoder padding builds up at the joins. The kept subtitle cues are shifted onto the new timeline and written beside the output.
