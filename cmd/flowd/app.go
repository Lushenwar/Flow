package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Lushenwar/Flow/internal/api"
	"github.com/Lushenwar/Flow/internal/blocklist"
	"github.com/Lushenwar/Flow/internal/enforce"
	"github.com/Lushenwar/Flow/internal/paths"
	"github.com/Lushenwar/Flow/internal/session"
	"github.com/Lushenwar/Flow/internal/store"
)

// daemon is everything the process owns. The UI owns none of it: kill the UI and
// this keeps running, which is the whole architecture in one sentence.
type daemon struct {
	st     *store.Store
	srv    *http.Server
	enf    *enforce.Enforcer
	mgr    *session.Manager
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

	mgr := session.NewManager(st, session.SystemClock{}, enf, blocklist.Presets(), blockIDs)
	if err := mgr.Load(); err != nil {
		st.Close()
		ln.Close()
		return nil, fmt.Errorf("restore session: %w", err)
	}
	if s := mgr.Snapshot(); s.Active() {
		log.Printf("session restored: %s, %v remaining", s.State, s.Target.Remaining(mgr.Clock(), nil))
	}
	log.Printf("baseline: %v", blockIDs)

	// Bound the event log. Once on start and daily after: it is a tamper record,
	// so Prune writes a log_pruned row naming what it removed — a gap in the ids
	// with no such row before it is evidence, a gap with one is housekeeping.
	if n, err := st.Prune(); err != nil {
		log.Printf("prune event log: %v", err)
	} else if n > 0 {
		log.Printf("pruned %d event(s) older than %d days", n, store.RetentionDays)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go enf.Run(ctx, reconcileEvery)
	go mgr.Run(ctx)
	go func() {
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, err := st.Prune(); err != nil {
					log.Printf("prune event log: %v", err)
				}
			}
		}
	}()

	log.Printf("listening on http://%s (token: %s)", ln.Addr(), paths.Token())

	d := &daemon{
		st:     st,
		enf:    enf,
		mgr:    mgr,
		cancel: cancel,
		srv:    &http.Server{Handler: api.New(st, token, dev, enf, mgr).Handler()},
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
	// Only tear enforcement down when there is nothing to protect. A service stop
	// during a locked session must not become an off switch: the hosts entries
	// stay, and the daemon re-applies the rest when it comes back.
	if s := d.mgr.Snapshot(); !s.Active() {
		d.enf.Clear()
	} else {
		log.Printf("session still active (%v remaining); leaving enforcement in place",
			s.Target.Remaining(d.mgr.Clock(), nil))
	}

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
