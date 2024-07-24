package procutils

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// Checks whether a process with the given pid (still) exists
func ProcessExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// "on Unix systems, FindProcess always succeeds"
	// see: https://pkg.go.dev/os#FindProcess
	exists := process.Signal(syscall.Signal(0)) == nil
	return exists
}

func KillProcess(pid int) error {
	kill := exec.Command("kill", strconv.Itoa(pid))
	return kill.Run()
}
