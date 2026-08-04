package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/Lushenwar/Flow/internal/api"
	"github.com/Lushenwar/Flow/internal/paths"
	"github.com/Lushenwar/Flow/internal/store"
)

// daemon is everything the process owns. The UI owns none of it: kill the UI and
// this keeps running, which is the whole architecture in one sentence.
type daemon struct {
	st  *store.Store
	srv *http.Server
}

func start(dev bool, port int) (*daemon, error) {
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
	log.Printf("listening on http://%s (token: %s)", ln.Addr(), paths.Token())

	d := &daemon{st: st, srv: &http.Server{Handler: api.New(st, token, dev).Handler()}}
	go func() {
		if err := d.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("serve: %v", err)
		}
	}()
	return d, nil
}

func (d *daemon) stop() {
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
