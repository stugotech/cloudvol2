package fs

import (
	"fmt"
	"os/exec"

	"os"
)

// DirExists checks for existence of directory
func DirExists(dir string) (bool, error) {
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
func CreateDir(dir string, recursive bool, perm os.FileMode) error {
	if recursive {
		return os.MkdirAll(dir, perm)
	}
	return os.Mkdir(dir, perm)
}

// RemoveDir deletes a directory
func RemoveDir(dir string, recursive bool) error {
	if recursive {
		return os.RemoveAll(dir)
	}
	return os.Remove(dir)
}

// Mount mounts a block device
func Mount(device string, target string) error {
	return osExec("mount", "-o", "defaults,discard", device, target)
}

// Unmount unmounts a block device
func Unmount(target string) error {
	return osExec("unmount", target)
}

// Format formats a block device
func Format(target string) error {
	return osExec("mkfs.ext4", target)
}

// osExec runs a shell command
func osExec(cmd string, args ...string) error {
	command := exec.Command(cmd, args...)

	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed, arguments: %q\noutput: %s", cmd, args, string(output))
	}
	return nil
}
