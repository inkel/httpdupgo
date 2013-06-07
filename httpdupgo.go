package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	Version = "0.0.1"
)

type remotes []string

var (
	listen, server  string
	duplicates      remotes
	sticky, verbose bool
)

func (rs *remotes) Set(value string) error {
	for _, r := range strings.Split(value, ",") {
		*rs = append(*rs, r)
	}
	return nil
}

func (rs *remotes) String() string {
	return strings.Join(duplicates, ", ")
}

func init() {
	const (
		defaultListen = "0.0.0.0:8080"
		defaultServer = "127.0.0.1:80"

		listenDesc  = "Address to listen for connections"
		serverDesc  = "Address of the server whose traffic will be duplicated"
		stickyDesc  = "Sticky connections"
		remotesDesc = "comma-separated list of remote servers address"
	)

	flag.StringVar(&listen, "l", defaultListen, listenDesc)

	flag.StringVar(&server, "s", defaultServer, serverDesc)

	flag.BoolVar(&sticky, "S", false, stickyDesc)

	flag.Var(&duplicates, "r", remotesDesc)
}

type Duplicator struct {
	listen, server  string
	duplicates      remotes
	sticky, verbose bool
}

func (d *Duplicator) debug(format string, args ...interface{}) {
	if d.verbose {
		fmt.Printf(format, args...)
	}
}

func (d *Duplicator) sendError(w http.ResponseWriter, err error) {
	fmt.Println("ERR", err)
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func (d *Duplicator) ListenAndServe() error {
	return http.ListenAndServe(d.listen, d)
}

func copyHeaders(from http.Header, to http.Header) {
	for header, values := range from {
		for _, value := range values {
			to.Add(header, value)
		}
	}
}

func createProxyRequest(dst string, o *http.Request) (*http.Request, error) {
	endpoint, err := url.Parse(dst)

	if err != nil {
		return nil, err
	}

	endpoint.Path = o.URL.Path

	request, err := http.NewRequest(o.Method, endpoint.String(), o.Body)

	if err != nil {
		return nil, err
	}

	request.Close = true

	copyHeaders(o.Header, request.Header)

	for param, values := range o.Form {
		for _, value := range values {
			request.Form.Add(param, value)
		}
	}

	return request, nil
}

func (d *Duplicator) duplicate(dst string, req *http.Request) (*http.Response, error) {
	nreq, err := createProxyRequest(dst, req)
	if err != nil {
		return nil, err
	}
	d.debug("> %s %s %s\n", nreq.Method, nreq.URL.String(), nreq.Proto)

	tr := http.DefaultTransport

	start := time.Now()

	res, err := tr.RoundTrip(nreq)
	if err != nil {
		return nil, err
	}

	dur := time.Since(start)
	d.debug("< %d %s %s %s\n", res.StatusCode, nreq.Method, nreq.URL.String(), dur)

	return res, nil
}

func (d *Duplicator) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// d.debug("> %s %s %s\n", req.Method, req.URL, req.Proto)

	res, err := d.duplicate(d.server, req)
	if err != nil {
		d.sendError(w, err)
	}

	for _, r := range d.duplicates {
		go func() {
			_, err := d.duplicate(r, req)
			if err != nil {
				fmt.Println("E", r, err)
			}
		}()
	}

	for hdr, vals := range res.Header {
		for _, val := range vals {
			w.Header().Add(hdr, val)
		}
	}

	w.WriteHeader(res.StatusCode)

	io.Copy(w, res.Body)

	res.Body.Close()
}

func main() {
	flag.Parse()

	fmt.Printf("httpdupgo v%s\nserver %s\nlistening %s\n", Version, server, listen)

	n := len(duplicates)
	fmt.Printf("duplicating connections to %d servers\n", n)

	if n > 0 {
		for _, d := range duplicates {
			fmt.Println("duplicating to", d)
		}
	}

	dup := &Duplicator{listen, server, duplicates, sticky, !false}

	if err := dup.ListenAndServe(); err != nil {
		panic(err)
	}
}
