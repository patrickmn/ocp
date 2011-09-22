package main

import (
	"compress/gzip"
	"flag"
	"fmt"
	"http"
	"io"
	"log"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"url"
	"xml"
)

const (
	version string = "2.1"
)

var (
	sem chan bool
	wg = &sync.WaitGroup{}
	client = http.DefaultClient

	throttle    *uint   = flag.Uint("c", 1, "pages to prime at once")
	localDir    *string = flag.String("l", "", "directory containing cached files (relative file names, i.e. /about/ -> <path>/about/index.html)")
	localSuffix *string = flag.String("ls", "index.html", "suffix of locally cached files")
	verbose     *bool   = flag.Bool("v", false, "show information about primed pages")
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

func GetUrlsFromSitemap(path string, follow bool) (*Urlset, os.Error) {
	var (
		urlset Urlset
		f      io.ReadCloser
		err    os.Error
	)
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		var res *http.Response
		res, err = client.Get(path)
		if res.Status == "404 Not Found" {
			return &Urlset{}, os.NewError("Web server returned 404 Not Found")
		} else if res.Status != "200 OK" {
			return &Urlset{}, os.NewError("Web server did not serve the file")
		} else {
			f = res.Body
		}
	} else {
		f, err = os.Open(path)
	}
	defer f.Close()
	if err != nil {
		return &Urlset{}, err
	}
	if strings.HasSuffix(path, ".gz") {
		f, err = gzip.NewReader(f)
		defer f.Close()
		if err != nil {
			return &Urlset{}, os.NewError("Gzip decompression failed")
		}
	}
	err = xml.Unmarshal(f, &urlset)
	if err == nil && follow && len(urlset.Sitemap) > 0 { // This is a sitemapindex
		ch := make(chan []Url, len(urlset.Sitemap))
		if *verbose {
			log.Printf("%s is a Sitemapindex. Fetching Urlsets from sitemaps...\n", path)
		}
		for _, v := range urlset.Sitemap {
			if *verbose {
				log.Printf("Adding Urlset from sitemap %s\n", v.Loc)
			}
			sem <- true
			go func(loc string) {
				// Follow is false as Sitemapindex spec says sitemapindex children are illegal
				ourlset, err := GetUrlsFromSitemap(loc, false)
				if err != nil {
					fmt.Printf("Error getting Urlset from sitemap %s: %s", loc, err)
				} else {
					ch <- ourlset.Url
				}
				<-sem
			}(v.Loc)
		}
		// Add every URL from each Urlset to the main Urlset
		for i := 0; i < len(urlset.Sitemap); i++ {
			v := <-ch
			for _, ov := range v {
				urlset.Url = append(urlset.Url, ov)
			}
		}
	}
	return &urlset, err
}

func PrimeUrlset(urlset *Urlset) {
	if *verbose {
		log.Println("URLs in sitemap: ", urlset.Len())
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
		log.Printf("%s (weight %d)\n", u.Loc, int(u.Priority*100))
	}
	if *localDir != "" {
		parsed, err := url.ParseWithReference(u.Loc)
		joined := path.Join(*localDir, parsed.Path, *localSuffix)
		if _, err = os.Lstat(joined); err == nil {
			found = true
		}
	}
	if !found {
		_, err = client.Get(u.Loc)
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
	fmt.Println("THrottle is: ", *throttle)
	path := flag.Arg(0)
	urlset, err = GetUrlsFromSitemap(path, true)
	if err != nil {
		fmt.Println(err)
	} else {
		sort.Sort(urlset)
		PrimeUrlset(urlset)
	}
}
