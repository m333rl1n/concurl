package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"flag"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func main() {
	var concurrency int
	flag.IntVar(&concurrency, "c", 10, "Concurrency level")

	var delay int
	flag.IntVar(&delay, "d", 5000, "Milliseconds between requests to the same domain")

	var outputDir string
	flag.StringVar(&outputDir, "o", "out", "Output directory")

	var doSave bool
	flag.BoolVar(&doSave, "save", false, "Save responses")

	var add_command bool
	flag.BoolVar(&add_command, "cmd", false, "Print executed command in output")

	flag.Parse()

	// channel to send URLs to workers
	jobs := make(chan string)

	rl := newRateLimiter(time.Duration(delay * 1000000))

	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)

		go func() {
			for u := range jobs {

				// get the domain for use in the path
				// and for rate limiting
				domain := "unknown"
				parsed, err := url.Parse(u)
				if err == nil {
					domain = parsed.Hostname()
				}

				// rate limit requests to the same domain
				rl.Block(domain)

				// we need the silent flag to get rid
				// of the progress output
				args := []string{u, "--silent"}

				if !doSave {
					// send HTTP request with HEAD method when we don't want save responses
					args = append(args, "--head")
				}

				// pass all the arguments on to curl
				args = append(args, flag.Args()...)
				cmd := exec.Command("curl", args...)

				out, err := cmd.CombinedOutput()
				if err != nil {
					fmt.Fprintf(os.Stderr, "[%s] failed to get output %s\n", err, u)
					continue
				}

				if !doSave {
					fmt.Println(u)
					continue
				}

				// use a hash of the URL and the arguments as the filename
				filename := fmt.Sprintf("%x", sha1.Sum([]byte(u+strings.Join(args, " "))))
				p := filepath.Join(outputDir, domain, filename)

				if _, err := os.Stat(path.Dir(p)); os.IsNotExist(err) {
					err = os.MkdirAll(path.Dir(p), 0755)
					if err != nil {
						fmt.Fprintf(os.Stderr, "[%s] failed to create output dir\n", err)
						continue
					}
				}

				// include the command at the top of the output file
				buf := &bytes.Buffer{}
				if add_command {
					buf.WriteString("$ curl ")
					buf.WriteString(strings.Join(args, " ") + "\n")
				}
				buf.Write(out)

				err = ioutil.WriteFile(p, buf.Bytes(), 0644)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[%s] failed to save output \"%s\"\n", err, p)
					continue
				}

				fmt.Printf("%s %s\n", p, u)
			}

			wg.Done()
		}()
	}

	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		// send each line (a domain) on the jobs channel
		jobs <- sc.Text()
	}

	close(jobs)
	wg.Wait()
}
