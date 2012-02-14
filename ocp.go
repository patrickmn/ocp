package main

import (
	"compress/gzip"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
)

const (
	version   = "2.5"
	useragent = "Optimus Cache Prime/" + version + " (http://patrickmylund.com/projects/ocp/)"
)

var (
	one    chan bool
	sem    chan bool
	wg     = &sync.WaitGroup{}
	client = http.DefaultClient

	throttle    *uint   = flag.Uint("c", 1, "URLs to prime at once")
	max         *uint   = flag.Uint("max", 0, "maximum number of uncached URLs to prime")
	localDir    *string = flag.String("l", "", "directory containing cached files (relative file names, i.e. /about/ -> <path>/about/index.html)")
	localSuffix *string = flag.String("ls", "index.html", "suffix of locally cached files")
	verbose     *bool   = flag.Bool("v", false, "show additional information about the priming process")
	nowarn      *bool   = flag.Bool("no-warn", false, "do not warn about pages that were not primed successfully")
	printurls   *bool   = flag.Bool("print", false, "(exclusive) just print the sorted URLs (can be used with xargs)")
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

func Get(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("User-Agent", useragent)
	return client.Do(req)
}

func GetUrlsFromSitemap(path string, follow bool) (*Urlset, error) {
	var (
		urlset Urlset
		f      io.ReadCloser
		err    error
		res    *http.Response
	)
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		if *verbose {
			log.Println("Downloading", path)
		}
		res, err = Get(path)
		if res.Status != "200 OK" {
			return nil, fmt.Errorf("HTTP %s", res.Status)
		}
		f = res.Body
	} else {
		f, err = os.Open(path)
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if strings.HasSuffix(path, ".gz") {
		if *verbose {
			log.Println("Extracting compressed data")
		}
		f, err = gzip.NewReader(f)
		defer f.Close()
		if err != nil {
			return nil, err
		}
	}
	data, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}
	err = xml.Unmarshal(data, &urlset)
	if err == nil && follow && len(urlset.Sitemap) > 0 { // This is a sitemapindex
		ch := make(chan *Urlset, len(urlset.Sitemap))
		if *verbose {
			log.Printf("%s is a Sitemapindex\n", path)
		}
		for _, v := range urlset.Sitemap {
			sem <- true
			if *verbose {
				log.Printf("Adding URLs from child sitemap %s\n", v.Loc)
			}
			go func(loc string) {
				// Follow is false as Sitemapindex spec says sitemapindex children are illegal
				ourlset, err := GetUrlsFromSitemap(loc, false)
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
		for i := 0; i < len(urlset.Sitemap); i++ {
			childUrlset := <-ch
			if childUrlset != nil {
				urlset.Url = append(urlset.Url, childUrlset.Url...)
			}
		}
	}
	return &urlset, err
}

func PrimeUrlset(urlset *Urlset) {
	if *verbose {
		var top int
		m := int(*max)
		l := len(urlset.Url)
		if m > 0 && l > m {
			top = m
		} else {
			top = l
		}
		log.Println("URLs in sitemap:", l, "- URLs to prime:", top)
	}
	for _, u := range urlset.Url {
		sem <- true
		wg.Add(1)
		go PrimeUrl(u)
	}
	wg.Wait()
}

func PrimeUrl(u Url) error {
	var (
		err    error
		found  = false
		weight = int(u.Priority * 100)
	)
	if *localDir != "" {
		var parsed *url.URL
		parsed, err = url.ParseWithReference(u.Loc)
		if err == nil {
			joined := path.Join(*localDir, parsed.Path, *localSuffix)
			if _, err = os.Lstat(joined); err == nil {
				found = true
				if *verbose {
					log.Printf("Exists (weight %d) %s\n", weight, u.Loc)
				}
			}
		}
	}
	if !found {
		if *verbose {
			log.Printf("Get (weight %d) %s\n", weight, u.Loc)
		}
		res, err := Get(u.Loc)
		if err != nil {
			if !*nowarn {
				log.Printf("Error priming %s: %v\n", u.Loc, err)
			}
		} else {
			res.Body.Close()
			if res.Status != "200 OK" && !*nowarn {
				log.Printf("Bad response for %s: %s\n", u.Loc, res.Status)
			}
		}
		if *max > 0 {
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
		if count == *max {
			log.Println("Uncached page prime limit reached; stopping")
			os.Exit(0)
		}
	}
}

func main() {
	var (
		urlset *Urlset
		err    error
	)
	flag.Parse()
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
		fmt.Println("")
		fmt.Println("If specifying a sitemap URL, make sure to prepend http:// or https://")
		return
	}
	if *max > 0 {
		one = make(chan bool)
	}
	sem = make(chan bool, *throttle)
	path := flag.Arg(0)
	urlset, err = GetUrlsFromSitemap(path, true)
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		sort.Sort(urlset)
		if *printurls {
			for _, v := range urlset.Url {
				fmt.Println(v.Loc)
			}
		} else {
			if *max > 0 {
				go maxStopper()
			}
			PrimeUrlset(urlset)
		}
	}
}
