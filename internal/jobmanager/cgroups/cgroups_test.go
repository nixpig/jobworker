package cgroups_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nixpig/jobworker/internal/jobmanager/cgroups"
)

func cleanupCgroup(t *testing.T, path string) {
	t.Helper()

	if err := os.WriteFile(
		filepath.Join(path, "cgroup.kill"),
		[]byte("1"),
		0644,
	); err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to write to cgroup.kill")
	}

	for {
		populated, err := isCgroupPopulated(path)
		if err != nil {
			t.Fatalf("failed check if populated: %s", err)
		}

		if !populated {
			break
		}

		if time.Now().After(time.Now().Add(5 * time.Second)) {
			t.Fatalf("timed out waiting for empty cgroup: %s", path)
		}

		time.Sleep(100 * time.Millisecond)
	}

	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to remove cgroup: %s", err)
	}
}

func isCgroupPopulated(path string) (bool, error) {
	eventsData, err := os.ReadFile(filepath.Join(path, "cgroup.events"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, fmt.Errorf("read cgroup.events data: %w", err)
	}

	fields := strings.Fields(string(eventsData))
	for i, field := range fields {
		if field == "populated" && i+1 < len(fields) {
			return fields[i+1] == "1", nil
		}
	}

	return false, nil
}

func TestCgroups(t *testing.T) {
	t.Parallel()

	testCgroupName := "cgroup-lifecycle-test-job"
	testCgroupPath := "/sys/fs/cgroup/" + testCgroupName

	cleanupCgroup(t, testCgroupPath)

	t.Run("Test lifecycle with limits", func(t *testing.T) {
		t.Parallel()

		limits := &cgroups.ResourceLimits{
			CPUMaxPercent:  50,
			MemoryMaxBytes: 536870912,
			IOMaxBPS:       10485760,
		}

		cgroup, err := cgroups.CreateCgroup(
			testCgroupName,
			limits,
		)
		if err != nil {
			t.Fatalf("expected not to receive error: got '%v'", err)
		}

		if cgroup.Path() != testCgroupPath {
			t.Errorf(
				"expected cgroup path: got '%s', want '%s'",
				cgroup.Path(),
				testCgroupPath,
			)
		}

		if _, err := os.Stat(cgroup.Path()); err != nil {
			t.Errorf("expected cgroup path to be created: got '%v'", err)
		}

		cpuLimit, err := os.ReadFile(filepath.Join(cgroup.Path(), "cpu.max"))
		if err != nil {
			t.Errorf("expected not to receive error: got '%v'", err)
		}

		gotCPULimit := string(bytes.TrimSpace(cpuLimit))
		wantCPULimit := "50000 100000"
		if gotCPULimit != wantCPULimit {
			t.Errorf(
				"expected cpu.max: got '%s', want '%s'",
				gotCPULimit,
				wantCPULimit,
			)
		}

		memoryLimit, err := os.ReadFile(
			filepath.Join(cgroup.Path(), "memory.max"),
		)
		if err != nil {
			t.Errorf("expected not to receive error: got '%v'", err)
		}

		gotMemoryLimit := string(bytes.TrimSpace(memoryLimit))
		wantMemoryLimit := "536870912"
		if gotMemoryLimit != wantMemoryLimit {
			t.Errorf(
				"expected memory.max: got '%s', want '%s'",
				gotMemoryLimit,
				wantMemoryLimit,
			)
		}

		ioLimit, err := os.ReadFile(
			filepath.Join(cgroup.Path(), "io.max"),
		)
		if err != nil {
			t.Errorf("expected not to receive error: got '%v'", err)
		}

		gotIOLimit := string(bytes.TrimSpace(ioLimit))
		wantIOLimit := "rbps=10485760 wbps=10485760"
		if !strings.Contains(gotIOLimit, wantIOLimit) {
			t.Errorf(
				"expected io.max: got '%s', want '%s'",
				gotIOLimit, wantIOLimit,
			)
		}

		fd, err := cgroup.FD()
		if err != nil {
			t.Errorf("expected not to receive error: got '%v'", err)
		}
		defer fd.Close()

		sleepDuration := 5 * time.Second

		cmd := exec.Command("sleep", strconv.Itoa(int(sleepDuration)))
		cmd.SysProcAttr = &syscall.SysProcAttr{
			UseCgroupFD: true,
			CgroupFD:    int(fd.Fd()),
		}

		startTime := time.Now()

		if err := cmd.Start(); err != nil {
			t.Errorf("expected not to receive error: got '%v'", err)
		}

		var wg sync.WaitGroup

		if err := cgroup.Kill(); err != nil {
			t.Errorf("expected not to receive error: got '%v'", err)
		}

		wg.Wait()

		if time.Since(startTime) >= sleepDuration {
			t.Errorf(
				"expected kill and destroy to complete without waiting for process: took '%v'",
				time.Since(startTime),
			)
		}

		if _, err := os.Stat(cgroup.Path()); !os.IsNotExist(err) {
			t.Errorf("expected cgroup path to be removed: got '%v'", err)
		}
	})
}
