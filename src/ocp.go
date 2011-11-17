package main

import (
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"log"
	"http"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"url"
	"xml"
)

const (
	version   = "2.1"
	useragent = "Optimus Cache Prime/" + version + " (http://patrickmylund.com/projects/ocp/)"
)

var (
	sem    chan bool
	wg     = &sync.WaitGroup{}
	client = http.DefaultClient

	throttle    *uint   = flag.Uint("c", 5, "pages to prime at once")
	localDir    *string = flag.String("l", "", "directory containing cached files (relative file names, i.e. /about/ -> <path>/about/index.html)")
	localSuffix *string = flag.String("ls", "index.html", "suffix of locally cached files")
	verbose     *bool   = flag.Bool("v", false, "show additional information about the priming process")
	nowarn      *bool   = flag.Bool("no-warn", false, "do not warn about pages that can't be loaded")
)

type Sitemap struct {
	Loc string
	// Lastmod string
}

type Url struct {
	Loc string
	// Lastmod string
	// Changefreq string
	Priority float64
}

type Urlset struct {
	XMLName xml.Name "urlset"
	Sitemap []Sitemap
	Url     []Url
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

func Get(url string) (r *http.Response, err os.Error) {
	req, err := http.NewRequest("GET", url, nil)
	req.Header.Add("User-Agent", useragent)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

func GetUrlsFromSitemap(path string, follow bool) (*Urlset, os.Error) {
	var (
		urlset Urlset
		f      io.ReadCloser
		err    os.Error
		res    *http.Response
	)
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		if *verbose {
			log.Println("Downloading", path)
		}
		res, err = Get(path)
		if res.Status != "200 OK" {
			return nil, os.NewError("Could not fetch sitemap: " + res.Status)
		}
		f = res.Body
	} else {
		f, err = os.Open(path)
	}
	defer f.Close()
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(path, ".gz") {
		if *verbose {
			log.Println("Extracting compressed data")
		}
		f, err = gzip.NewReader(f)
		defer f.Close()
		if err != nil {
			return nil, os.NewError("Gzip decompression failed")
		}
	}
	err = xml.Unmarshal(f, &urlset)
	if err == nil && follow && len(urlset.Sitemap) > 0 { // This is a sitemapindex
		ch := make(chan *Urlset, len(urlset.Sitemap))
		if *verbose {
			log.Printf("%s is a Sitemapindex\n", path)
		}
		for _, v := range urlset.Sitemap {
			sem <- true
			if *verbose {
				log.Printf("Adding URLs from child sitemap %s\n", v)
			}
			go func(loc string) {
				// Follow is false as Sitemapindex spec says sitemapindex children are illegal
				ourlset, err := GetUrlsFromSitemap(loc, false)
				if err != nil {
					log.Printf("Error getting Urlset from sitemap %s: %s", loc, err)
				} else {
					ch <- ourlset
				}
				<-sem
			}(v.Loc)
		}
		// Add every URL from each Urlset to the main Urlset
		for i := 0; i < len(urlset.Sitemap); i++ {
			childUrlset := <-ch
			urlset.Url = append(urlset.Url, childUrlset.Url...)
		}
	}
	return &urlset, err
}

func PrimeUrlset(urlset *Urlset) {
	if *verbose {
		log.Println("URLs to prime:", urlset.Len())
	}
	for _, u := range urlset.Url {
		sem <- true
		wg.Add(1)
		go PrimeUrl(u)
	}
	wg.Wait()
}

func PrimeUrl(u Url) os.Error {
	var (
		err   os.Error
		found bool = false
	)
	if *verbose {
		log.Printf("Get (weight %d) %s\n", int(u.Priority*100), u.Loc)
	}
	if *localDir != "" {
		parsed, err := url.ParseWithReference(u.Loc)
		joined := path.Join(*localDir, parsed.Path, *localSuffix)
		if _, err = os.Lstat(joined); err == nil {
			found = true
		}
	}
	if !found {
		res, err := Get(u.Loc)
		if (err != nil || res.Status != "200 OK") && !*nowarn {
			var errmsg string
			if err != nil {
				errmsg = err.String()
			} else {
				errmsg = res.Status
			}
			log.Printf("Error priming %s: %s\n", u.Loc, errmsg)
		}
	}
	wg.Done()
	<-sem
	return err
}

func main() {
	var (
		urlset *Urlset
		err    os.Error
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
		fmt.Println(" ", os.Args[0], "-l /var/www/mysite.com/wp-content/cache/supercache/ http://mysite.com/sitemap.xml")
		fmt.Println(" ", os.Args[0], "-l /var/www/mysite.com/wp-content/w3tc/pgcache/ -ls _index.html http://mysite.com/sitemap.xml")
		fmt.Println("")
		fmt.Println("If specifying a sitemap URL, make sure to prepend http:// or https://")
		return
	}
	sem = make(chan bool, *throttle)
	path := flag.Arg(0)
	urlset, err = GetUrlsFromSitemap(path, true)
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		sort.Sort(urlset)
		PrimeUrlset(urlset)
	}
}
