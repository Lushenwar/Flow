package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/Lushenwar/Flow/internal/api"
	"github.com/Lushenwar/Flow/internal/blocklist"
	"github.com/Lushenwar/Flow/internal/enforce"
	"github.com/Lushenwar/Flow/internal/paths"
	"github.com/Lushenwar/Flow/internal/store"
)

// daemon is everything the process owns. The UI owns none of it: kill the UI and
// this keeps running, which is the whole architecture in one sentence.
type daemon struct {
	st     *store.Store
	srv    *http.Server
	enf    *enforce.Enforcer
	cancel context.CancelFunc
}

// reconcileEvery is the drift-repair interval. Fixed poll, not event-driven, so
// there is a ~3s window after a rule is deleted by hand. Closing it needs WFP
// callouts; this loop is still the actual anti-tamper mechanism.
const reconcileEvery = 3 * time.Second

func start(dev bool, port int, blockIDs []string) (*daemon, error) {
	if err := paths.EnsureDir(); err != nil {
		return nil, err
	}
	st, err := store.Open(paths.DB(), paths.Key())
	if err != nil {
		return nil, err
	}
	token, err := api.LoadOrCreateToken(paths.Token())
	if err != nil {
		st.Close()
		return nil, err
	}
	ln, err := api.Listen(port, paths.Port())
	if err != nil {
		st.Close()
		return nil, err
	}

	if _, err := st.Append("service_start", `{"dev":`+boolJSON(dev)+`}`); err != nil {
		log.Printf("event log: %v", err)
	}
	if dev {
		log.Printf("DEV MODE: enforcement is logged, not applied")
	}

	// Phase 1 has no session logic: the rule set is fixed at startup. Phase 2
	// replaces this with the state machine's output.
	enf := enforce.New(dev, func(kind, data string) {
		if _, err := st.Append(kind, data); err != nil {
			log.Printf("event log: %v", err)
		}
	}, enforce.Layers(paths.Dir(), dev)...)

	var rules []enforce.Rule
	for _, id := range blockIDs {
		rules = append(rules, enforce.Rule{ListID: id, Source: enforce.Baseline})
	}
	eff := enforce.Union(blocklist.Presets(), rules)
	log.Printf("baseline: %v - %d domains, %d processes",
		eff.SortedLists(), len(eff.Domains), len(eff.Processes))
	enf.Set(eff)

	ctx, cancel := context.WithCancel(context.Background())
	go enf.Run(ctx, reconcileEvery)

	log.Printf("listening on http://%s (token: %s)", ln.Addr(), paths.Token())

	d := &daemon{
		st:     st,
		enf:    enf,
		cancel: cancel,
		srv:    &http.Server{Handler: api.New(st, token, dev, enf).Handler()},
	}
	go func() {
		if err := d.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("serve: %v", err)
		}
	}()
	return d, nil
}

func (d *daemon) stop() {
	d.cancel()
	// Enforcement is torn down on shutdown because Phase 1 has no session to
	// protect yet. Phase 2 makes this conditional on the state machine — a
	// service stop must not become an off switch.
	d.enf.Clear()

	if _, err := d.st.Append("service_stop", "{}"); err != nil {
		log.Printf("event log: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d.srv.Shutdown(ctx)
	d.st.Close()
}

func boolJSON(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
