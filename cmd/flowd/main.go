// Command flowd is the Flow daemon: a Windows Service under LocalSystem that owns
// all enforcement authority. It runs as a console app with -dev for development.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/Lushenwar/Flow/internal/paths"
)

const usage = `flowd — Flow daemon

  flowd [-dev] [-port 8787]   run in the foreground (or as a service when SCM starts it)
  flowd install [-port 8787]  register the Windows Service, auto-start on boot
  flowd uninstall             stop, deregister, and delete all state

  -dev   log enforcement actions instead of applying them
`

func main() {
	log.SetFlags(log.Ltime)

	// Subcommand comes first so "flowd install -port 9000" parses.
	cmd := ""
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		cmd = os.Args[1]
		os.Args = append(os.Args[:1], os.Args[2:]...)
	}

	dev := flag.Bool("dev", false, "log enforcement instead of applying it")
	port := flag.Int("port", 8787, "loopback API port")
	block := flag.String("block", "preset.adult,preset.gambling",
		"comma-separated preset ids enforced as baseline (Phase 1: fixed at startup)")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	var err error
	switch cmd {
	case "install":
		err = install(*port)
	case "uninstall":
		err = uninstall()
	case "":
		err = run(*dev, *port, splitIDs(*block))
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		log.Fatalf("%s: %v", cmdName(cmd), err)
	}
}

func cmdName(c string) string {
	if c == "" {
		return "run"
	}
	return c
}

// run picks service mode when the SCM launched us, console mode otherwise.
func run(dev bool, port int, block []string) error {
	if isService() {
		return runService(dev, port, block)
	}
	return runConsole(dev, port, block)
}

func runConsole(dev bool, port int, block []string) error {
	d, err := start(dev, port, block)
	if err != nil {
		return err
	}
	defer d.stop()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	<-sigs
	log.Printf("shutting down")
	return nil
}

// dataDir is what uninstall removes. Named here so both platforms agree.
func dataDir() string { return paths.Dir() }

func splitIDs(s string) []string {
	var out []string
	for _, id := range strings.Split(s, ",") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}
