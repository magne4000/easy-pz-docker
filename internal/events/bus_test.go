package events

import (
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEventTypeMapCoversTopics(t *testing.T) {
	m := EventTypeMap()
	require.Len(t, m, len(Topics()))
	for _, tp := range Topics() {
		ev, ok := m[string(tp)]
		require.True(t, ok, "missing %s", tp)
		require.Equal(t, tp, TopicOf(ev.(Event)))
	}
}

func TestFanOut(t *testing.T) {
	b := NewBus(slog.Default(), 64)
	const subs, n = 5, 20
	var wg sync.WaitGroup
	for range subs {
		ch, cancel := b.Subscribe()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			for i := range n {
				ev := <-ch
				require.Equal(t, ConsoleLine{Seq: uint64(i)}, ev)
			}
		}()
	}
	for i := range n {
		b.Publish(ConsoleLine{Seq: uint64(i)})
	}
	wg.Wait()
	require.Equal(t, 0, b.Stats().Subscribers)
}

func TestSlowSubscriberDropsAndDesyncs(t *testing.T) {
	b := NewBus(slog.Default(), 4)
	ch, cancel := b.Subscribe()
	defer cancel()
	done := make(chan struct{})
	go func() {
		for range 100 {
			b.Publish(ModsChanged{})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}
	require.Positive(t, b.Stats().Dropped)
	var got []Event
	for range 4 {
		got = append(got, <-ch)
	}
	require.IsType(t, StreamDesync{}, got[len(got)-1])
}

func TestCancelIdempotent(t *testing.T) {
	b := NewBus(slog.Default(), 4)
	ch, cancel := b.Subscribe()
	require.Equal(t, 1, b.Stats().Subscribers)
	cancel()
	cancel()
	_, open := <-ch
	require.False(t, open)
	require.Equal(t, 0, b.Stats().Subscribers)
	b.Publish(ModsChanged{})
}
