package cgroups

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	cpuPeriodMicros = 100000
	procMountinfo   = "/proc/self/mountinfo"
)

type ResourceLimits struct {
	CPUMaxPercent  int64
	MemoryMaxBytes int64
	IOMaxBPS       int64
}

type Cgroup struct {
	name string
	path string
	fd   *os.File
}

func CreateCgroup(root, name string, limits *ResourceLimits) (*Cgroup, error) {
	cg := &Cgroup{
		name: name,
		path: filepath.Join(root, "jobmanager-"+name),
	}

	if err := os.MkdirAll(cg.path, 0755); err != nil {
		return nil, fmt.Errorf("make cgroup dir: %w", err)
	}

	if limits != nil {
		if err := cg.applyLimits(limits); err != nil {
			os.RemoveAll(cg.path)
			return nil, fmt.Errorf("apply cgroup limits: %w", err)
		}
	}

	if isRealCgroupRoot(root) {
		fd, err := os.Open(cg.path)
		if err != nil {
			os.RemoveAll(cg.path)
			return nil, fmt.Errorf("open cgroup dir: %w", err)
		}

		cg.fd = fd
	}

	return cg, nil
}

func (c *Cgroup) applyLimits(limits *ResourceLimits) error {

	if limits.CPUMaxPercent > 0 {
		if err := c.setCPULimit(limits.CPUMaxPercent); err != nil {
			return fmt.Errorf("set CPU max limit: %w", err)
		}
	}

	if limits.MemoryMaxBytes > 0 {
		if err := c.setMemoryLimit(limits.MemoryMaxBytes); err != nil {
			return fmt.Errorf("set memory max limit: %w", err)
		}
	}

	if limits.IOMaxBPS > 0 {
		// if err := c.setIOLimit(limits.IOMaxBPS); err != nil {
		// 	return fmt.Errorf("set I/O max limit: %w", err)
		// }
		c.setIOLimit(limits.IOMaxBPS)
	}

	return nil
}

func (c *Cgroup) setCPULimit(percent int64) error {
	quota := (percent * cpuPeriodMicros) / 100
	cpuMaxPath := filepath.Join(c.path, "cpu.max")
	value := fmt.Sprintf("%d %d", quota, cpuPeriodMicros)

	if err := os.WriteFile(cpuMaxPath, []byte(value), 0644); err != nil {
		return fmt.Errorf("write cpu.max: %w", err)
	}

	return nil
}

func (c *Cgroup) setMemoryLimit(bytes int64) error {
	memoryMaxPath := filepath.Join(c.path, "memory.max")

	if err := os.WriteFile(memoryMaxPath, []byte(strconv.FormatInt(bytes, 10)), 0644); err != nil {
		return fmt.Errorf("write memory.max: %w", err)
	}

	return nil
}

func (c *Cgroup) setIOLimit(bps int64) error {
	deviceID, err := detectRootDevice()
	if err != nil {
		return fmt.Errorf("detect root device: %w", err)
	}

	ioMaxPath := filepath.Join(c.path, "io.max")
	value := fmt.Sprintf("%s rbps=%d wbps=%d", deviceID, bps, bps)

	if err := os.WriteFile(ioMaxPath, []byte(value), 0644); err != nil {
		return fmt.Errorf("write io.max: %w", err)
	}

	return nil
}

func (c *Cgroup) Join(pid int) error {
	procsPath := filepath.Join(c.path, "cgroup.procs")

	if err := os.WriteFile(
		procsPath,
		[]byte(strconv.Itoa(pid)),
		0644,
	); err != nil {
		return fmt.Errorf("add process to cgroup: %w", err)
	}

	return nil
}

func (c *Cgroup) Destroy() error {
	// Ignore error and just go ahead and remove.
	c.close()

	if err := os.RemoveAll(c.path); err != nil {
		return fmt.Errorf("remove cgroup: %w", err)
	}

	return nil
}

func (c *Cgroup) close() error {
	if c.fd != nil {
		err := c.fd.Close()

		c.fd = nil

		if err != nil {
			return fmt.Errorf("close cgroup fd: %w", err)
		}
	}

	return nil
}

func (c *Cgroup) FD() *os.File {
	return c.fd
}

func (c *Cgroup) Name() string {
	return c.name
}

func (c *Cgroup) Path() string {
	return c.path
}

func detectRootDevice() (string, error) {
	mountinfo, err := os.ReadFile(procMountinfo)
	if err != nil {
		return "", fmt.Errorf("read mountinfo: %w", err)
	}

	for line := range strings.SplitSeq(string(mountinfo), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		if fields[4] == "/" {
			return fields[2], nil
		}
	}

	return "", fmt.Errorf("detect root device in %s", procMountinfo)
}

func isRealCgroupRoot(root string) bool {
	return root == "/sys/fs/cgroup"
}

func ValidateCgroupRoot(root string) error {
	controllersPath := filepath.Join(root, "cgroup.controllers")
	if _, err := os.Stat(controllersPath); err != nil {
		return fmt.Errorf("cgroup root not valid at %s: %w", root, err)
	}
	return nil
}
