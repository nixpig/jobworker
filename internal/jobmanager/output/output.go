// Package output provides concurrent streaming of process output. Multiple
// clients can subscribe to a Streamer and each receive the complete output
// from the beginning.
package output

import (
	"io"
	"sync"
)

const (
	// initialBufferCapacity is the starting size for the output buffer.
	// 4KB seems like a reasonable default.
	// TODO: Observe and tune based on typical job output.
	initialBufferCapacity = 4096

	// readBufferSize is the temporary buffer size for reading from source pipe.
	// 4KB aligns with typical pipe buffer sizes.
	readBufferSize = 4096
)

// Streamer is responsible for processing job output by reading from a
// source io.ReadCloser and storing the data in an internal buffer for use by a
// reader. The internal buffer grows indefinitely to accommodate new output.
type Streamer struct {
	// NOTE: the buffer size will grow indefinitely with no upper bound. The
	// assumption for this is that 'everything will fit in memory'.
	// In a production system, we'd need to look at alternative strategies, such
	// as flushing the buffer to disk and reconstructing the segments for new
	// clients on subscription.
	buffer []byte

	done chan struct{}
	mu   sync.Mutex
	cond sync.Cond
}

// NewStreamer creates a Streamer that reads from source and immediately begins
// processing. It continues processing until it recieves an io.EOF error from
// source.
func NewStreamer(source io.ReadCloser) *Streamer {
	s := &Streamer{
		buffer: make([]byte, 0, initialBufferCapacity),
		done:   make(chan struct{}),
	}

	s.cond.L = &s.mu

	// TODO: A nicer API might be to return a Streamer and give the consumer the
	// responsibility of initiating processing and have them decide whether they
	// want to do that concurrently (in a goroutine). For this current use case,
	// I think handling that here in the Streamer is fine and simplifies things
	// slightly.
	go s.processOutput(source)

	return s
}

func (s *Streamer) processOutput(source io.ReadCloser) {
	defer func() {
		close(s.done)
		source.Close()

		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	}()

	// TODO: If the service is handling many short-running processes, it might
	// be beneficial to use a sync.Pool of byte buffers. Perhaps profile in
	// future.
	buffer := make([]byte, readBufferSize)

	for {
		n, err := source.Read(buffer)
		if n > 0 {
			s.mu.Lock()

			s.buffer = append(s.buffer, buffer[:n]...)

			s.cond.Broadcast()

			s.mu.Unlock()
		}

		if err != nil {
			if err == io.EOF {
				return
			}

			// TODO: Review whether to do anything with non-EOF read errors. For now,
			// just returning and letting the stream end seems okay. In a production
			// system we'd want to at least log them.
			return
		}
	}
}

// Subscribe returns a io.ReadCloser for reading data from the Streamer.
// Close cancels the subscription.
func (s *Streamer) Subscribe() io.ReadCloser {
	return &reader{s: s}
}

// Done returns a channel that is closed when processing has finished, i.e. the
// source io.ReadCloser is closed.
func (s *Streamer) Done() <-chan struct{} {
	return s.done
}

func (s *Streamer) isDone() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}
