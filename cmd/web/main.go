package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"github.com/fulldump/goconfig"
)

func handler(directory string, upstream *url.URL) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", httputil.NewSingleHostReverseProxy(upstream)))
	mux.Handle("/", http.FileServer(http.Dir(directory)))
	return mux
}

type Config struct {
	Addr string `json:"addr" usage:"HTTP listen address"`
	Dir  string `json:"dir" usage:"Static files directory, relative to the working directory"`
	API  string `json:"api" usage:"Presence API URL"`
}

func main() {

	c := &Config{
		Addr: ":8081",
		Dir:  "cmd/web/www",
		API:  "http://localhost:8080",
	}

	goconfig.Read(c)

	upstream, err := url.Parse(c.API)
	if err != nil || upstream.Host == "" || (upstream.Scheme != "http" && upstream.Scheme != "https") {
		log.Fatal("Invalid presence API URL")
	}
	info, err := os.Stat(c.Dir)
	if err != nil || !info.IsDir() {
		log.Fatalf("Static directory is unavailable: %s", c.Dir)
	}
	server := &http.Server{Addr: c.Addr, Handler: handler(c.Dir, upstream), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("Web app listening on %s", c.Addr)
	log.Fatal(server.ListenAndServe())
}
