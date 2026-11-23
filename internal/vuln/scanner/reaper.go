package scanner

import (
	"errors"
	"log/slog"
	"os"
	"syscall"
)

// Reaper ends every other process of the scanner's container when no analysis runs (Q-ARCH-530): a process the analyser started
// and that left its process group survives the group's kill, and could wait for the next image and its token. The scanner is the
// container's first process (no shell, no init, R-HA-065), so "every other process" is exactly what kill(-1) reaches, and orphans
// are its to reap. Anywhere else - a development process, a test - the same call would reach every process of the user, so the
// reaper does nothing unless the process is PID 1.
type Reaper struct {
	log *slog.Logger
	// pid1 says the process is its container's first; tests set it false.
	pid1 bool
	kill func(pid int, sig syscall.Signal) error
	wait func() (int, error)
}

// NewReaper returns the reaper of this process.
func NewReaper(log *slog.Logger) *Reaper {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Reaper{log: log, pid1: os.Getpid() == 1, kill: syscall.Kill, wait: reapOne}
}

// Active reports whether the reaper acts: the process is its container's first.
func (r *Reaper) Active() bool { return r != nil && r.pid1 }

// Reap sends SIGKILL to every other process of the container and reaps what has ended. The caller holds every slot, so no analyser
// of a running call can be reached and no wait of exec.Cmd races the reaping.
func (r *Reaper) Reap() {
	if !r.Active() {
		return
	}
	if err := r.kill(-1, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		r.log.Warn("the scanner could not end the leftover processes of its container", "error", err)
	}
	for n := 0; n < 1024; n++ {
		pid, err := r.wait()
		if pid <= 0 || err != nil {
			return
		}
		r.log.Warn("the scanner ended a process that outlived its analysis", "pid", pid)
	}
}

// reapOne waits for one ended child without blocking: 0 when none has ended.
func reapOne() (int, error) {
	var ws syscall.WaitStatus
	return syscall.Wait4(-1, &ws, syscall.WNOHANG, nil)
}
