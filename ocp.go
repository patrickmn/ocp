package main

import (
	"compress/gzip"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	version   = "2.7"
	defaultUA = "Optimus Cache Prime/" + version + " (http://patrickmylund.com/projects/ocp/)"
)

var (
	one    chan bool
	sem    chan bool
	wg     sync.WaitGroup
	client = http.DefaultClient
)

type Sitemap struct {
	Loc string `xml:"loc"`
	// Lastmod string `xml:"lastmod"`
}

type Url struct {
	Loc string `xml:"loc"`
	// Lastmod string `xml:"lastmod"`
	// Changefreq string `xml:"changefreq"`
	Priority float64 `xml:"priority"`
}

type Urlset struct {
	XMLName xml.Name  "urlset"
	Sitemap []Sitemap `xml:"sitemap"`
	Url     []Url     `xml:"url"`
}

// Functions needed by sort.Sort
func (u Urlset) Len() int {
	return len(u.Url)
}

func (u Urlset) Swap(i, j int) {
	u.Url[i], u.Url[j] = u.Url[j], u.Url[i]
}

func (u Urlset) Less(i, j int) bool {
	return u.Url[i].Priority > u.Url[j].Priority
}

func get(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	return client.Do(req)
}

func getUrlsFromSitemap(path string, follow bool) (*Urlset, error) {
	var (
		urlset Urlset
		f      io.ReadCloser
		err    error
		res    *http.Response
	)
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		if verbose {
			log.Println("Downloading", path)
		}
		res, err = get(path)
		if err != nil {
			return nil, err
		}
		if res.Status != "200 OK" {
			return nil, fmt.Errorf("HTTP %s", res.Status)
		}
		f = res.Body
	} else {
		f, err = os.Open(path)
		if err != nil {
			return nil, err
		}
	}
	defer f.Close()
	if strings.HasSuffix(path, ".gz") {
		if verbose {
			log.Println("Extracting compressed data")
		}
		f, err = gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer f.Close()
	}
	err = xml.NewDecoder(f).Decode(&urlset)
	if err == nil && follow && len(urlset.Sitemap) > 0 { // This is a sitemapindex
		children := len(urlset.Sitemap)
		ch := make(chan *Urlset, children)
		if verbose {
			log.Printf("%s is a Sitemapindex\n", path)
		}
		for _, v := range urlset.Sitemap {
			sem <- true
			if verbose {
				log.Printf("Adding URLs from child sitemap %s\n", v.Loc)
			}
			go func(loc string) {
				// Follow is false as Sitemapindex spec says sitemapindex children are illegal
				ourlset, err := getUrlsFromSitemap(loc, false)
				if err != nil {
					log.Printf("Error getting Urlset from sitemap %s: %s", loc, err)
					ch <- nil
				} else {
					ch <- ourlset
				}
				<-sem
			}(v.Loc)
		}
		// Add every URL from each Urlset to the main Urlset
		for i := 0; i < children; i++ {
			childUrlset := <-ch
			urlset.Url = append(urlset.Url, childUrlset.Url...)
		}
	}
	return &urlset, err
}

func urlSlice(args []string) []Url {
	urls := make([]Url, len(args))
	for i, v := range args {
		if !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
			v = "http://" + v
		}
		urls[i] = Url{
			Loc: v,
		}
	}
	return urls
}

func primeUrlset(urlset *Urlset) {
	if verbose {
		var top int
		m := int(max)
		l := len(urlset.Url)
		if m > 0 && l > m {
			top = m
		} else {
			top = l
		}
		log.Println("URLs in sitemap:", l, "- URLs to prime:", top)
	}
	wg.Add(len(urlset.Url))
	for _, u := range urlset.Url {
		sem <- true
		go primeUrl(u)
	}
	wg.Wait()
}

func primeUrl(u Url) error {
	var (
		err     error
		found   = false
		weight  = int(u.Priority * 100)
		start   time.Time
		elapsed time.Duration
	)
	if localDir != "" {
		var parsed *url.URL
		parsed, err = url.Parse(u.Loc)
		if err == nil {
			joined := path.Join(localDir, parsed.Path, localSuffix)
			if _, err = os.Lstat(joined); err == nil {
				found = true
				if verbose {
					log.Printf("Exists (weight %d) %s\n", weight, u.Loc)
				}
			}
		}
	}
	if !found {
		if verbose {
			log.Printf("Get (weight %d) %s\n", weight, u.Loc)
		}
		if audit {
			start = time.Now()
		}
		res, err := get(u.Loc)
		if audit {
			elapsed = time.Since(start)
		}
		if err != nil {
			if !nowarn {
				log.Printf("Error priming %s: %v\n", u.Loc, err)
			}
		} else {
			res.Body.Close()
			if audit {
				fmt.Printf("%d\t%4.2f\t%s\n", res.StatusCode, float64(elapsed)/float64(time.Millisecond), u.Loc)
			}
			if res.Status != "200 OK" && !nowarn {
				log.Printf("Bad response for %s: %s\n", u.Loc, res.Status)
			}
		}
		if max > 0 {
			one <- true
		}
	}
	wg.Done()
	<-sem
	return err
}

func maxStopper() {
	count := uint(0)
	for {
		<-one
		count++
		if count == max {
			log.Println("Uncached page prime limit reached; stopping")
			os.Exit(0)
		}
	}
}

var (
	throttle    uint
	max         uint
	localDir    string
	localSuffix string
	userAgent   string
	verbose     bool
	audit       bool
	nowarn      bool
	printUrls   bool
	primeUrls   bool
)

func init() {
	flag.UintVar(&throttle, "c", 1, "URLs to prime at once")
	flag.UintVar(&max, "max", 0, "maximum number of uncached URLs to prime")
	flag.StringVar(&localDir, "l", "", "directory containing cached files (relative file names, i.e. /about/ -> <path>/about/index.html)")
	flag.StringVar(&localSuffix, "ls", "index.html", "suffix of locally cached files")
	flag.StringVar(&userAgent, "ua", defaultUA, "User-Agent header to send")
	flag.BoolVar(&verbose, "v", false, "show additional information about the priming process")
	flag.BoolVar(&nowarn, "no-warn", false, "do not warn about pages that were not primed successfully")
	flag.BoolVar(&audit, "a", false, "output HTTP status codes, fetch time. Incompatible with -v -a")
	flag.BoolVar(&printUrls, "print", false, "(exclusive) just print the sorted URLs (can be used with xargs)")
	flag.BoolVar(&primeUrls, "urls", false, "prime the URLs given as arguments rather than a sitemap")
	flag.Parse()
}

func main() {
	var (
		urlset *Urlset
		err    error
	)
	if flag.NArg() == 0 {
		fmt.Println("Optimus Cache Prime", version)
		fmt.Println("http://patrickmylund.com/projects/ocp/")
		fmt.Println("-----")
		flag.Usage()
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println(" ", os.Args[0], "sitemap.xml")
		fmt.Println(" ", os.Args[0], "http://mysite.com/sitemap.xml")
		fmt.Println(" ", os.Args[0], "-c 10 http://mysite.com/sitemap.xml.gz")
		fmt.Println(" ", os.Args[0], "-l /var/www/mysite.com/wp-content/cache/supercache/ http://mysite.com/sitemap.xml")
		fmt.Println(" ", os.Args[0], "-l /var/www/mysite.com/wp-content/w3tc/pgcache/ -ls _index.html http://mysite.com/sitemap.xml")
		fmt.Println(" ", os.Args[0], "--print http://mysite.com/sitemap.xml | xargs curl -I")
		fmt.Println(" ", os.Args[0], "--urls http://foo.com/a http://foo.com/b")
		fmt.Println("")
		fmt.Println("If specifying a sitemap URL, make sure to prepend http:// or https://")
		return
	}
	if max > 0 {
		one = make(chan bool)
	}
	sem = make(chan bool, throttle)
	if primeUrls {
		urlset = &Urlset{
			Url: urlSlice(flag.Args()),
		}
	} else {
		path := flag.Arg(0)
		urlset, err = getUrlsFromSitemap(path, true)
		sort.Sort(urlset)
	}
	if audit {
		verbose = false
		nowarn = true
	}
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		if printUrls {
			for _, v := range urlset.Url {
				fmt.Println(v.Loc)
			}
		} else {
			if max > 0 {
				go maxStopper()
			}
			primeUrlset(urlset)
		}
	}
}
