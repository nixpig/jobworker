package output

import (
	"io"
	"sync"
)

const (
	// initialBufferCapacity is the starting size for the output buffer.
	// 4KB is a reasonable default
	// TODO: Observe and tune based on typical job output.
	initialBufferCapacity = 4096

	// readBufferSize is the temporary buffer size for reading from source pipe.
	// 4KB aligns with typical pipe buffer sizes.
	readBufferSize = 4096
)

// Manager is responsible for processing job output by reading from a
// source io.ReadCloser and storing the data in an internal buffer for use by a
// reader. The internal buffer will grow indefinitely to accommodate new job
// output.
type Manager struct {
	// NOTE: the buffer size will grow indefinitely with no upper bound. The
	// assumption for this is that 'everything will fit in memory'.
	// In a production system, we'd need to look at alternative strategies, such
	// as flushing the buffer to disk and reconstructing the segments for new
	// subs.
	buffer []byte

	done chan struct{}
	mu   sync.Mutex
	cond *sync.Cond
}

// NewManager creates a Manager that reads from source and immediately begins
// processing. It continues processing until it recieves an io.EOF error from
// source.
func NewManager(source io.ReadCloser) *Manager {
	m := &Manager{
		buffer: make([]byte, 0, initialBufferCapacity),
		done:   make(chan struct{}),
	}

	m.cond = sync.NewCond(&m.mu)

	// TODO: Review whether really want to start processing immediately or
	// whether the API would be nicer to have the consumer initiate the
	// processing. E.g.
	//	m := NewManager(source)
	//	go m.Start()
	go m.processOutput(source)

	return m
}

func (m *Manager) processOutput(source io.ReadCloser) {
	defer func() {
		close(m.done)

		source.Close()

		m.mu.Lock()
		m.cond.Broadcast()
		m.mu.Unlock()
	}()

	// TODO: If the service is handling many short-running processes, it might
	// be beneficial to use a sync.Pool of byte buffers. Requires profiling.
	buffer := make([]byte, readBufferSize)

	for {
		n, err := source.Read(buffer)
		if n > 0 {
			m.mu.Lock()

			m.buffer = append(m.buffer, buffer[:n]...)

			m.cond.Broadcast()

			m.mu.Unlock()
		}

		if err != nil {
			if err == io.EOF {
				return
			}

			// TODO: Review whether to do anything with other read errors. For now,
			// just returning and letting the stream end seems okay.
			return
		}
	}
}

// Subscribe returns a io.ReadCloser for reading data from the Manager.
// Closing this will 'cancel the subscription'.
func (m *Manager) Subscribe() io.ReadCloser {
	return &reader{m: m}
}

// Done returns a channel that is closed when processing has finished, i.e. the
// source io.ReadCloser is closed.
func (m *Manager) Done() <-chan struct{} {
	return m.done
}

func (m *Manager) isDone() bool {
	select {
	case <-m.done:
		return true
	default:
		return false
	}
}
