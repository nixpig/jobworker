package output

import (
	"io"
	"sync/atomic"
)

// reader is used for reading data from a Streamer, internally
// managing its position in the buffer and reading new data as it arrives.
// It implements the io.ReadCloser interface.
type reader struct {
	position int
	closed   atomic.Bool

	s *Streamer
}

// Read performs a blocking read of data from the Streamer buffer.
// When there's no more data left and there's no more coming, it returns an
// io.EOF error.
func (r *reader) Read(p []byte) (n int, err error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	// If we've read all data in the buffer but we're not finished, then wait...
	// Broadcast is called in the event of 'close' or 'more data available'.
	for r.position >= len(r.s.buffer) && !r.isFinished() {
		r.s.cond.Wait()
	}

	if r.isFinished() {
		return 0, io.EOF
	}

	n = copy(p, r.s.buffer[r.position:])

	r.position += n

	return n, nil
}

// Close is used by a client to 'unsubscribe'. It marks the reader as closed
// and notifies any waiting reads that they can stop waiting.
func (r *reader) Close() error {
	r.closed.Swap(true)

	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	r.s.cond.Broadcast()

	return nil
}

func (r *reader) isFinished() bool {
	// We're finished if the reader is closed or the Streamer is done and we've
	// read all the data.
	return r.closed.Load() || (r.s.isDone() && r.position >= len(r.s.buffer))
}
