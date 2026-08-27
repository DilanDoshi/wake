package ui

import (
	"fmt"
	"slices"
	"testing"
)

// filled returns a sequence of n elements, each naming its own index.
func filled(n int) chunked[string] {
	var c chunked[string]
	for i := range n {
		c = c.append(element(i))
	}
	return c
}

func element(i int) string { return fmt.Sprintf("e%d", i) }

// at returns the single element at i, or "" if the sequence is shorter.
func at(c chunked[string], i int) string {
	if got := c.slice(i, i+1); len(got) == 1 {
		return got[0]
	}
	return ""
}

// Appending must never write into an array an older value still points at -
// the property the DM's whole immutability contract rests on, pinned here at
// the two places that can break it: the elements of the last chunk, and the
// list of chunk headers, which append deliberately leaves room in for one more
// chunk. The boundary lengths are where the second one is live, because the
// next append is the one that adds a chunk.
func TestChunkedAppendDoesNotDisturbAnEarlierValue(t *testing.T) {
	for _, base := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, 2 * chunkSize} {
		c := filled(base)

		first := c.append("branch one")
		second := c.append("branch two")

		if got := at(first, base); got != "branch one" {
			t.Errorf("from %d elements, the first branch holds %q at %d, want %q", base, got, base, "branch one")
		}
		if got := at(second, base); got != "branch two" {
			t.Errorf("from %d elements, the second branch holds %q at %d, want %q", base, got, base, "branch two")
		}
		if c.len() != base {
			t.Errorf("appending grew the value it was called on: %d elements, want %d", c.len(), base)
		}
		for i := range base {
			if got := at(c, i); got != element(i) {
				t.Errorf("from %d elements, element %d became %q", base, i, got)
			}
		}
	}
}

// A re-wrap hands the whole transcript over at once, so the bulk path has to
// place several chunks in one call and leave the same sequence a run of single
// appends would.
func TestChunkedAppendsInBulkExactlyAsItDoesOneAtATime(t *testing.T) {
	const n = 2*chunkSize + 7

	one := make([]string, n)
	for i := range one {
		one[i] = element(i)
	}
	bulk := chunked[string]{}.append(one...)

	if bulk.len() != n {
		t.Fatalf("bulk append holds %d elements, want %d", bulk.len(), n)
	}
	if got := bulk.slice(0, n); !slices.Equal(got, filled(n).slice(0, n)) {
		t.Errorf("bulk append and single appends disagree:\n%v", got)
	}
	// Half a chunk on top of a bulk load lands in the same place.
	if got := at(bulk.append("after"), n); got != "after" {
		t.Errorf("appending onto a bulk-loaded sequence holds %q, want %q", got, "after")
	}
}

// slice is how every read reaches the elements, including the one that draws
// the pane, so it has to hold across chunk boundaries and clamp rather than
// panic at the ends.
func TestChunkedSliceSpansChunksAndClampsToWhatIsThere(t *testing.T) {
	const n = 2*chunkSize + 5
	c := filled(n)

	for _, tc := range []struct {
		name     string
		from, to int
		want     []string
	}{
		{"across a chunk boundary", chunkSize - 2, chunkSize + 2, []string{element(chunkSize - 2), element(chunkSize - 1), element(chunkSize), element(chunkSize + 1)}},
		{"past the end", n - 2, n + 100, []string{element(n - 2), element(n - 1)}},
		{"before the start", -5, 2, []string{element(0), element(1)}},
		{"entirely past the end", n + 1, n + 4, nil},
		{"empty range", 10, 10, nil},
		{"reversed range", 10, 4, nil},
	} {
		if got := c.slice(tc.from, tc.to); !slices.Equal(got, tc.want) {
			t.Errorf("%s: slice(%d,%d) = %v, want %v", tc.name, tc.from, tc.to, got, tc.want)
		}
	}

	if got := c.slice(0, n); len(got) != n {
		t.Errorf("the whole sequence is %d elements, want %d", len(got), n)
	}
	if got := (chunked[string]{}).slice(0, 10); got != nil {
		t.Errorf("an empty sequence sliced to %v, want nil", got)
	}
}

// replace is what lets a tool call settle from running to finished without
// re-rendering the transcript, so it has to keep the property append is held
// to: a caller holding an older value keeps what it had.
func TestChunkedReplaceDoesNotDisturbAnEarlierValue(t *testing.T) {
	var c chunked[string]
	for i := range chunkSize * 3 {
		c = c.append(fmt.Sprintf("line %d", i))
	}
	before := c.slice(0, c.len())

	// One index in each chunk, including the boundaries either side of one.
	for _, i := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, chunkSize*3 - 1} {
		next := c.replace(i, "settled")
		if got := next.at(i); got != "settled" {
			t.Errorf("at(%d) = %q after replace, want %q", i, got, "settled")
		}
		if got := c.at(i); got != before[i] {
			t.Errorf("replace(%d) reached back into the receiver: at(%d) = %q, want %q", i, i, got, before[i])
		}
		if next.len() != c.len() {
			t.Errorf("replace(%d) changed the length: %d, want %d", i, next.len(), c.len())
		}
	}
	for i, want := range before {
		if got := c.at(i); got != want {
			t.Errorf("receiver moved at %d: %q, want %q", i, got, want)
		}
	}
}

// Out of range is a no-op rather than a panic: the line a tool block was drawn
// on is remembered across a re-wrap that may have shortened the transcript.
func TestChunkedReplaceOutOfRangeIsANoOp(t *testing.T) {
	c := chunked[string]{}.append("a", "b")
	for _, i := range []int{-1, 2, 99} {
		if got := c.replace(i, "x"); got.len() != 2 || got.at(0) != "a" || got.at(1) != "b" {
			t.Errorf("replace(%d) disturbed the sequence: %v", i, got.slice(0, got.len()))
		}
	}
}

func TestChunkedAtClampsToWhatIsThere(t *testing.T) {
	c := chunked[string]{}.append("a", "b")
	for _, i := range []int{-1, 2, 99} {
		if got := c.at(i); got != "" {
			t.Errorf("at(%d) = %q, want the zero value", i, got)
		}
	}
}

// Production mutation caught: rebuilding a trimmed sequence from index zero
// renumbers every retained transcript line and invalidates viewport/selection
// anchors held by an older value.
func TestChunkedFrontTrimPreservesAbsoluteIndicesAndOlderCopies(t *testing.T) {
	c := filled(2*chunkSize + 7)
	before := c
	trimmed := c.trimBefore(chunkSize + 3)

	if trimmed.first() != chunkSize+3 || trimmed.len() != c.len() {
		t.Fatalf("trimmed bounds = [%d,%d), want [%d,%d)", trimmed.first(), trimmed.len(), chunkSize+3, c.len())
	}
	if got := trimmed.at(chunkSize + 3); got != element(chunkSize+3) {
		t.Errorf("first retained absolute index holds %q, want %q", got, element(chunkSize+3))
	}
	if got := trimmed.at(chunkSize + 2); got != "" {
		t.Errorf("evicted absolute index still returns %q", got)
	}
	if got := before.at(0); got != element(0) {
		t.Errorf("front trim changed the older copy's first element to %q", got)
	}

	appended := trimmed.append("after")
	if got := appended.at(c.len()); got != "after" {
		t.Errorf("append after a front trim landed at %q, want absolute index %d", got, c.len())
	}
}

// Production mutation caught: reslicing the first retained chunk leaves its
// backing array holding pointer-bearing evicted values.
func TestChunkedFrontTrimClearsEvictedReferencesFromCurrentChunks(t *testing.T) {
	values := make([]*int, chunkSize+3)
	for i := range values {
		v := i
		values[i] = &v
	}
	c := chunked[*int]{}.append(values...)
	trimmed := c.trimBefore(3)

	for _, chunk := range trimmed.chunks {
		for _, got := range chunk {
			if got == values[0] || got == values[1] || got == values[2] {
				t.Fatal("a current chunk still reaches an evicted pointer")
			}
		}
	}
	for i := range 3 {
		if c.at(i) != values[i] {
			t.Fatalf("front trim cleared the older copy's pointer at %d", i)
		}
	}
}
