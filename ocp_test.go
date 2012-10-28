package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

func init() {
	flag.Parse()
	sem = make(chan bool, throttle)
}

var hostPortHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("close") == "true" {
		w.Header().Set("Connection", "close")
	}
	w.Write([]byte(r.RemoteAddr))
})

func dummyServer(ch chan<- string) *httptest.Server {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "ocpdummy %s", r.URL.Path)
		ch <- r.URL.Path
	})
	return httptest.NewServer(h)
}

func TestGetUrlsFromSitemap(t *testing.T) {
	f, err := ioutil.TempFile("", "ocp-testsitemap.xml")
	if err != nil {
		t.Fatal("Couldn't write test sitemap:", f.Name())
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
	urlset, err := getUrlsFromSitemap(f.Name(), true)
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
	f1, err := ioutil.TempFile("", "ocp-testchild1.xml")
	if err != nil {
		t.Fatal("Couldn't write test child 1")
	}
	f1.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<url>
    <loc>http://localhost:8081/a</loc>
    <priority>0.4</priority>
</url>
</urlset>`)
	f1.Close()

	f2, err := ioutil.TempFile("", "ocp-testchild2.xml")
	if err != nil {
		t.Fatal("Couldn't write test child 2")
	}
	f2.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<url>
    <loc>http://localhost:8081/b</loc>
    <priority>0.6</priority>
</url>
</urlset>`)
	f2.Close()

	f3, err := ioutil.TempFile("", "ocp-testchild3.xml")
	if err != nil {
		t.Fatal("Couldn't write test child 3")
	}
	f3.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<url>
    <loc>http://localhost:8081/c</loc>
    <priority>1.0</priority>
</url>
</urlset>`)
	f3.Close()

	fi, err := ioutil.TempFile("", "ocp-testsitemapindex.xml")
	if err != nil {
		t.Fatal("Couldn't write test sitemapindex")
	}
	fmt.Fprintf(fi, `<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="http://www.sitemaps.org/schemas/sitemap/0.9 http://www.sitemaps.org/schemas/sitemap/0.9/siteindex.xsd" xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<sitemap>
    <loc>%s</loc>
</sitemap>
<sitemap>
    <loc>%s</loc>
</sitemap>
<sitemap>
    <loc>%s</loc>
</sitemap>
</sitemapindex>`, f1.Name(), f2.Name(), f3.Name())
	fi.Close()
	urlset, err := getUrlsFromSitemap(fi.Name(), true)
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
	s := dummyServer(ch)
	defer s.Close()
	a := Url{s.URL + "/a", 0.4}
	b := Url{s.URL + "/b", 0.6}
	c := Url{s.URL + "/c", 1.0}
	urlset := &Urlset{Url: []Url{a, b, c}}
	sort.Sort(urlset)
	go primeUrlset(urlset)
	for i := 0; i < 3; i++ {
		msg := <-ch
		if msg != "/a" && msg != "/b" && msg != "/c" {
			t.Error("Web server on :8081 did not acknowledge a, b, c requests")
		}
	}
}
