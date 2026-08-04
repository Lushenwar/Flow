//go:build windows

package enforce

import (
	"log"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ProcWatch enforces process rules by polling the process list and terminating
// matches. Not ETW: Go's ETW story is weak and nobody launches Steam in under a
// second, so a poll on the reconcile tick is enough.
type ProcWatch struct{}

func NewProcWatch() *ProcWatch { return &ProcWatch{} }

func (p *ProcWatch) Name() string { return "process" }

func (p *ProcWatch) Apply(eff Effective) error {
	if len(eff.Processes) == 0 {
		return nil
	}
	running, err := snapshot()
	if err != nil {
		return err
	}
	for _, proc := range running {
		if !shouldTerminate(proc.name, eff) {
			continue
		}
		if err := terminate(proc.pid); err != nil {
			log.Printf("process %s (%d): %v", proc.name, proc.pid, err)
			continue
		}
		log.Printf("terminated %s (%d)", proc.name, proc.pid)
	}
	return nil
}

// Drifted reports whether a blocked process is currently running.
func (p *ProcWatch) Drifted(eff Effective) (bool, error) {
	if len(eff.Processes) == 0 {
		return false, nil
	}
	running, err := snapshot()
	if err != nil {
		return false, err
	}
	for _, proc := range running {
		if shouldTerminate(proc.name, eff) {
			return true, nil
		}
	}
	return false, nil
}

// Clear is a no-op: there is nothing installed to remove, and resurrecting the
// games we closed is not our job.
func (p *ProcWatch) Clear() error { return nil }

type procInfo struct {
	pid  uint32
	name string
}

func snapshot() ([]procInfo, error) {
	h, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)

	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	if err := windows.Process32First(h, &e); err != nil {
		return nil, err
	}
	var out []procInfo
	for {
		out = append(out, procInfo{
			pid:  e.ProcessID,
			name: strings.ToLower(windows.UTF16ToString(e.ExeFile[:])),
		})
		if err := windows.Process32Next(h, &e); err != nil {
			break // ERROR_NO_MORE_FILES
		}
	}
	return out, nil
}

func terminate(pid uint32) error {
	// PID 0 and 4 are System; never touch them.
	if pid <= 4 {
		return nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}
