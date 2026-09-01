package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"
)

//go:embed static
var staticFiles embed.FS

func main() {
	defaultAddress := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		defaultAddress = ":" + port
	}
	address := flag.String("addr", defaultAddress, "HTTP listen address")
	flag.Parse()

	store := NewMemoryStore()
	api := NewAPI(store)
	mux := http.NewServeMux()
	api.Register(mux)
	assets, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/", http.FileServer(http.FS(assets)))

	server := &http.Server{
		Addr:              *address,
		Handler:           requestLogger(mux),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("chirepk is running at http://localhost%s", *address)
	log.Fatal(server.ListenAndServe())
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(started).Round(time.Millisecond))
	})
}
