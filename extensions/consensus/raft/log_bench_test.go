package raft

import (
	"strconv"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

func benchLog(b *testing.B, size int) *Log {
	b.Helper()

	l, err := NewLog(forge.NewNoopLogger(), newMemStorage(b), 1024)
	if err != nil {
		b.Fatal(err)
	}

	for i := 1; i <= size; i++ {
		if err := l.Append(internal.LogEntry{Index: uint64(i), Term: 1, Data: make([]byte, 64)}); err != nil {
			b.Fatal(err)
		}
	}

	return l
}

// A heartbeat to an up-to-date follower reads the previous entry and the
// (empty) tail after it.
func BenchmarkLogHeartbeatRead(b *testing.B) {
	for _, size := range []int{1000, 10000, 100000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			l := benchLog(b, size)

			b.ResetTimer()

			for b.Loop() {
				_, _ = l.Get(uint64(size))
				_, _ = l.GetEntriesAfter(uint64(size), 64)
			}
		})
	}
}

// A lagging follower needs entries far behind the newest ones.
func BenchmarkLogLaggingFollowerRead(b *testing.B) {
	l := benchLog(b, 100000)

	b.ResetTimer()

	i := 0
	for b.Loop() {
		prev := uint64(50000 + i%1000)
		_, _ = l.Get(prev)
		_, _ = l.GetEntriesAfter(prev, 64)
		i++
	}
}

// Appending once the log is larger than the configured cache size.
func BenchmarkLogAppend(b *testing.B) {
	l := benchLog(b, 0)

	b.ReportAllocs()
	b.ResetTimer()

	i := uint64(0)
	for b.Loop() {
		i++
		_ = l.Append(internal.LogEntry{Index: i, Term: 1, Data: make([]byte, 64)})
	}
}
