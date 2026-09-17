package main

import (
	"flag"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

func handler(directory string, upstream *url.URL) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", httputil.NewSingleHostReverseProxy(upstream)))
	mux.Handle("/", http.FileServer(http.Dir(directory)))
	return mux
}

func main() {
	addr := flag.String("addr", ":8081", "HTTP listen address")
	directory := flag.String("dir", "cmd/web/www", "Static files directory, relative to the working directory")
	api := flag.String("api", "http://localhost:8080", "Presence API URL")
	flag.Parse()
	upstream, err := url.Parse(*api)
	if err != nil || upstream.Host == "" || (upstream.Scheme != "http" && upstream.Scheme != "https") {
		log.Fatal("Invalid presence API URL")
	}
	server := &http.Server{Addr: *addr, Handler: handler(*directory, upstream), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("Web app listening on %s", *addr)
	log.Fatal(server.ListenAndServe())
}
