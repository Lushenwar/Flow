//go:build !windows

package main

import (
	"errors"
	"log"
	"os"
)

// ponytail: LaunchDaemon / systemd registration lands with the macOS and Linux
// enforcers. Until then the daemon runs in the foreground off Windows.

func isService() bool { return false }

func runService(dev bool, port int) error { return runConsole(dev, port) }

func install(port int) error {
	return errors.New("service install is Windows-only for now; run `mastd -dev` in the foreground")
}

func uninstall() error {
	if err := os.RemoveAll(dataDir()); err != nil {
		return err
	}
	log.Printf("removed %s", dataDir())
	return nil
}
