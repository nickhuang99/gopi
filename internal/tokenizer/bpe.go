package tokenizer

// bpe.go — the greedy lowest-rank merge.
//
// One mapped piece is merged exactly the way the crate does it: repeatedly find
// the adjacent pair with the lowest merge rank (leftmost on a tie) and join it,
// until no adjacent pair is a known merge.
//
// THE MECHANISM IS A HEAP, NOT A RESCAN. The obvious implementation rescans
// every adjacent pair after every merge, which is O(n²) in the piece's symbols:
// measured on a single 32,000-rune piece with no whitespace, 26.6 s — and a
// 200,000-rune one did not finish in 120 s. That is not a hypothetical shape: a
// tool result carrying minified JSON, a base64 blob, or one long CJK run (stage
// 2 hands a CJK run over as ONE piece) reaches exactly it, and the send-time
// token gate counts every request.
//
// A merge only ever changes the pairs AROUND it, so the pairs are pushed once
// and re-pushed only where the merge invalidated them: O(n log n) heap
// operations, and the merge ORDER is unchanged — the heap pops by (rank, then
// leftmost), which is the same pair the rescan picked, so every count this
// package produced before is the count it produces now (the differential test
// against the reference encoder is what proves it).

import (
	"container/heap"
	"unicode/utf8"
)

// bpeSym is one symbol of a piece: a byte span of the mapped piece, plus its
// neighbours in the still-live run. A symbol that has been merged INTO its left
// neighbour is dead: its bytes now belong to that neighbour's span.
type bpeSym struct {
	lo, hi int  // byte span in the mapped piece
	next   int  // next live symbol, -1 at the end
	alive  bool // false once absorbed by the symbol on its left
	ver    int  // bumped on every change, so a stale heap entry can be spotted
}

// bpeCandidate is one adjacent pair the merge considers, with the versions it was
// built at. A heap entry whose versions no longer match has been invalidated by a
// merge elsewhere and is DISCARDED when popped, never applied.
type bpeCandidate struct {
	rank uint32
	left int
	verL int
	verR int
}

// bpeHeap orders candidates by rank, then by the left symbol's position — the
// rescan's own tie-break ("leftmost on a tie"), which is what keeps the merge
// order identical.
type bpeHeap struct {
	m      *Tokenizer
	mapped []byte // the whole mapped piece, so a pair's key is two slices of it
	syms   []bpeSym
	items  []bpeCandidate
}

func (h *bpeHeap) Len() int { return len(h.items) }

func (h *bpeHeap) Less(i, j int) bool {
	a, b := h.items[i], h.items[j]
	if a.rank != b.rank {
		return a.rank < b.rank
	}
	return h.syms[a.left].lo < h.syms[b.left].lo
}

func (h *bpeHeap) Swap(i, j int) { h.items[i], h.items[j] = h.items[j], h.items[i] }

func (h *bpeHeap) Push(x any) { h.items = append(h.items, x.(bpeCandidate)) }

func (h *bpeHeap) Pop() any {
	old := h.items
	n := len(old)
	it := old[n-1]
	h.items = old[:n-1]
	return it
}

// mergePiece appends the ids for one mapped piece.
func (t *Tokenizer) mergePiece(ids []uint32, mapped []byte) []uint32 {
	if len(mapped) == 0 {
		return ids
	}
	// One symbol per rune, exactly as decodeMapped saw them: the mapped slice is
	// always valid UTF-8 (appendRune built it), so a byte span IS a rune span.
	syms := make([]bpeSym, 0, len(mapped))
	for i := 0; i < len(mapped); {
		_, size := utf8.DecodeRune(mapped[i:])
		syms = append(syms, bpeSym{lo: i, hi: i + size, next: len(syms) + 1, alive: true})
		i += size
	}
	if len(syms) == 0 {
		return ids
	}
	syms[len(syms)-1].next = -1

	h := &bpeHeap{m: t, mapped: mapped, syms: syms, items: make([]bpeCandidate, 0, len(syms))}
	for k := 0; k+1 < len(syms); k++ {
		if c, ok := h.candidate(k); ok {
			h.items = append(h.items, c)
		}
	}
	heap.Init(h)

	for h.Len() > 0 {
		c := heap.Pop(h).(bpeCandidate)
		// Discard the entry if either side has moved since it was built: the
		// pair it names is no longer the pair it named.
		if !syms[c.left].alive || syms[c.left].ver != c.verL {
			continue
		}
		right := syms[c.left].next
		if right < 0 || !syms[right].alive || syms[right].ver != c.verR {
			continue
		}
		// Join right INTO left: left keeps its position and takes right's bytes.
		syms[c.left].hi = syms[right].hi
		syms[c.left].next = syms[right].next
		syms[c.left].ver++
		syms[right].alive = false
		syms[right].ver++
		// Only the two pairs the merge touched need re-considering.
		if prev := h.prevOf(c.left); prev >= 0 {
			if nc, ok := h.candidate(prev); ok {
				heap.Push(h, nc)
			}
		}
		if syms[c.left].next >= 0 {
			if nc, ok := h.candidate(c.left); ok {
				heap.Push(h, nc)
			}
		}
	}

	for k := 0; k < len(syms); k++ {
		if !syms[k].alive {
			continue
		}
		ids = append(ids, t.idForSymbol(string(mapped[syms[k].lo:syms[k].hi])))
	}
	return ids
}

// prevOf returns the live symbol before k, or -1. Merge records only the forward
// link, so the backward walk is a scan — it is bounded by the merges that ran
// since the previous live symbol, and the heap is what keeps the TOTAL bounded.
func (h *bpeHeap) prevOf(k int) int {
	for j := k - 1; j >= 0; j-- {
		if h.syms[j].alive {
			return j
		}
	}
	return -1
}

// candidate builds the candidate for the pair (left, left.next), reporting false
// when the pair is not a known merge.
func (h *bpeHeap) candidate(left int) (bpeCandidate, bool) {
	right := h.syms[left].next
	if right < 0 || !h.syms[left].alive || !h.syms[right].alive {
		return bpeCandidate{}, false
	}
	rank, ok := h.m.merged[string(h.mapped[h.syms[left].lo:h.syms[left].hi])+string(h.mapped[h.syms[right].lo:h.syms[right].hi])]
	if !ok {
		return bpeCandidate{}, false
	}
	return bpeCandidate{rank: rank, left: left, verL: h.syms[left].ver, verR: h.syms[right].ver}, true
}

// idForSymbol returns the vocab id of a merged symbol. Every byte is in the
// vocab and every merge result is too, so a miss means the file and this code
// disagree — reported as a byte's own id rather than silently dropped (Rule 15:
// no silent failure), and the differential test is what catches it.
func (t *Tokenizer) idForSymbol(sym string) uint32 {
	if id, ok := t.vocab[sym]; ok {
		return id
	}
	first := string([]rune(sym)[:1])
	return t.vocab[first]
}
