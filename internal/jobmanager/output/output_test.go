package output_test

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

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

				jobCh := make(chan struct{})

				if !config.lateSub {
					close(jobCh)
				}

				s := output.NewStreamer(
					io.NopCloser(bytes.NewReader(config.payload)),
					jobCh,
				)

				if config.lateSub {
					close(jobCh)
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
							errCh <- fmt.Errorf("expected read all not to return error: got '%v'", err)
						}

						if string(got) != string(config.payload) {
							errCh <- fmt.Errorf(
								"expected stream data to match: got '%s', want '%s'",
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

		jobCh := make(chan struct{})

		s := output.NewStreamer(pr, jobCh)

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
					errCh <- fmt.Errorf("expected read all not to return error: got '%v'", err)
				}

				if string(got) != wantData {
					errCh <- fmt.Errorf(
						"expected stream data to match: got '%s', want '%s'",
						string(got),
						wantData,
					)
				}
			})
		}

		writerWg.Wait()
		pw.Close()
		close(jobCh)
		readerWg.Wait()

		close(errCh)

		for err := range errCh {
			t.Error(err)
		}
	})

	t.Run("Test read from closed sub", func(t *testing.T) {
		t.Parallel()

		pr, pw := io.Pipe()

		jobCh := make(chan struct{})

		s := output.NewStreamer(pr, jobCh)

		sub := s.Subscribe()

		// Close immediately.
		sub.Close()
		close(jobCh)

		// Read after closed.
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

		jobCh := make(chan struct{})

		s := output.NewStreamer(
			io.NopCloser(strings.NewReader("Hello, world!")),
			jobCh,
		)

		sub := s.Subscribe()

		if err := sub.Close(); err != nil {
			t.Errorf("expected close sub not to return error: got '%v'", err)
		}

		if err := sub.Close(); err != io.ErrClosedPipe {
			t.Errorf(
				"expected sub close error to be ErrClosedPipe: got '%v'",
				err,
			)
		}

		close(jobCh)
	})

	t.Run("Test concurrent access of single sub (race)", func(t *testing.T) {
		t.Parallel()

		jobCh := make(chan struct{})

		s := output.NewStreamer(
			io.NopCloser(strings.NewReader("Hello, world!")),
			jobCh,
		)

		sub := s.Subscribe()
		defer sub.Close()

		var wg sync.WaitGroup

		wg.Go(func() {
			sub.Read([]byte{})
		})

		wg.Go(func() {
			sub.Close()
			close(jobCh)
		})

		wg.Wait()
	})

	t.Run("Test pipe closes before job completes", func(t *testing.T) {
		t.Parallel()

		pr, pw := io.Pipe()

		jobCh := make(chan struct{})

		s := output.NewStreamer(pr, jobCh)

		payload := []byte("Hello, world!")

		pw.Write(payload)
		pw.Close()

		sub := s.Subscribe()
		defer sub.Close()

		readCh := make(chan []byte)
		errCh := make(chan error, 1)

		go func() {
			got, err := io.ReadAll(sub)
			if err != nil {
				errCh <- fmt.Errorf("expected read not to return error: got '%v'", err)
				return
			}

			readCh <- got
		}()

		select {
		case <-readCh:
			t.Errorf("expected read not to return before job end")
		case err := <-errCh:
			t.Errorf("expected read not to return error: got '%v'", err)
		case <-time.After(50 * time.Millisecond):
			// Wait until blocked.
		}

		close(jobCh)

		select {
		case got := <-readCh:
			if string(got) != string(payload) {
				t.Errorf(
					"expected read data to match: got '%s', want '%s'",
					string(got),
					payload,
				)
			}
		case err := <-errCh:
			t.Errorf("expected read not to return error: '%v'", err)
		case <-time.After(200 * time.Millisecond):
			t.Errorf("expected read to extend to lifetime of job")
		}
	})

	t.Run("Test job completes before pipe closes", func(t *testing.T) {
		t.Parallel()

		pr, pw := io.Pipe()

		jobCh := make(chan struct{})

		s := output.NewStreamer(pr, jobCh)

		payload := []byte("Hello, world!")

		pw.Write(payload)

		close(jobCh)

		select {
		case <-s.Done():
		// Finished.
		case <-time.After(200 * time.Millisecond):
			t.Errorf("expected pipe to close once job completes")
		}

		pw.Close()

		sub := s.Subscribe()
		defer sub.Close()

		got, err := io.ReadAll(sub)
		if err != nil {
			t.Errorf("expected read not to return error: got '%v'", err)
		}

		if string(got) != string(payload) {
			t.Errorf(
				"expected data to match: got '%s', want '%s'",
				string(got),
				payload,
			)
		}

	})
}
