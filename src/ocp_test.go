package main

import (
	"flag"
	"fmt"
	"http"
	"os"
	"sync"
	"testing"
)

func initVars() {
	flag.Parse()
	sem = make(chan bool, 1)
	wg = &sync.WaitGroup{}
	client = http.DefaultClient
}

func DummyServer(address string, ch chan<- string) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "ocpdummy %s", r.URL.Path)
		ch <- r.URL.Path
	})
	http.ListenAndServe(address, nil)
}

func TestPrimeUrlset(t *testing.T) {
	initVars()
	ch := make(chan string)
	go DummyServer(":8081", ch)
	a := Url{"http://localhost:8081/a", 0.4}
	b := Url{"http://localhost:8081/b", 0.6}
	c := Url{"http://localhost:8081/c", 1.0}
	urlset := &Urlset{Url: []Url{a, b, c}}
	go PrimeUrlset(urlset)
	for i := 0; i < 3; i++ {
		msg := <-ch
		if msg != "/a" && msg != "/b" && msg != "/c" {
			t.Error("Web server on :8081 did not acknowledge a, b, c requests")
		}
	}
}

func TestGetUrlsFromSitemap(t *testing.T) {
	fname := "_test/testsitemap.xml"
	f, err := os.Create(fname)
	if err != nil {
		t.Fatal("Couldn't write test sitemap ", fname)
	}
	f.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<url>
    <loc>http://localhost:8081/a</loc>
    <priority>0.4</priority>
</url>
<url>
    <loc>http://localhost:8081/b</loc>
    <priority>0.6</priority>
</url>
<url>
    <loc>http://localhost:8081/c</loc>
    <priority>1.0</priority>
</url>
</urlset>`)
	f.Close()
	urlset, err := GetUrlsFromSitemap(fname)
	if err != nil ||
		urlset.Url[0].Loc != "http://localhost:8081/a" ||
		urlset.Url[0].Priority != 0.4 ||
		urlset.Url[1].Loc != "http://localhost:8081/b" ||
		urlset.Url[1].Priority != 0.6 ||
		urlset.Url[2].Loc != "http://localhost:8081/c" ||
		urlset.Url[2].Priority != 1.0 {
		t.Error("Incorrectly parsed urlset:", urlset)
	}
}
