// Copyright 2022 The Parca Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package cgroup

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/prometheus/procfs"
	"golang.org/x/sys/unix"
)


// FindFirstCPU returns the first cgroup with cpu controller.
func FindFirstCPU(cgroups []procfs.Cgroup) procfs.Cgroup {
	// If only 1 cgroup, simply return it
	if len(cgroups) == 1 {
		return cgroups[0]
	}

	for _, cg := range cgroups {
		// Find first cgroup v1 with cpu controller
		for _, ctlr := range cg.Controllers {
			if ctlr == "cpu" {
				return cg
			}
		}

		// Find first systemd slice
		// https://systemd.io/CGROUP_DELEGATION/#systemds-unit-types
		if strings.HasPrefix(cg.Path, "/system.slice/") || strings.HasPrefix(cg.Path, "/user.slice/") {
			return cg
		}

		// FIXME: what are we looking for here?
		// https://systemd.io/CGROUP_DELEGATION/#controller-support
		for _, ctlr := range cg.Controllers {
			if strings.Contains(ctlr, "systemd") {
				return cg
			}
		}
	}

	return procfs.Cgroup{}
}

// TODO(kakkoyun): Convert other strings to constants.

const (
	CgroupV1FsType          = "cgroup"
	CgroupV2FsType          = "cgroup2"
	CgroupDefaultController = "cpuset"
	CgroupControllersFile   = "/sys/fs/cgroup/cgroup.controllers"
	procCgroups             = "/proc/cgroups"
	sysFsCgroup             = "/sys/fs/cgroup"
)

// TODO(kakkoyun): Does this need to be exported?
type Version int

func (v Version) String() string {
	switch v {
	case V1:
		return CgroupV1FsType
	case V2:
		return CgroupV2FsType
	}

	return ""
}

const (
	V1 Version = iota
	V2
)

var errVersionNotSupported = errors.New("unsupported cgroup version")

// TOOD(kakkoyun): Can be converted to a top-level cache if it's needed.
// - If that's the decision then we should have an interface. .Get?

type Cgroups struct {
	cgroupv1 Cgroup
	cgroupv2 Cgroup
	cgroup   *Cgroup // pointer to default cgroup version.
	hid      int     // default cgroup controller hiearchy ID.
}

func NewCgroups() (*Cgroups, error) {
	var err error
	var cgrp *Cgroup
	var cgroupv1, cgroupv2 Cgroup

	defaultVersion, err := getCgroupDefaultVersion()
	if err != nil {
		return nil, err
	}

	// only start cgroupv1 if it is the OS default (orelse it isn't needed).
	if defaultVersion == V1 {
		cgroupv1, err = NewCgroup(V1)
		if err != nil {
			if !errors.Is(err, errVersionNotSupported) {
				return nil, err
			}
		}
	}

	// start cgroupv2 (if supported).
	cgroupv2, err = NewCgroup(V2)
	if err != nil {
		if !errors.Is(err, errVersionNotSupported) {
			return nil, err
		}
	}

	// at least one (or both) has to be supported.
	if cgroupv1 == nil && cgroupv2 == nil {
		return nil, fmt.Errorf("failed to find cgroup support")

	}

	hid := 0

	// adjust pointer to the default cgroup version
	switch defaultVersion {
	case V1:
		if cgroupv1 == nil {
			return nil, fmt.Errorf("failed to find/mount default %v support", V1.String())
		}
		cgrp = &cgroupv1

		// discover default cgroup controller hierarchy id for cgroupv1
		hid, err = GetCgroupControllerHierarchy(CgroupDefaultController)
		if err != nil {
			return nil, err
		}

	case V2:
		if cgroupv2 == nil {
			return nil, fmt.Errorf("failed to find/mount default %v support", V2.String())
		}
		cgrp = &cgroupv2
	}

	cs := &Cgroups{
		cgroupv1: cgroupv1,
		cgroupv2: cgroupv2,
		cgroup:   cgrp,
		hid:      hid,
	}

	return cs, nil
}

// TODO(kakkoyun): Remove if they are not used.
func (cs *Cgroups) GetDefaultCgroupHierarchyID() int {
	return cs.hid
}

func (cs *Cgroups) GetDefaultCgroup() Cgroup {
	return *cs.cgroup
}

func (cs *Cgroups) GetCgroup(ver Version) Cgroup {
	switch ver {
	case V1:
		return cs.cgroupv1
	case V2:
		return cs.cgroupv2
	}

	return nil
}

// Cgroup models one line from /proc/[pid]/cgroup. Each Cgroup struct describes the placement of a PID inside a
// specific control hierarchy. The kernel has two cgroup APIs, v1 and v2. v1 has one hierarchy per available resource
// controller, while v2 has one unified hierarchy shared by all controllers. Regardless of v1 or v2, all hierarchies
// contain all running processes, so the question answerable with a Cgroup struct is 'where is this process in
// this hierarchy' (where==what path on the specific cgroupfs). By prefixing this path with the mount point of
// *this specific* hierarchy, you can locate the relevant pseudo-files needed to read/set the data for this PID
// in this hierarchy
//
// Also see http://man7.org/linux/man-pages/man7/cgroups.7.html
// type Cgroup struct {
// 	// HierarchyID that can be matched to a named hierarchy using /proc/cgroups. Cgroups V2 only has one
// 	// hierarchy, so HierarchyID is always 0. For cgroups v1 this is a unique ID number
// 	HierarchyID int
// 	// Controllers using this hierarchy of processes. Controllers are also known as subsystems. For
// 	// Cgroups V2 this may be empty, as all active controllers use the same hierarchy
// 	Controllers []string
// 	// Path of this control group, relative to the mount point of the cgroupfs representing this specific
// 	// hierarchy
// 	Path string
// }

type Cgroup interface {
	Path() string
	Version() Version
}

func NewCgroup(ver Version) (Cgroup, error) {
	var c Cgroup

	switch ver {
	case V1:
		c = &CgroupV1{}
	case V2:
		c = &CgroupV2{}
	}

	return c, c.init()
}

// TODO(kakkoyun): Add ID to cgroups?
type CgroupV1 struct {
	hid  int
	path string
	mountpoint string
}

func (c *CgroupV1) Version() Version {
	return V1
}

type CgroupV2 struct {
	hid  int
	path string
	mountpoint string
}

func (c *CgroupV2) Version() Version {
	return V2
}

func getCgroupDefaultVersion() (Version, error) {
	// 1st Method: already mounted cgroupv1 filesystem

	if ok, _ := IsCgroupV2MountedAndDefault(); ok {
		return V2, nil
	}

	//
	// 2nd Method: From cgroup man page:
	// ...
	// 2. The unique ID of the cgroup hierarchy on which this
	//    controller is mounted. If multiple cgroups v1
	//    controllers are bound to the same hierarchy, then each
	//    will show the same hierarchy ID in this field.  The
	//    value in this field will be 0 if:
	//
	//    a) the controller is not mounted on a cgroups v1
	//       hierarchy;
	//    b) the controller is bound to the cgroups v2 single
	//       unified hierarchy; or
	//    c) the controller is disabled (see below).
	// ...

	var value int

	file, err := os.Open(procCgroups)
	if err != nil {
		return -1, fmt.Errorf("failed to open %s: %w", procCgroups, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.Fields(scanner.Text())
		if line[0] != CgroupDefaultController {
			continue
		}
		value, err = strconv.Atoi(line[1])
		if err != nil || value < 0 {
			return -1, fmt.Errorf("error parsing %s: %w", procCgroups, err)
		}
	}

	if value == 0 { // == (a), (b) or (c)
		return V2, nil
	}

	return V1, nil
}

// IsCgroupV2MountedAndDefault tests if cgroup2 is mounted and is the default
// cgroup version being used by the running environment. It does so by checking
// the existance of a "cgroup.controllers" file in default cgroupfs mountpoint.
func IsCgroupV2MountedAndDefault() (bool, error) {
	_, err := os.Stat(CgroupControllersFile)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to open %s: %w", CgroupControllersFile, err)
	}

	return true, nil
}

// Returns a cgroup controller hierarchy value
func GetCgroupControllerHierarchy(subsys string) (int, error) {
	var value int

	file, err := os.Open(procCgroups)
	if err != nil {
		return -1, fmt.Errorf("failed to open %s: %w", procCgroups, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.Fields(scanner.Text())
		if len(line) < 2 || line[0] != subsys {
			continue
		}
		value, err = strconv.Atoi(line[1])
		if err != nil || value < 0 {
			return -1, fmt.Errorf("error parsing %s: %w", procCgroups, err)
		}
	}

	return value, nil
}

// GetCgroupPath walks the cgroup fs and provides the cgroup directory path of
// given cgroupId and subPath (related to cgroup fs root dir). If subPath is
// empty, then all directories from cgroup fs will be searched for the given
// cgroupId.
func GetCgroupPath(rootDir string, cgroupId uint64, subPath string) (string, error) {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		entryPath := filepath.Join(rootDir, entry.Name())
		if strings.HasSuffix(entryPath, subPath) {
			// Lower 32 bits of the cgroup id == inode number of matching cgroupfs entry
			var stat syscall.Stat_t
			if err := syscall.Stat(entryPath, &stat); err == nil {
				// Check if this cgroup path belongs to cgroupId
				if (stat.Ino & 0xFFFFFFFF) == (cgroupId & 0xFFFFFFFF) {
					return entryPath, nil
				}
			}
		}

		// No match at this dir level: continue recursively
		path, err := GetCgroupPath(entryPath, cgroupId, subPath)
		if err == nil {
			return path, nil
		}
	}

	return "", fs.ErrNotExist
}


// TODO(kakkoyun): Find equivalent function using procfs package.

// CRIContainerRuntime defines the interface to interact with the container runtime interfaces.
func CgroupPathV2AddMountpoint(path string) (string, error) {
	pathWithMountpoint := filepath.Join("/sys/fs/cgroup/unified", path)
	if _, err := os.Stat(pathWithMountpoint); os.IsNotExist(err) {
		pathWithMountpoint = filepath.Join("/sys/fs/cgroup", path)
		if _, err := os.Stat(pathWithMountpoint); os.IsNotExist(err) {
			return "", fmt.Errorf("cannot access cgroup %q: %w", path, err)
		}
	}
	return pathWithMountpoint, nil
}


// TODO(kakkoyun): Find equivalent function using procfs package.

// GetCgroupID returns the cgroup2 ID of a path.
func GetCgroupID(pathWithMountpoint string) (uint64, error) {
	hf, _, err := unix.NameToHandleAt(unix.AT_FDCWD, pathWithMountpoint, 0)
	if err != nil {
		return 0, fmt.Errorf("GetCgroupID on %q failed: %w", pathWithMountpoint, err)
	}
	if hf.Size() != 8 {
		return 0, fmt.Errorf("GetCgroupID on %q failed: unexpected size", pathWithMountpoint)
	}
	ret := *(*uint64)(unsafe.Pointer(&hf.Bytes()[0]))
	return ret, nil
}

// TODO(kakkoyun): Find equivalent function using procfs package.

// GetCgroupPaths returns the cgroup1 and cgroup2 paths of a process.
// It does not include the "/sys/fs/cgroup/{unified,systemd,}" prefix.
func GetCgroupPaths(pid int) (string, string, error) {
	cgroupPathV1 := ""
	cgroupPathV2 := ""
	if cgroupFile, err := os.Open(filepath.Join("/proc", fmt.Sprintf("%d", pid), "cgroup")); err == nil {
		defer cgroupFile.Close()

		reader := bufio.NewReader(cgroupFile)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}
			// Fallback in case the system the agent is running on doesn't run systemd
			if strings.Contains(line, ":perf_event:") {
				cgroupPathV1 = strings.SplitN(line, ":", 3)[2]
				cgroupPathV1 = strings.TrimSuffix(cgroupPathV1, "\n")
				continue
			}
			if strings.HasPrefix(line, "1:name=systemd:") {
				cgroupPathV1 = strings.TrimPrefix(line, "1:name=systemd:")
				cgroupPathV1 = strings.TrimSuffix(cgroupPathV1, "\n")
				continue
			}
			if strings.HasPrefix(line, "0::") {
				cgroupPathV2 = strings.TrimPrefix(line, "0::")
				cgroupPathV2 = strings.TrimSuffix(cgroupPathV2, "\n")
				continue
			}
		}
	} else {
		return "", "", fmt.Errorf("cannot parse cgroup: %w", err)
	}

	if cgroupPathV1 == "/" {
		cgroupPathV1 = ""
	}

	if cgroupPathV2 == "/" {
		cgroupPathV2 = ""
	}

	if cgroupPathV2 == "" && cgroupPathV1 == "" {
		return "", "", fmt.Errorf("cannot find cgroup path in /proc/PID/cgroup")
	}

	return cgroupPathV1, cgroupPathV2, nil
}


// TODO(kakkoyun): Remove if there's no use case for it.

// GetMntNs returns the inode number of the mount namespace of a process.
func GetMntNs(pid int) (uint64, error) {
	fileinfo, err := os.Stat(filepath.Join("/proc", fmt.Sprintf("%d", pid), "ns/mnt"))
	if err != nil {
		return 0, err
	}
	stat, ok := fileinfo.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("not a syscall.Stat_t")
	}
	return stat.Ino, nil
}
