package fs

import (
	"fmt"
	"os/exec"

	"os"
	"path"
	"strings"
)

const (
	mountNamespace = "/proc/1/ns/mnt"
)

// Filesystem represents a file system
type Filesystem interface {
	// DirExists checks for existence of directory
	DirExists(dir string) (bool, error)

	// CreateDir creates a new directory
	CreateDir(dir string, recursive bool, perm os.FileMode) error

	// RemoveDir deletes a directory
	RemoveDir(dir string, recursive bool) error

	// Mount mounts a block device
	Mount(device string, target string) error

	// Unmount unmounts a block device
	Unmount(target string) error

	// Format formats a block device
	Format(target string) error
}

type fsInfo struct {
	root string
}

// NewFilesystem creates a new file system object
func NewFilesystem() Filesystem {
	return &fsInfo{}
}

// NewFilesystemBasePath creates a new file system object with a base path
func NewFilesystemBasePath(root string) Filesystem {
	return &fsInfo{root: strings.TrimSuffix(root, "/")}
}

// DirExists checks for existence of directory
func (fs *fsInfo) DirExists(dir string) (bool, error) {
	dir = fs.resolve(dir)
	stat, err := os.Stat(dir)

	if err == nil && stat.IsDir() {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// CreateDir creates a new directory
func (fs *fsInfo) CreateDir(dir string, recursive bool, perm os.FileMode) error {
	dir = fs.resolve(dir)
	if recursive {
		return os.MkdirAll(dir, perm)
	}
	return os.Mkdir(dir, perm)
}

// RemoveDir deletes a directory
func (fs *fsInfo) RemoveDir(dir string, recursive bool) error {
	dir = fs.resolve(dir)
	if recursive {
		return os.RemoveAll(dir)
	}
	return os.Remove(dir)
}

// Mount mounts a block device
func (fs *fsInfo) Mount(device string, target string) error {
	device = fs.resolve(device)
	target = fs.resolve(target)
	return fs.osExec("mount", "-o", "defaults,discard", device, target)
}

// Unmount unmounts a block device
func (fs *fsInfo) Unmount(target string) error {
	target = fs.resolve(target)
	return fs.osExec("umount", target)
}

// Format formats a block device
func (fs *fsInfo) Format(target string) error {
	target = fs.resolve(target)
	return fs.osExec("mkfs.ext4", target)
}

// nsEnter prepends an nsEnter command to the given commnd
func (fs *fsInfo) nsEnter(args ...string) []string {
	if fs.root != "" {
		nse := []string{
			"nsenter",
			fmt.Sprintf("--mount=%s", path.Join(fs.root, mountNamespace)),
			"--",
		}
		return append(nse, args...)
	}
	return args
}

// resolve resolves the given path relative to the fsRoot
func (fs *fsInfo) resolve(p string) string {
	if fs.root != "" {
		return path.Join(fs.root, p)
	}
	return p
}

// osExec runs a shell command
func (fs *fsInfo) osExec(args ...string) error {
	cmd := args[0]
	args = args[1:]
	command := exec.Command(cmd, args...)

	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed, arguments: %v\noutput: %s", cmd, args, string(output))
	}
	return nil
}
