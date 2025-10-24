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
	"time"
)

// TODO: Make emptyTimeout and emptyInterval configurable.
const (
	cpuPeriodMicros   = 100000
	procSelfMountinfo = "/proc/self/mountinfo"
	sysDevBlock       = "/sys/dev/block"

	// emptyTimeout is the timeout for waiting for the cgroup to empty.
	emptyTimeout = 10 * time.Second
	// emptyInterval is the interval between checking for the cgroup to be empty.
	emptyInterval = 50 * time.Millisecond
)

var (
	cgroupRoot   string
	rootDeviceID string

	initGetCgroupRoot    sync.Once
	initDetectRootDevice sync.Once
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

	mu sync.Mutex
}

// CreateCgroup creates a cgroup with the given name and limits. It returns a
// Cgroup with a file descriptor for atomic placement using
// SysProcAttr.CgroupFD.
func CreateCgroup(name string, limits *ResourceLimits) (c *Cgroup, err error) {
	// TODO: Add validation against things like path traversal. Since this is
	// only used by the Job package and we know it always passes a UUID (and not
	// arbitrary strings) just checking for empty string is safe for prototype.
	if name == "" {
		return nil, errors.New("name cannot be empty")
	}

	root, err := getCgroupRoot()
	if err != nil {
		return nil, fmt.Errorf("get cgroup root: %w", err)
	}

	c = &Cgroup{
		name: name,
		path: filepath.Join(root, name),
	}

	if err := os.Mkdir(c.path, 0755); err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("cgroup already exists: %w", err)
		}

		return nil, fmt.Errorf("make cgroup dir: %w", err)
	}

	defer func(cgroupPath string) {
		if err != nil {
			os.RemoveAll(cgroupPath)
		}
	}(c.path)

	if limits != nil {
		if err := c.applyLimits(limits); err != nil {
			return nil, fmt.Errorf("apply cgroup limits: %w", err)
		}
	}

	return c, nil
}

// TODO: Add validation for limits, e.g. CPUMaxPercent >= 0 and <=100.
// Values are currently hard-coded in server.go, so omitting validation for
// the prototype.
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

// Kill kills all processes in the cgroup by writing to `cgroup.kill` (if it
// exists), waits for the cgroup to be empty, then deletes the cgroup
// directory.
func (c *Cgroup) Kill() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := os.WriteFile(
		filepath.Join(c.path, "cgroup.kill"),
		[]byte("1"),
		0644,
	); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("write cgroup kill: %w", err)
	}

	if err := c.waitForEmpty(emptyTimeout, emptyInterval); err != nil {
		return fmt.Errorf("wait for empty cgroup: %w", err)
	}

	if err := os.RemoveAll(c.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove cgroup: %w", err)
	}

	return nil
}

// FD opens the cgroup path and returns the file descriptor used for atomic
// placement using SysProcAttr.CgroupFD.
func (c *Cgroup) FD() (*os.File, error) {
	return os.Open(c.path)
}

func (c *Cgroup) Name() string {
	return c.name
}

func (c *Cgroup) Path() string {
	return c.path
}

// waitForEmpty waits for the cgroup to be empty by polling at a fixed interval
// until the given timeout is reached. When the cgroup is empty then nil is
// returned, or if the timeout is reached then an error.
func (c *Cgroup) waitForEmpty(
	timeout time.Duration,
	interval time.Duration,
) error {
	deadline := time.Now().Add(timeout)

	for {
		populated, err := c.isPopulated()
		if err != nil {
			return fmt.Errorf("check if cgroup populated: %w", err)
		}

		if !populated {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf(
				"timed out after %s waiting for empty cgroup: %s",
				timeout,
				c.path,
			)
		}

		time.Sleep(interval)
	}
}

// isPopulated checks if there are processes in the cgroup by reading the
// `populated` field from the cgroup.events file.
func (c *Cgroup) isPopulated() (bool, error) {
	eventsData, err := os.ReadFile(filepath.Join(c.path, "cgroup.events"))
	if err != nil {
		if !os.IsNotExist(err) {
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

// detectRootDevice detects the root device and caches the result. Subsequent
// calls return the cached result.
func detectRootDevice() (_ string, err error) {
	initDetectRootDevice.Do(func() {
		var deviceID string
		deviceID, err = getRootDeviceID()
		if err != nil {
			return
		}

		partitionPath := filepath.Join(sysDevBlock, deviceID, "partition")

		if _, err = os.Stat(partitionPath); err == nil {
			// Using fmt.Sprintf rather than filepath.Join() so that resolution
			// of `..` is handled by kernel instead of Go.
			parentDevicePath := fmt.Sprintf(
				"%s/%s/../dev",
				sysDevBlock,
				deviceID,
			)

			var parentDeviceID []byte
			parentDeviceID, err = os.ReadFile(parentDevicePath)
			if err != nil {
				return
			}

			rootDeviceID = string(bytes.TrimSpace(parentDeviceID))

			return
		}

		rootDeviceID = deviceID

		return
	})

	return rootDeviceID, err
}

// getRootDeviceID returns the device ID of the root filesystem in
// 'major:minor' format.
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

// getCgroupRoot determines the root cgroup for cgroups v2 by finding the first
// cgroup2 entry in /proc/self/mountinfo and caches the result. Subsequent
// calls return the cached result.
func getCgroupRoot() (_ string, err error) {
	initGetCgroupRoot.Do(func() {
		var mountinfo []byte
		mountinfo, err = os.ReadFile(procSelfMountinfo)
		if err != nil {
			return
		}

		for line := range strings.SplitSeq(string(mountinfo), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 9 {
				continue
			}

			if fields[8] == "cgroup2" {
				cgroupRoot = fields[4]
				break
			}
		}
	})

	return cgroupRoot, err
}
