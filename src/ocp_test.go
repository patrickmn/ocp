package main

import (
	"flag"
	"fmt"
	"http"
	"os"
	"sort"
	"sync"
	"testing"
)

func init() {
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

func TestGetUrlsFromSitemap(t *testing.T) {
	fname := "_test/testsitemap.xml"
	f, err := os.Create(fname)
	if err != nil {
		t.Fatal("Couldn't write test sitemap:", fname)
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
	urlset, err := GetUrlsFromSitemap(fname, true)
	if err != nil ||
		urlset.Url[0].Loc != "http://localhost:8081/a" ||
		urlset.Url[0].Priority != 0.4 ||
		urlset.Url[1].Loc != "http://localhost:8081/b" ||
		urlset.Url[1].Priority != 0.6 ||
		urlset.Url[2].Loc != "http://localhost:8081/c" ||
		urlset.Url[2].Priority != 1.0 {
		t.Fatal("Incorrectly parsed urlset:", urlset)
	}
}

func TestGetUrlsFromSitemapindex(t *testing.T) {
	fname := "_test/testchild1.xml"
	f, err := os.Create(fname)
	if err != nil {
		t.Fatal("Couldn't write test child 1:", fname)
	}
	f.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<url>
    <loc>http://localhost:8081/a</loc>
    <priority>0.4</priority>
</url>
</urlset>`)
	f.Close()

	fname = "_test/testchild2.xml"
	f, err = os.Create(fname)
	if err != nil {
		t.Fatal("Couldn't write test child 2:", fname)
	}
	f.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<url>
    <loc>http://localhost:8081/b</loc>
    <priority>0.6</priority>
</url>
</urlset>`)
	f.Close()

	fname = "_test/testchild3.xml"
	f, err = os.Create(fname)
	if err != nil {
		t.Fatal("Couldn't write test child 3:", fname)
	}
	f.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<url>
    <loc>http://localhost:8081/c</loc>
    <priority>1.0</priority>
</url>
</urlset>`)
	f.Close()

	fname = "_test/testsitemapindex.xml"
	f, err = os.Create(fname)
	if err != nil {
		t.Fatal("Couldn't write test sitemapindex:", fname)
	}
	f.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="http://www.sitemaps.org/schemas/sitemap/0.9 http://www.sitemaps.org/schemas/sitemap/0.9/siteindex.xsd" xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<sitemap>
    <loc>_test/testchild1.xml</loc>
</sitemap>
<sitemap>
    <loc>_test/testchild2.xml</loc>
</sitemap>
<sitemap>
    <loc>_test/testchild3.xml</loc>
</sitemap>
</sitemapindex>`)
	f.Close()
	urlset, err := GetUrlsFromSitemap(fname, true)
	if err != nil ||
		urlset.Url[0].Loc != "http://localhost:8081/a" ||
		urlset.Url[0].Priority != 0.4 ||
		urlset.Url[1].Loc != "http://localhost:8081/b" ||
		urlset.Url[1].Priority != 0.6 ||
		urlset.Url[2].Loc != "http://localhost:8081/c" ||
		urlset.Url[2].Priority != 1.0 {
		t.Fatal("Incorrectly parsed urlset:", urlset)
	}
}

func TestPrimeUrlset(t *testing.T) {
	ch := make(chan string)
	go DummyServer(":8081", ch)
	a := Url{"http://localhost:8081/a", 0.4}
	b := Url{"http://localhost:8081/b", 0.6}
	c := Url{"http://localhost:8081/c", 1.0}
	urlset := &Urlset{Url: []Url{a, b, c}}
	sort.Sort(urlset)
	go PrimeUrlset(urlset)
	for i := 0; i < 3; i++ {
		msg := <-ch
		if msg != "/a" && msg != "/b" && msg != "/c" {
			t.Error("Web server on :8081 did not acknowledge a, b, c requests")
		}
	}
}
