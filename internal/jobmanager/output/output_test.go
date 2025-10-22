package output_test

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/nixpig/jobworker/internal/jobmanager/output"
)

func TestOutputStreamer(t *testing.T) {
	t.Parallel()

	t.Run("Test basic scenarios", func(t *testing.T) {
		t.Parallel()

		scenarios := map[string]struct {
			payload []byte
			subs    int
			lateSub bool
		}{
			"Single subscriber": {
				payload: []byte("Hello, world!"),
				subs:    1,
				lateSub: false,
			},
			"Multiple subscribers": {
				payload: []byte("Hello, world!"),
				subs:    5,
				lateSub: false,
			},
			"Late subscriber": {
				payload: []byte("Hello, world!"),
				subs:    5,
				lateSub: true,
			},
			"Empty data": {
				payload: []byte(""),
				subs:    1,
				lateSub: false,
			},
			"Large data": {
				// Larger than initial buffer size of 4KB
				payload: bytes.Repeat([]byte("x"), 1024*1024),
				subs:    1,
				lateSub: false,
			},
		}

		for scenario, config := range scenarios {
			t.Run(scenario, func(t *testing.T) {
				t.Parallel()

				s := output.NewStreamer(
					io.NopCloser(bytes.NewReader(config.payload)),
				)

				if config.lateSub {
					<-s.Done()
				}

				errCh := make(chan error, config.subs)

				var wg sync.WaitGroup

				for range config.subs {
					wg.Go(func() {
						sub := s.Subscribe()
						defer sub.Close()

						got, err := io.ReadAll(sub)

						if err != nil {
							errCh <- fmt.Errorf("expected not to receive error: got '%v'", err)
						}

						if string(got) != string(config.payload) {
							errCh <- fmt.Errorf(
								"expected data to match: got '%s', want '%s'",
								string(got),
								config.payload,
							)
						}
					})
				}

				wg.Wait()

				close(errCh)

				for err := range errCh {
					t.Error(err)
				}
			})
		}
	})

	t.Run("Test concurrent writes", func(t *testing.T) {
		t.Parallel()

		writes := 1000
		subs := 100
		payload := []byte("Hello, world!")

		wantData := strings.Repeat(string(payload), 1000)

		pr, pw := io.Pipe()

		s := output.NewStreamer(pr)

		errCh := make(chan error, subs)

		var writerWg sync.WaitGroup

		for range writes {
			writerWg.Go(func() {
				pw.Write(payload)
			})
		}

		var readerWg sync.WaitGroup

		for range subs {
			readerWg.Go(func() {
				sub := s.Subscribe()
				defer sub.Close()

				got, err := io.ReadAll(sub)

				if err != nil {
					errCh <- fmt.Errorf("expected not to receive error: got '%v'", err)
				}

				if string(got) != wantData {
					errCh <- fmt.Errorf(
						"expected data to match: got '%s', want '%s'",
						string(got),
						wantData,
					)
				}
			})
		}

		writerWg.Wait()
		pw.Close()
		readerWg.Wait()

		close(errCh)

		for err := range errCh {
			t.Error(err)
		}
	})

	t.Run("Test read from closed sub", func(t *testing.T) {
		t.Parallel()

		pr, pw := io.Pipe()

		s := output.NewStreamer(pr)

		sub := s.Subscribe()

		// Close immediately.
		sub.Close()

		// Read _after_ closed.
		n, err := sub.Read([]byte{})

		if n != 0 {
			t.Errorf("expected to read zero bytes: got '%d'", n)
		}

		if err != io.EOF {
			t.Errorf("expected error to be EOF: got '%v'", err)
		}

		pw.Close()
	})

	t.Run("Test closing a closed sub", func(t *testing.T) {
		t.Parallel()

		s := output.NewStreamer(
			io.NopCloser(strings.NewReader("Hello, world!")),
		)

		sub := s.Subscribe()

		if err := sub.Close(); err != nil {
			t.Errorf("expected not to receive error: got '%v'", err)
		}

		if err := sub.Close(); err != io.ErrClosedPipe {
			t.Errorf("expected error to be ErrClosedPipe: got '%v'", err)
		}
	})

	t.Run("Test concurrent access of single sub", func(t *testing.T) {
		t.Parallel()

		s := output.NewStreamer(
			io.NopCloser(strings.NewReader("Hello, world!")),
		)
		sub := s.Subscribe()
		defer sub.Close()

		var wg sync.WaitGroup

		wg.Go(func() {
			sub.Read([]byte{})
		})

		wg.Go(func() {
			sub.Close()
		})

		wg.Wait()
	})
}
