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
	version   = "2.4 beta"
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

func Get(url string) (*http.Response, os.Error) {
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
		<- one
		count++
		if count == *max {
			log.Fatal("Uncached page prime limit reached; stopping")
		}
	}
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
		fmt.Println(" ", os.Args[0], "-c 10 http://mysite.com/sitemap.xml.gz")
		fmt.Println(" ", os.Args[0], "-l /var/www/mysite.com/wp-content/cache/supercache/ http://mysite.com/sitemap.xml")
		fmt.Println(" ", os.Args[0], "-l /var/www/mysite.com/wp-content/w3tc/pgcache/ -ls _index.html http://mysite.com/sitemap.xml")
		fmt.Println(" ", os.Args[0], "--print http://mysite.com/sitemap.xml | xargs curl -I")
		fmt.Println("")
		fmt.Println("If specifying a sitemap URL, make sure to prepend http:// or https://")
		return
	}
	if *max > 0 {
		one = make(chan bool, *max)
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
