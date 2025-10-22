// Package cgroups provides utilities for managing cgroups v2 limits for CPU,
// memory and disk I/O.
package cgroups

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	cpuPeriodMicros   = 100000
	procSelfMountinfo = "/proc/self/mountinfo"
)

// ResourceLimits are the limits applied to a cgroup. Zero (0) for any resource
// means no limit will be applied for that resource.
type ResourceLimits struct {
	// CPUMaxPercent limits CPU usage to the given percentage, e.g. 50 = 50%.
	CPUMaxPercent int64
	// MemoryMaxBytes limits memory usage by the given bytes.
	MemoryMaxBytes int64
	// IOMaxBPS limits disk I/O in by the given bytes per second.
	IOMaxBPS int64
}

// Cgroup represents a control group for limiting resources on a process.
type Cgroup struct {
	name string
	path string
	fd   *os.File

	mu sync.Mutex
}

// CreateCgroup creates a new cgroup at the given root path with the given name
// and limits. It returns a Cgroup with a file descriptor for atomic placement
// using SysProcAttr.CgroupFD.
func CreateCgroup(name string, limits *ResourceLimits) (cg *Cgroup, err error) {
	root, err := getCgroupRoot()
	if err != nil {
		return nil, fmt.Errorf("get cgroup root: %w", err)
	}

	if err := validateCgroupRoot(root); err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			os.RemoveAll(cg.path)
		}
	}()

	cg = &Cgroup{
		name: name,
		path: filepath.Join(root, name),
	}

	if err := os.MkdirAll(cg.path, 0755); err != nil {
		return nil, fmt.Errorf("make cgroup dir: %w", err)
	}

	if limits != nil {
		// TODO: Validate the limits provided, e.g. CPUMaxPercent >= 0 and <=100.
		// Values are currently hard-coded in server.go, so omitting validation for
		// the prototype.

		if err := cg.applyLimits(limits); err != nil {
			return nil, fmt.Errorf("apply cgroup limits: %w", err)
		}
	}

	fd, err := os.Open(cg.path)
	if err != nil {
		return nil, fmt.Errorf("open cgroup dir: %w", err)
	}

	cg.fd = fd

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
		if err := c.setIOLimit(limits.IOMaxBPS); err != nil {
			return fmt.Errorf("set I/O max limit: %w", err)
		}
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
		return fmt.Errorf("write io.max (device: %s): %w", deviceID, err)
	}

	return nil
}

// closeFD closes the file descriptor of the cgroup.
func (c *Cgroup) closeFD() error {
	if c.fd != nil {
		err := c.fd.Close()

		c.fd = nil

		if err != nil {
			return fmt.Errorf("close cgroup fd: %w", err)
		}
	}

	return nil
}

// CloseFD provides synchronised access to close the file descriptor.
func (c *Cgroup) CloseFD() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.closeFD()
}

// Kill kills all processes in the cgroup by writing to `cgroup.kill`.
func (c *Cgroup) Kill() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := os.WriteFile(
		filepath.Join(c.path, "cgroup.kill"),
		[]byte("1"),
		0644,
	); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

// Destroy attempts to close the cgroup file descriptor then removes the cgroup
// directory.
func (c *Cgroup) Destroy() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Ignore close error and just go ahead and remove. Log in future if/when
	// observability comes into scope.
	c.closeFD()

	if err := os.RemoveAll(c.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove cgroup: %w", err)
	}

	return nil
}

// FD returns the cgroup file descriptor used for atomic placement using
// SysProcAttr.CgroupFD.
func (c *Cgroup) FD() *os.File {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.fd
}

func (c *Cgroup) Name() string {
	return c.name
}

func (c *Cgroup) Path() string {
	return c.path
}

func detectRootDevice() (string, error) {
	deviceID, err := getRootDeviceID()
	if err != nil {
		return "", err
	}

	devicePath, err := getDevicePath(deviceID)
	if err != nil {
		return "", err
	}

	if isPartition(devicePath) {
		return getDeviceID(filepath.Dir(devicePath))
	}

	return deviceID, nil
}

// getRootDeviceID returns device ID in 'major:minor' format for the root
// filesystem.
func getRootDeviceID() (string, error) {
	mountinfo, err := os.ReadFile(procSelfMountinfo)
	if err != nil {
		return "", fmt.Errorf("read mountinfo: %w", err)
	}

	for line := range strings.SplitSeq(string(mountinfo), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		mountPoint := fields[4]
		deviceID := fields[2]

		if mountPoint == "/" {
			return deviceID, nil
		}
	}

	return "", fmt.Errorf("root device not found in %s", procSelfMountinfo)
}

func getDevicePath(deviceID string) (string, error) {
	if !strings.Contains(deviceID, ":") {
		return "", fmt.Errorf("invalid deviceID: %s", deviceID)
	}

	sysPath := filepath.Join("/sys/dev/block", deviceID)
	realPath, err := filepath.EvalSymlinks(sysPath)
	if err != nil {
		return "", fmt.Errorf("resolve device symlinks: %w", err)
	}

	return realPath, nil
}

func isPartition(devicePath string) bool {
	partitionFile := filepath.Join(devicePath, "partition")
	_, err := os.Stat(partitionFile)

	return err == nil
}

func getDeviceID(devicePath string) (string, error) {
	devData, err := os.ReadFile(filepath.Join(devicePath, "dev"))
	if err != nil {
		return "", fmt.Errorf("read device id from %s: %w", devicePath, err)
	}

	return string(bytes.TrimSpace(devData)), nil
}

// getCgroupRoot determines the root cgroup for cgroups v2 by finding the first
// cgroup2 entry in /proc/self/mountinfo.
func getCgroupRoot() (string, error) {
	mountinfo, err := os.ReadFile(procSelfMountinfo)
	if err != nil {
		return "", fmt.Errorf("read mountinfo: %w", err)
	}

	for line := range strings.SplitSeq(string(mountinfo), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}

		fs := fields[8]

		if fs == "cgroup2" {
			mountPoint := fields[4]
			return mountPoint, nil
		}
	}

	return "", errors.New("cgroup2 mount point not found")
}

// validateCgroupRoot checks if the provided cgroupRoot is a vlaid cgroup v2
// root by confirming the presence of a cgroup.controllers file.
func validateCgroupRoot(cgroupRoot string) error {
	controllersPath := filepath.Join(cgroupRoot, "cgroup.controllers")

	if _, err := os.Stat(controllersPath); err != nil {
		return fmt.Errorf("cgroup root not valid at %s: %w", cgroupRoot, err)
	}

	return nil
}
