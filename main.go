package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func main() {
	os.Exit(mainCLI())
}

func mainCLI() int {
	var (
		addrs     string
		outputDir string
	)

	flag.StringVar(&addrs, "addrs", "", "File path for the text file cointains list of URLs (required)")
	flag.StringVar(&outputDir, "output", ".", "output directory")
	flag.Parse()

	if addrs == "" {
		fmt.Fprintf(os.Stderr, "-addrs is required")
		return 1
	}

	addrsF, err := os.Open(addrs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening addrs file: "+err.Error())
		return 1
	}

	if err := do(addrsF, outputDir); err != nil {
		fmt.Fprintf(os.Stderr, "error: "+err.Error())
		return 1
	}

	return 0
}

func do(input io.Reader, outputDir string) error {
	strm := consumeInput(input)
	strm = consumeQueue(strm, outputDir)

	for q := range strm {
		fmt.Println(q.URL, q.FaviconURL)
	}

	return nil
}

func consumeInput(input io.Reader) chan Queue {
	next := make(chan Queue)
	go func() {
		defer close(next)

		// Read input line by line
		scanner := bufio.NewScanner(input)
		for scanner.Scan() {
			raw := scanner.Text()
			if raw == "" {
				continue
			}
			u, err := url.Parse(raw)
			if err != nil {
				next <- Queue{Error: err}
				return
			}

			next <- Queue{URL: raw, FileNamePrefix: u.Hostname() + "-"}
		}

		if err := scanner.Err(); err != nil {
			next <- Queue{Error: err}
			return
		}
	}()
	return next
}

func consumeQueue(in chan Queue, outDir string) chan Queue {
	next := make(chan Queue)
	go func() {
		defer close(next)

		wg := sync.WaitGroup{}

		for q := range in {
			q := q
			wg.Add(1)
			go func() {
				defer wg.Done()
				if q.Error != nil {
					next <- q
					return
				}

				q.FaviconURL, q.Error = fetchAndFindFaviconURL(q.URL)
				if q.Error != nil {
					next <- q
					return
				}

				q.LocalFilepath, q.Error = saveFavicon(q.FaviconURL, q.FileNamePrefix, outDir)
				next <- q
			}()
		}

		wg.Wait()
	}()
	return next
}

// Queue : job queue
type Queue struct {
	URL            string
	FileNamePrefix string
	Error          error

	FaviconURL    []string
	LocalFilepath []string
}

func fetchAndFindFaviconURL(baseURL string) ([]string, error) {
	resp, err := http.Get(baseURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	metaTags := []string{}
	idx := 0
	content := strings.Replace(string(b), `'`, `"`, -1)
	startTag := "<link"
	endTag := ">"

	for {
		startIdx := strings.Index(content[idx:], startTag)
		if startIdx == -1 {
			break
		}
		idx = startIdx + idx + len(startTag)
		cidx := strings.Index(content[idx:], endTag)
		if cidx == -1 {
			break
		}
		cidx = cidx + idx + len(endTag)
		metaTags = append(metaTags, content[idx-len(startTag):cidx])
		idx = cidx
	}

	faviconURLs := make(map[string]struct{})
	for _, t := range metaTags {
		if strings.Index(t, `"icon`) != -1 || strings.Index(t, `"shortcut icon"`) != -1 {
			refStart := strings.Index(t, `href="`)
			if refStart == -1 {
				continue
			}
			refStart = refStart + 6
			refEnd := strings.Index(t[refStart:], `"`)
			if refEnd == -1 {
				continue
			}
			faviconURLs[t[refStart:refStart+refEnd]] = struct{}{}
		}
	}

	ret := make([]string, 0, len(faviconURLs))
	for k := range faviconURLs {
		ret = append(ret, k)
	}

	return ret, nil
}

func saveFavicon(urls []string, fileNamePrefix string, outputDir string) ([]string, error) {
	results := []string{}

	for i, u := range urls {
		resp, err := http.Get(u)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		outPath := filepath.Join(outputDir, fileNamePrefix+path.Base(u))
		out, err := os.Create(outPath)
		if err != nil {
			return nil, err
		}
		defer out.Close()

		if _, err := io.Copy(out, resp.Body); err != nil {
			return nil, err
		}

		results = append(results, outPath)

		if len(urls) > i-1 {
			time.Sleep(time.Second)
		}
	}
	return nil, nil
}
