package main

import (
	"compress/gzip"
	"flag"
	"fmt"
	"http"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"xml"
)

type Url struct {
	Loc string
	// Lastmod string
	// Changefreq string
	Priority float64
}

type Urlset struct {
	XMLName xml.Name "urlset"
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

func GetUrlsFromSitemap(path string) (*Urlset, os.Error) {
	var (
		urlset Urlset
		f      io.ReadCloser
		err    os.Error
	)
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		var res *http.Response
		res, err = http.DefaultClient.Get(path)
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
	return &urlset, err
}

func PrimeUrlset(urlset *Urlset, sem chan bool) {
	wg := &sync.WaitGroup{}
	if *verbose {
		fmt.Println("URLs in sitemap: ", urlset.Len())
	}
	for _, url := range urlset.Url {
		sem <- true
		wg.Add(1)
		go PrimeUrl(url, sem, wg)
	}
	wg.Wait()
}

func PrimeUrl(url Url, sem <-chan bool, wg *sync.WaitGroup) os.Error {
	var (
		err os.Error
		found bool = false
	)
	if *verbose {
		fmt.Printf("%s (weight %d)\n", url.Loc, int(url.Priority*100))
	}
	if *localDir != "" {
		parsed, err := http.ParseURLReference(url.Loc)
		joined := path.Join(*localDir, parsed.Path, *localSuffix)
		if _, err = os.Lstat(joined); err == nil {
			found = true
		}
	}
	if !found {
		_, err = http.DefaultClient.Get(url.Loc)
	}
	wg.Done()
	<-sem
	return err
}

var (
	throttle    *uint   = flag.Uint("c", 1, "pages to prime at once")
	localDir    *string = flag.String("l", "", "directory containing cached files (relative file names, i.e. /about/ -> <path>/about/index.html)")
	localSuffix *string = flag.String("ls", "index.html", "suffix of locally cached files")
	verbose     *bool   = flag.Bool("v", false, "show information about primed pages")
)

func main() {
	var (
		urlset *Urlset
		err    os.Error
	)
	flag.Parse()
	if flag.NArg() == 0 {
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
	path := flag.Arg(0)
	urlset, err = GetUrlsFromSitemap(path)
	if err != nil {
		fmt.Println(err)
	} else {
		sort.Sort(urlset)
		sem := make(chan bool, *throttle)
		PrimeUrlset(urlset, sem)
	}
}
