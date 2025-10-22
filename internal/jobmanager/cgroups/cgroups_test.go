//go:build !e2e

package cgroups_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nixpig/jobworker/internal/jobmanager/cgroups"
)

func TestCgroups(t *testing.T) {
	t.Parallel()

	t.Run("Test lifecycle with limits", func(t *testing.T) {
		t.Parallel()

		limits := &cgroups.ResourceLimits{
			CPUMaxPercent:  50,
			MemoryMaxBytes: 536870912,
			IOMaxBPS:       10485760,
		}

		cgroup, err := cgroups.CreateCgroup(
			"test-job",
			limits,
		)
		if err != nil {
			t.Fatalf("expected not to receive error: got '%v'", err)
		}

		// NOTE: Assuming mounted at /sys/fs/cgroup
		wantPath := "/sys/fs/cgroup/test-job"
		if cgroup.Path() != wantPath {
			t.Errorf(
				"expected cgroup path: got '%s', want '%s'",
				cgroup.Path(),
				wantPath,
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

		if err := cgroup.Destroy(); err != nil {
			t.Errorf("expected not to receive error: got '%v'", err)
		}

		if _, err := os.Stat(cgroup.Path()); !os.IsNotExist(err) {
			t.Errorf("expected cgroup path to be removed: got '%v'", err)
		}
	})
}
