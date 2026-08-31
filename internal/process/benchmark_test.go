package process

import (
	"testing"
	"time"
)

func BenchmarkPruneCompletedProcessSnapshots(b *testing.B) {
	manager := NewManager()
	manager.completedRetention = 256
	for index := 0; index < 1024; index++ {
		value := testManagedProcess(ID("completed"+string(rune(index))), retentionWriteCloser{})
		value.state = StateCompleted
		value.finishedAt = time.Unix(int64(index), 0)
		manager.processes[value.id] = value
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		manager.mutex.Lock()
		manager.pruneCompletedLocked()
		manager.mutex.Unlock()
		// Refill a small tail to exercise the same bounded retention path.
		manager.mutex.Lock()
		id := ID("new-" + time.Now().Format("150405.000000000"))
		manager.processes[id] = &managed{id: id, state: StateCompleted, finishedAt: time.Now(), done: make(chan struct{}), output: newTranscript(1)}
		manager.mutex.Unlock()
	}
}
