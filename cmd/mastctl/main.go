// Command mastctl is the debug CLI. Read-only: it inspects the store and the
// daemon, and has no verb that ends a session.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Lushenwar/Flow/internal/paths"
	"github.com/Lushenwar/Flow/internal/store"
)

const usage = `mastctl — Mast debug CLI (read-only)

  mastctl verify   check every signed row against its HMAC
  mastctl health   GET /api/health from the running daemon
  mastctl events   dump the event log
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "verify":
		err = verify()
	case "health":
		err = fetch("/api/health")
	case "events":
		err = fetch("/api/events")
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func verify() error {
	st, err := store.Open(paths.DB(), paths.Key())
	if err != nil {
		return err
	}
	defer st.Close()

	bad, err := st.Verify()
	if err != nil {
		return err
	}
	if len(bad) == 0 {
		fmt.Println("ok — every signed row matches its contents")
		return nil
	}
	fmt.Printf("TAMPERED — %d row(s) do not match their signature:\n", len(bad))
	for _, b := range bad {
		fmt.Println("  " + b)
	}
	os.Exit(1)
	return nil
}

func fetch(path string) error {
	port, err := os.ReadFile(paths.Port())
	if err != nil {
		return fmt.Errorf("daemon not running? %w", err)
	}
	token, err := os.ReadFile(paths.Token())
	if err != nil {
		return err
	}
	req, err := http.NewRequest("GET", "http://127.0.0.1:"+strings.TrimSpace(string(port))+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var pretty any
	if json.Unmarshal(body, &pretty) == nil {
		out, _ := json.MarshalIndent(pretty, "", "  ")
		fmt.Println(string(out))
	} else {
		fmt.Println(string(body))
	}
	if resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
	return nil
}
