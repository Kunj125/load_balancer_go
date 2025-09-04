package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Backend struct {
	URL          *url.URL
	Alive        bool
	mux          sync.RWMutex
	ReverseProxy *httputil.ReverseProxy
}

func (b *Backend) SetAlive(alive bool) {
	b.mux.Lock()
	b.Alive = alive
	b.mux.Unlock()
}

func (b *Backend) IsAlive() bool {
	b.mux.RLock()
	defer b.mux.RUnlock()
	return b.Alive
}

type ServerPool struct {
	backends []*Backend
	current  uint64
}

func (s *ServerPool) AddBackend(b *Backend) {
	s.backends = append(s.backends, b)
}

func (s *ServerPool) NextIndex() int {
	return int(atomic.AddUint64(&s.current, 1) % uint64(len(s.backends)))
}

var pool ServerPool

func addBackend(raw string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		log.Fatalf("invalid backend URL %q: %v", raw, err)
	}
	rp := httputil.NewSingleHostReverseProxy(u)
	pool.AddBackend(&Backend{URL: u, Alive: true, ReverseProxy: rp})
	log.Printf("backend added: %s", u)
}

func runBackend(port int, name string, delay time.Duration) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		fmt.Fprintf(w, "Hello from %s (:%d)\n", name, port)
	})
	addr := fmt.Sprintf(":%d", port)
	log.Printf("%s listening on %s (delay=%v)", name, addr, delay)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func (s *ServerPool) GetNextPeer() *Backend {
	length := len(s.backends)
	if length == 0 {
		return nil
	}
	next := s.NextIndex()

	for i := 0; i < length; i++ {
		idx := (next + i) % length
		if s.backends[idx].IsAlive() {
			if i != 0 {
				atomic.StoreUint64(&s.current, uint64(idx))
			}
			return s.backends[idx]
		}
	}
	return nil
}

func lb(w http.ResponseWriter, r *http.Request) {
	peer := pool.GetNextPeer()
	if peer == nil {
		http.Error(w, "no backends available", http.StatusServiceUnavailable)
		return
	}
	log.Printf("%s %s -> %s", r.Method, r.URL.Path, peer.URL)
	peer.ReverseProxy.ServeHTTP(w, r)
}

func main() {
	var backendList string
	var port int
	var backendOnly bool
	var name string
	var delay time.Duration

	flag.StringVar(&backendList, "backends", "", "Comma-separated backend URLs (e.g. http://127.0.0.1:3031,http://127.0.0.1:3032)")
	flag.IntVar(&port, "port", 8080, "Port to listen on (LB) or for backend when -backend is set")
	flag.BoolVar(&backendOnly, "backend", false, "Run as a simple backend instead of load balancer")
	flag.StringVar(&name, "name", "backend", "Backend display name (when -backend is set)")
	flag.DurationVar(&delay, "delay", 0, "Optional artificial delay for backend responses, e.g. 200ms")
	flag.Parse()

	if backendOnly {
		runBackend(port, name, delay)
		return
	}

	if backendList == "" {
		log.Fatal("please provide -backends=http://127.0.0.1:3031[,http://127.0.0.1:3032,...]")
	}

	for _, raw := range strings.Split(backendList, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil {
			log.Fatalf("invalid backend URL %q: %v", raw, err)
		}
		rp := httputil.NewSingleHostReverseProxy(u)
		pool.AddBackend(&Backend{URL: u, Alive: true, ReverseProxy: rp})
		log.Printf("backend added: %s", u)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		peer := pool.GetNextPeer()
		if peer == nil {
			http.Error(w, "no backends available", http.StatusServiceUnavailable)
			return
		}
		log.Printf("%s %s -> %s", r.Method, r.URL.Path, peer.URL)
		peer.ReverseProxy.ServeHTTP(w, r)
	})

	addr := fmt.Sprintf(":%d", port)
	log.Printf("load balancer listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
