package ui

// An append sequence that survives being copied and can release a retained
// prefix without renumbering what remains. The DM only appends; the room also
// advances the front when its retention window fills.

// chunkSize is how many elements one chunk holds. It plays the two halves of
// an append's cost against each other: a larger chunk copies more elements, a
// smaller one copies more chunk headers. At 256 both stay small for any
// transcript a session reaches - one append copies at most 26KB of events,
// and even at 40,000 events only 156 chunk headers.
const chunkSize = 256

// chunked is an immutable sequence held as fixed-size chunks so appending or
// front trimming copies one chunk rather than the whole sequence.
//
// The copy is not optional. A DM method returns a new DM, so a plain append
// onto a shared slice writes into a backing array an older DM still points at:
// two appends from one base, and the second silently overwrites the first's
// event. TestAppendDoesNotShareItsBackingArray exists to catch exactly that.
//
// What chunking buys is the bound on it. Copying the whole slice is O(n) per
// append - invisible next to a transcript rebuild, and the dominant cost once
// the rebuild is gone: measured on this package, copying the event history
// costs 63µs an append at 4,000 events and 331µs at 40,000, and a session's
// history only grows. Rewriting the last chunk and the chunk headers instead
// costs at most chunkSize elements plus n/chunkSize headers, whatever n is.
//
// Nothing here is ever written through a header an older value still holds, so
// two DMs sharing a chunked need no coordination to stay correct.
//
// The zero value is an empty sequence.
type chunked[T any] struct {
	// chunks holds every element, in order. Every chunk but the last is
	// exactly chunkSize long, which is what lets slice reach an index by
	// dividing rather than by walking.
	chunks [][]T

	// base and n are the absolute bounds of the retained elements. base may
	// advance when a room reclaims its front; n never moves backwards, so line
	// indices held by a viewport or selection keep naming the same element.
	base, n int

	// offset is how many cleared slots precede base in the first chunk. Keeping
	// chunk alignment makes append and lookup constant-time after a front trim;
	// clearing those slots is what releases the evicted values.
	offset int
}

// len is the absolute index one past the last retained element.
func (c chunked[T]) len() int { return c.n }

// first is the absolute index of the first retained element.
func (c chunked[T]) first() int { return c.base }

// count is how many elements are retained.
func (c chunked[T]) count() int { return c.n - c.base }

// append returns a new sequence with vs added to the end. The receiver is
// unchanged and keeps every element it had.
func (c chunked[T]) append(vs ...T) chunked[T] {
	next := chunked[T]{chunks: make([][]T, len(c.chunks), len(c.chunks)+1), base: c.base, n: c.n + len(vs), offset: c.offset}
	copy(next.chunks, c.chunks)

	// Fill the last chunk first, by rewriting it rather than extending it -
	// extending is the write into someone else's backing array.
	used := c.offset + c.count()
	if last := len(next.chunks) - 1; last >= 0 && used%chunkSize != 0 {
		room := min(chunkSize-used%chunkSize, len(vs))
		grown := make([]T, len(next.chunks[last])+room)
		copy(grown, next.chunks[last])
		copy(grown[len(next.chunks[last]):], vs[:room])
		next.chunks[last] = grown
		vs = vs[room:]
	}
	for len(vs) > 0 {
		room := min(chunkSize, len(vs))
		full := make([]T, room)
		copy(full, vs[:room])
		next.chunks = append(next.chunks, full)
		vs = vs[room:]
	}
	return next
}

// at is the element at i, and the zero value for an index the sequence does
// not hold. Reached by dividing, the way slice does.
func (c chunked[T]) at(i int) T {
	var zero T
	if i < c.base || i >= c.n {
		return zero
	}
	at := c.offset + i - c.base
	return c.chunks[at/chunkSize][at%chunkSize]
}

// replace returns a new sequence with i set to v, and the receiver untouched.
//
// It rewrites the one chunk holding i rather than writing through it, which is
// append's own rule and for append's own reason: a DM method returns a new DM,
// so writing in place would change what an older DM already handed out. The
// cost is the same bound append pays - one chunk plus the chunk headers -
// which is what lets a tool call settle from running to finished without
// re-rendering a transcript.
func (c chunked[T]) replace(i int, v T) chunked[T] {
	if i < c.base || i >= c.n {
		return c
	}
	next := chunked[T]{chunks: make([][]T, len(c.chunks)), base: c.base, n: c.n, offset: c.offset}
	copy(next.chunks, c.chunks)

	pos := c.offset + i - c.base
	chunk := pos / chunkSize
	grown := make([]T, len(c.chunks[chunk]))
	copy(grown, c.chunks[chunk])
	grown[pos%chunkSize] = v
	next.chunks[chunk] = grown
	return next
}

// slice returns the elements in [from, to) as one slice, clamped to what the
// sequence holds. The result is a copy: the caller may keep it, and appending
// to the sequence afterwards will not disturb it.
func (c chunked[T]) slice(from, to int) []T {
	from, to = max(from, c.base), min(to, c.n)
	if to <= from {
		return nil
	}
	out := make([]T, 0, to-from)
	start := c.offset + from - c.base
	end := c.offset + to - c.base
	for pos := start; pos < end; {
		chunk := pos / chunkSize
		fromChunk := pos % chunkSize
		take := min(end-pos, len(c.chunks[chunk])-fromChunk)
		out = append(out, c.chunks[chunk][fromChunk:fromChunk+take]...)
		pos += take
	}
	return out
}

// trimBefore returns a sequence retaining [at, len). Absolute indices do not
// move. The receiver stays untouched, and the current value holds no backing
// array containing an evicted element.
func (c chunked[T]) trimBefore(at int) chunked[T] {
	at = min(max(at, c.base), c.n)
	if at == c.base {
		return c
	}
	if at == c.n {
		return chunked[T]{base: at, n: at}
	}

	pos := c.offset + at - c.base
	drop := pos / chunkSize
	offset := pos % chunkSize
	next := chunked[T]{chunks: make([][]T, len(c.chunks)-drop), base: at, n: c.n, offset: offset}
	copy(next.chunks, c.chunks[drop:])
	if offset > 0 {
		first := make([]T, len(next.chunks[0]))
		copy(first[offset:], next.chunks[0][offset:])
		next.chunks[0] = first
	}
	return next
}
