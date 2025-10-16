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

func TestOutputManager(t *testing.T) {
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
				om := output.NewManager(
					io.NopCloser(bytes.NewReader(config.payload)),
				)

				if config.lateSub {
					<-om.Done()
				}

				errCh := make(chan error, config.subs)

				var wg sync.WaitGroup

				for range config.subs {
					wg.Go(func() {
						sub := om.Subscribe()
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

		om := output.NewManager(pr)

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
				sub := om.Subscribe()
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

				sub.Close()
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
		pr, pw := io.Pipe()

		om := output.NewManager(pr)

		sub := om.Subscribe()

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

	t.Run("Test concurrent access of single sub", func(t *testing.T) {
		t.Parallel()

		om := output.NewManager(
			io.NopCloser(strings.NewReader("Hello, world!")),
		)
		sub := om.Subscribe()
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
