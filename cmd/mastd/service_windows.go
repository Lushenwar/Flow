//go:build windows

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Lushenwar/Flow/internal/paths"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// serviceName is what shows up in services.msc. Real name, no spoofing — CLAUDE.md
// forbids hiding, and a service pretending to be something else gets flagged by Defender.
const serviceName = paths.AppName

func isService() bool {
	is, err := svc.IsWindowsService()
	return err == nil && is
}

type handler struct {
	dev  bool
	port int
}

func (h *handler) Execute(args []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}

	d, err := start(h.dev, h.port)
	if err != nil {
		log.Printf("start: %v", err)
		return false, 1
	}
	defer d.stop()

	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for c := range req {
		switch c.Cmd {
		case svc.Interrogate:
			status <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			// An elevated user stopped us through the SCM. That is allowed and final:
			// watchdogs monitor, they do not resurrect past an explicit SCM stop.
			status <- svc.Status{State: svc.StopPending}
			return false, 0
		}
	}
	return false, 0
}

func runService(dev bool, port int) error {
	return svc.Run(serviceName, &handler{dev: dev, port: port})
}

func install(port int) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM (run elevated): %w", err)
	}
	defer m.Disconnect()

	if s, err := m.OpenService(serviceName); err == nil {
		s.Close()
		return fmt.Errorf("%s is already installed; run `mastd uninstall` first", serviceName)
	}

	s, err := m.CreateService(serviceName, exe, mgr.Config{
		DisplayName:  paths.AppName + " commitment daemon",
		Description:  "Enforces focus sessions and baseline blocklists. Uninstall with `mastd uninstall` from an elevated prompt.",
		StartType:    mgr.StartAutomatic, // survives reboot — threat model row 5
		ServiceType:  windowsServiceOwnProcess,
		Dependencies: []string{"Tcpip"},
	}, "-port", fmt.Sprint(port))
	if err != nil {
		return err
	}
	defer s.Close()

	if err := s.Start(); err != nil {
		return fmt.Errorf("created but failed to start: %w", err)
	}
	log.Printf("installed and started %s (port %d)", serviceName, port)
	return nil
}

const windowsServiceOwnProcess = 0x10

// uninstall always works, in every state, mid-session included. Never fight the
// administrator. Every step runs even if an earlier one fails — a teardown that
// gives up halfway leaves exactly the residue it was written to prevent.
func uninstall() error {
	var errs []error

	if err := removeService(); err != nil {
		errs = append(errs, err)
	}
	// Phase 1 adds WFP filters and hosts entries to this teardown.
	if err := os.RemoveAll(dataDir()); err != nil {
		errs = append(errs, fmt.Errorf("remove %s: %w", dataDir(), err))
	} else {
		log.Printf("removed %s", dataDir())
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	log.Printf("uninstalled — zero residue")
	return nil
}

func removeService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM, run from an elevated prompt to remove the service: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		log.Printf("service %s not registered", serviceName)
		return nil
	}
	defer s.Close()

	if _, err := s.Control(svc.Stop); err != nil {
		log.Printf("stop: %v (continuing)", err)
	}
	waitStopped(s)
	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	log.Printf("removed service %s", serviceName)
	return nil
}

func waitStopped(s *mgr.Service) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		st, err := s.Query()
		if err != nil || st.State == svc.Stopped {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}
