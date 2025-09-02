package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
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

func (s *ServerPool) GetNextPeer() *Backend {
	next := s.NextIndex()
	length := len(s.backends)

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

func main() {
	port := 8080
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello from Load Balancer!")
	})
	log.Printf("Server started at :%d", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}
