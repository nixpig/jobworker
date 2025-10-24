//go:build e2e

package e2e_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nixpig/jobworker/certs"
)

type testEnv struct {
	binDir     string
	certDir    string
	serverCmd  *exec.Cmd
	cliPath    string
	serverPath string
}

// NOTE: Relative paths are used to determine the source locations to build
// the CLI and server binaries. Running this test from anywhere that breaks
// those relative paths will not work.
func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	env := &testEnv{
		binDir:  t.TempDir(),
		certDir: t.TempDir(),
	}

	env.serverPath = filepath.Join(env.binDir, "jobserver")

	buildServer := exec.Command(
		"go",
		"build",
		"-o",
		env.serverPath,
		"../cmd/jobserver",
	)

	if output, err := buildServer.CombinedOutput(); err != nil {
		t.Fatalf(
			"failed to build server binary: '%v' (output: '%s')",
			err,
			output,
		)
	}

	env.cliPath = filepath.Join(env.binDir, "jobctl")

	buildCLI := exec.Command("go", "build", "-o", env.cliPath, "../cmd/jobctl")

	if output, err := buildCLI.CombinedOutput(); err != nil {
		t.Fatalf("failed to build CLI binary: '%v' (output: '%s')", err, output)
	}

	certFiles := []string{
		"ca.crt",
		"server.crt",
		"server.key",
		"client-operator.crt",
		"client-operator.key",
	}

	for _, filename := range certFiles {
		data, err := certs.FS.ReadFile(filename)
		if err != nil {
			t.Fatalf("read cert %s: %v", filename, err)
		}

		path := filepath.Join(env.certDir, filename)
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("save cert '%s': '%v'", filename, err)
		}
	}

	env.serverCmd = exec.Command(
		env.serverPath,
		"--port", "8443",
		"--cert-path", filepath.Join(env.certDir, "server.crt"),
		"--key-path", filepath.Join(env.certDir, "server.key"),
		"--ca-cert-path", filepath.Join(env.certDir, "ca.crt"),
	)

	if err := env.serverCmd.Start(); err != nil {
		t.Fatalf("failed to exec server command: '%v'", err)
	}

	t.Cleanup(func() {
		if env.serverCmd.Process != nil {
			env.serverCmd.Process.Kill()
			env.serverCmd.Wait()
		}
	})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("failed to start server")
		case <-ticker.C:
			if _, _, err := env.runCLI(t, "start", "echo", "started"); err == nil {
				return env
			}
		}
	}
}

func (env *testEnv) runCLI(
	t *testing.T,
	args ...string,
) (string, string, error) {
	t.Helper()

	cliArgs := []string{
		"--server-hostname", "localhost",
		"--server-port", "8443",
		"--cert-path", filepath.Join(env.certDir, "client-operator.crt"),
		"--key-path", filepath.Join(env.certDir, "client-operator.key"),
		"--ca-cert-path", filepath.Join(env.certDir, "ca.crt"),
	}

	cliArgs = append(cliArgs, args...)

	cmd := exec.Command(env.cliPath, cliArgs...)

	var stdout strings.Builder
	var stderr strings.Builder

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	return stdout.String(), stderr.String(), err
}

// TODO: For a production solution, we might consider a more comprehensive E2E
// test suite. For this prototype, a quick smoke test to verify CLI is able to
// communicate with the server and the available commands run should suffice.
func TestBasicE2E(t *testing.T) {
	env := setupTestEnv(t)

	t.Run("Test job lifecycle", func(t *testing.T) {
		startStdout, _, err := env.runCLI(t, "start", "echo", "Hello, world!")
		if err != nil {
			t.Fatalf("expected start not to return error: got '%v'", err)
		}

		jobID := strings.TrimSpace(startStdout)
		if _, err := uuid.Parse(jobID); err != nil {
			t.Errorf("expected start to return UUID: got '%v'", err)
		}

		statusStdout, _, err := env.runCLI(t, "status", jobID)
		if err != nil {
			t.Errorf("expected status not to return error: got '%v'", err)
		}

		// TODO: If we built out this E2E test suite, we'd add a 'waitForStatus'
		// helper function (like in job_server.go). But for the purposes of a quick
		// smoke test, this sleep should be fine.
		time.Sleep(100 * time.Millisecond)

		if !strings.Contains(statusStdout, "Stopped") {
			t.Errorf(
				"expected job state: got '%s', want 'Stopped'",
				statusStdout,
			)
		}

		streamStdout, _, err := env.runCLI(t, "stream", jobID)
		if err != nil {
			t.Errorf("expected stream not to return error: got '%v'", err)
		}

		if !strings.Contains(streamStdout, "Hello, world!") {
			t.Errorf(
				"expected stream text: got '%s', want 'Hello, world!'",
				streamStdout,
			)
		}

		_, stopStderr, err := env.runCLI(t, "stop", jobID)
		if err == nil {
			t.Error("expected stop to return error")
		}
		if !strings.Contains(
			stopStderr,
			"Error: cannot go from Stopped to Stopping",
		) {
			t.Errorf("expected error message: got '%s'", stopStderr)
		}
	})
}
