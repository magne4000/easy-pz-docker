package console

import (
	"fmt"
	"testing"

	"github.com/magne4000/easy-pz-docker/internal/events"
	"github.com/stretchr/testify/require"
)

func seqs(lines []events.ConsoleLine) []uint64 {
	out := []uint64{}
	for _, l := range lines {
		out = append(out, l.Seq)
	}
	return out
}

func TestRingWrap(t *testing.T) {
	r := NewRing(4)
	for i := range 6 {
		r.Append(fmt.Sprint(i))
	}
	require.Equal(t, []uint64{3, 4, 5, 6}, seqs(r.Tail(4)))
	require.Equal(t, []uint64{5, 6}, seqs(r.Tail(2)))
	require.Equal(t, []uint64{3, 4, 5, 6}, seqs(r.Tail(0)))
	require.Equal(t, "5", r.Tail(1)[0].Text)
}

func TestRingSince(t *testing.T) {
	r := NewRing(4)
	for range 6 {
		r.Append("x")
	}
	lines, gap := r.Since(1)
	require.True(t, gap)
	require.Equal(t, []uint64{3, 4, 5, 6}, seqs(lines))

	lines, gap = r.Since(2)
	require.False(t, gap)
	require.Equal(t, []uint64{3, 4, 5, 6}, seqs(lines))

	lines, gap = r.Since(4)
	require.False(t, gap)
	require.Equal(t, []uint64{5, 6}, seqs(lines))

	lines, gap = r.Since(6)
	require.False(t, gap)
	require.Empty(t, lines)
}

func TestTailDoesNotAlias(t *testing.T) {
	r := NewRing(3)
	r.Append("a")
	r.Append("b")
	got := r.Tail(2)
	for range 5 {
		r.Append("z")
	}
	require.Equal(t, "a", got[0].Text)
	require.Equal(t, "b", got[1].Text)
}
