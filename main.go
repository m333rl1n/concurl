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

	var stdout bool
	flag.BoolVar(&stdout, "stdout", false, "Print responses in stdout")

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

				if !doSave && !stdout {
					// send HTTP request with HEAD method when we don't want save responses
					args = append(args, "-X", "HEAD", "-w", "%{http_code}", "-o", "/dev/null")
				}

				// pass all the arguments on to curl
				args = append(args, flag.Args()...)
				cmd := exec.Command("curl", args...)

				out, err := cmd.CombinedOutput()
				if err != nil && err.Error() != "exit status 18" {
					fmt.Fprintf(os.Stderr, "[%s] failed to get output %s\n", err, u)
					continue
				}

				if stdout {
					buf := &bytes.Buffer{}
					if add_command {
						buf.WriteString("$ curl ")
						buf.WriteString(u + "\n")
					}
					buf.Write(out)
					buf.WriteString("\n\n")

					fmt.Print(buf.String())
					continue
				}

				if !doSave {
					fmt.Println(u + "  " + string(out))
					continue
				}

				// use a hash of the URL and the arguments as the filename
				filename := fmt.Sprintf("%x", sha1.Sum([]byte(u)))
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
					buf.WriteString(u + "\n")
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
