//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package executioninbox

import (
	"golang.org/x/sys/unix"
	"os"
)

func lockFile(f *os.File) error         { return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
func publishFile(from, to string) error { return os.Rename(from, to) }
func syncDirectory(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
