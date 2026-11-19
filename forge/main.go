package main

// The process: bind an ephemeral port, tell the suite which, serve until the suite says it is done.
//
//	MAKEFUN_ENV_URL     the suite's origin; POST /register there, then poll GET /done
//	MAKEFUN_SHOP_TOKEN  the bearer token every API call must present
//	MAKEFUN_TRIAL_NOW   the instance's opening time; every timestamp the forge writes derives from it
//	FORGE_ADDR          (development) a fixed listen address such as 127.0.0.1:7100
//
// Without MAKEFUN_ENV_URL the forge just serves, which is how the kit's local runner drives it.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

func main() {
	// The trial pod gives the entry two CPUs and shares them with the suite. The runtime cannot see
	// that limit (it counts the host's CPUs), so cap the scheduler at it.
	if os.Getenv("GOMAXPROCS") == "" {
		procs := runtime.NumCPU()
		if procs > 2 {
			procs = 2
		}
		runtime.GOMAXPROCS(procs)
	}
	// Everything lives in memory and the trial is short: collect rarely, well inside 1 GiB.
	debug.SetGCPercent(400)
	debug.SetMemoryLimit(640 << 20)

	app := newApp(os.Getenv("MAKEFUN_SHOP_TOKEN"), os.Getenv("MAKEFUN_TRIAL_NOW"))

	addr := os.Getenv("FORGE_ADDR")
	if addr == "" {
		addr = ":0"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[forge] cannot listen on %s: %v\n", addr, err)
		os.Exit(1)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	fmt.Fprintf(os.Stdout, "[forge] listening on port %d\n", port)

	go func() {
		if err := serve(listener, app); err != nil {
			fmt.Fprintf(os.Stderr, "[forge] server stopped: %v\n", err)
			os.Exit(1)
		}
	}()

	envURL := strings.TrimRight(os.Getenv("MAKEFUN_ENV_URL"), "/")
	if envURL == "" {
		select {}
	}
	register(envURL, port)
	fmt.Fprintln(os.Stdout, "[forge] registered; waiting for the suite to say done")
	waitForDone(envURL)
	fmt.Fprintln(os.Stdout, "[forge] done")
	os.Exit(0)
}

// lifecycleClient talks to the suite directly: the suite sits beside the forge, so no proxy setting
// in the environment may come between them.
var lifecycleClient = &http.Client{
	Timeout:   30 * time.Second,
	Transport: &http.Transport{Proxy: nil, MaxIdleConns: 2, IdleConnTimeout: 90 * time.Second},
}

// register POSTs the bound port until the suite takes it. 200 is the normal answer and 409 means a
// first register already won; anything else is retried, since the suite may still be starting.
func register(envURL string, port int) {
	body, _ := json.Marshal(map[string]int{"port": port})
	delay := 50 * time.Millisecond
	for {
		res, err := lifecycleClient.Post(envURL+"/register", "application/json", bytes.NewReader(body))
		if err == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if res.StatusCode == 200 || res.StatusCode == 409 {
				return
			}
			if res.StatusCode == 400 || res.StatusCode == 405 {
				fmt.Fprintf(os.Stderr, "[forge] register answered HTTP %d\n", res.StatusCode)
				os.Exit(1)
			}
		}
		time.Sleep(delay)
		if delay < 2*time.Second {
			delay *= 2
		}
	}
}

// waitForDone polls GET /done until the suite answers {"done": true}.
func waitForDone(envURL string) {
	for {
		res, err := lifecycleClient.Get(envURL + "/done")
		if err == nil {
			var answer struct {
				Done bool `json:"done"`
			}
			decodeErr := json.NewDecoder(res.Body).Decode(&answer)
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if decodeErr == nil && res.StatusCode == 200 && answer.Done {
				return
			}
		}
		time.Sleep(400 * time.Millisecond)
	}
}
