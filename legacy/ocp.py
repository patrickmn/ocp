#!/usr/bin/env python
"""
  Optimus Cache Prime
  Reasonably smart cache primer for websites with XML sitemaps

  OCP crawls all URLs listed in the sitemap based on their set priority. With
  Local mode enabled the local cache is probed first, and only pages that aren't
  already cached are crawled, reducing the amount of web requests significantly.

  The Local mode was designed for use with W3 Total Cache and WP Super Cache for
  WordPress, but will work with any system that uses a URL-relative flat file cache.

  Version 1.1
  by Patrick Mylund Nielsen
  http://patrickmylund.com/projects/ocp/

  ---

  Optimus Cache Prime is released under the MIT License:
 
  Copyright (c) 2010 Patrick Mylund Nielsen
 
  Permission is hereby granted, free of charge, to any person obtaining a copy
  of this software and associated documentation files (the "Software"), to deal
  in the Software without restriction, including without limitation the rights
  to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
  copies of the Software, and to permit persons to whom the Software is
  furnished to do so, subject to the following conditions:
 
  The above copyright notice and this permission notice shall be included in
  all copies or substantial portions of the Software.
 
  THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
  IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
  FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
  AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
  LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
  OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
  THE SOFTWARE.
"""

__version__ = '1.1'

import sys
import os
import time
import urllib2
import operator
import gzip
import StringIO
from xml.etree.ElementTree import ElementTree

###########################
### Begin configuration ###
###########################

# URL or local path of a sitemap containing the URLs to crawl. This must
# be uncommented and defined to use Local mode. The sitemap may be gzipped.
#
# sitemap = '/var/www/patrickmylund.com/blog/sitemap.xml'
# sitemap = 'http://patrickmylund.com/blog/sitemap.xml'

# Delay (in seconds) between requests when crawling. Increase to reduce web
# server load. Note that OCP never performs more than one request at once,
# so the load should be negligible even with no throttling.
crawl_delay = 0

# Local mode
#   True : The local page cache is probed and only unprimed pages are crawled.
#   False: All URLs in the sitemap are crawled, and the settings below are ignored.
local = False

# Base URL string (Local mode)
#   The part of the URLs from the sitemap which is not included in the local
#   cache file structure, e.g. http://yourdomain.com or http://yourdomain.com/blog
#   For WP Super Cache, this is just 'http://' as it also creates a folder with
#   the name of your domain locally.
url_base = 'http://patrickmylund.com/blog'

# Local cache path (Local mode)
#   The root of the static file cache folder structure.
#
#   W3 Total Cache: /path/to/wordpress/wp-content/w3tc/pgcache
#   WP Super Cache: /path/to/wordpress/wp-content/cache/supercache
cache_dir = '/var/www/patrickmylund.com/blog/wp-content/w3tc/pgcache'

# Optional: Local cache index/suffix file (Local mode)
#   The name of the file that is created in the "mirror" folders, e.g.
#   about/index.html. This is -optional- for most setups since OCP also checks
#   if the folder 'about' exists.
#
#   W3 Total Cache: _index.html
#   WP Super Cache: index.html
#
# cache_file = '_index.html'

###########################
###  End configuration  ###
###########################

crawler = urllib2.build_opener()
crawler.addheaders = [
    ('User-Agent', 'Optimus Cache Prime/%s' % __version__),
    ('Accept-encoding', 'gzip')
]

def main():
    if local and not os.path.isdir(cache_dir):
        sys.exit("The folder %s doesn't seem to exist. Please ensure that cache_dir points to the base of the local file cache." % cache_dir)
    urls = getSitemapUrls()
    crawlUrls(urls)

def getSitemapUrls():
    urlmap = {}
    tree = ElementTree()
    try:
        if os.path.isfile(sitemap):
            try:
                tree.parse(sitemap)
            except:
                f = open(sitemap, 'r')
                tree.parse(gzip.GzipFile(fileobj=f))
        else:
            tree.parse(getUrlFile(sitemap))
    except Exception, e:
        sys.exit("Couldn't parse %s. Is it a valid sitemap XML file?\n\rInclude 'http://' when pointing to an online sitemap.\n\r\n\rError: %s" % (sitemap, e))
    doc = tree.getroot()
    ns = doc.tag[1:].partition('}')[0]
    # An iterfind object would be much better than findall(), but it's only available in Python 2.7+
    for x in doc.findall('{%s}url' % ns):
        url = x.find('{%s}loc' % ns)
        prio = x.find('{%s}priority' % ns)
        urlmap[url.text] = prio.text if prio is not None else '0.0'
    return [x[0] for x in sorted(urlmap.iteritems(), key=operator.itemgetter(1), reverse=True)]

def isPrimed(url):
    relative_path = url.partition(url_base)[2]
    if relative_path[0] == '/':
        relative_path = relative_path[1:]
    return os.path.exists(os.path.join(cache_dir, relative_path, cache_file if 'cache_file' in globals() else ''))

def crawlUrls(urls):
    for url in urls:
        if local and isPrimed(url):
            continue
        try:
            crawlUrl(url)
        except Exception, e:
            print "Couldn't crawl %s. Error: %s" % (url, e)
        if crawl_delay:
            time.sleep(crawl_delay)

def crawlUrl(url):
    crawler.open(url).close()

def getUrlFile(url):
    f = crawler.open(url)
    if hasattr(f, 'headers') and f.headers.get('content-encoding', '') == 'gzip':
        data = f.read()
        # Data may be compressed multiple times!
        while True:
            data_f = StringIO.StringIO(data)
            try:
                data = gzip.GzipFile(fileobj=data_f).read()
            except Exception:
                break
        return StringIO.StringIO(data)
    else:
        return f

if __name__ == '__main__':
    args = len(sys.argv) - 1
    if args > 1 or not args and not 'sitemap' in globals():
        print """Usage: %s <path/url to sitemap.xml>\r
\r
You can run OCP without any arguments after configuring the settings inside %s.""" % (sys.argv[0], sys.argv[0])
    else:
        if args:
            sitemap = sys.argv[1]
            local = False
        main()
