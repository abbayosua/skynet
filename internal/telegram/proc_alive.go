package telegram

import (
	"os"
	"syscall"
)

func findProcess(pid int) (*os.Process, error) {
	return os.FindProcess(pid)
}

func signalProcess(proc *os.Process, sig syscall.Signal) bool {
	return proc.Signal(sig) == nil
}
