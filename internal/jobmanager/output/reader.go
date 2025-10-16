package output

import (
	"io"
	"sync/atomic"
)

// reader is used for reading data from a Manager, internally
// managing its position in the buffer and reading new data as it arrives.
// It implements the io.ReadCloser interface. Safe for concurrent use.
type reader struct {
	position int
	closed   atomic.Bool

	m *Manager
}

// Read performs a blocking read of data from the buffer of the Manager.
// When there's no more data left and there's no more coming, it returns an
// io.EOF error.
func (r *reader) Read(p []byte) (n int, err error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()

	// If we've read all data in the buffer but we're not finished, then wait...
	// Broadcast is called in the event of 'close' or 'more data available'.
	for r.position >= len(r.m.buffer) && !r.isFinished() {
		r.m.cond.Wait()
	}

	if r.isFinished() {
		return 0, io.EOF
	}

	availableData := len(r.m.buffer) - r.position
	amountToCopy := min(availableData, len(p))

	n = copy(p, r.m.buffer[r.position:r.position+amountToCopy])

	r.position += n

	return n, nil
}

// Close is used by a client to 'unsubscribe'. It marks the reader as closed
// and notifies any waiting reads that they can stop waiting.
func (r *reader) Close() error {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()

	r.closed.Store(true)

	r.m.cond.Broadcast()

	return nil
}

func (r *reader) isFinished() bool {
	// We're finished if the reader is closed or the Manager is done and we've
	// read all the data.
	return r.closed.Load() || (r.m.isDone() && r.position >= len(r.m.buffer))
}
