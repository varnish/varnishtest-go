package varnishtest

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSynth(t *testing.T) {

	// just a simple VCL with a synthetic response
	varnish := New().VclString(`
		backend default none;

		sub vcl_recv {
			return(synth(200, "Good test"));
		}
	`).Start()
	defer varnish.Close()

	// use a regular client to send a request
	resp, err := http.Get(varnish.URL + "/test")

	// test the response using generic go facilities
	if err != nil {
		t.Error(err)
		return
	}

	if resp.Status != "200 Good test" {
		t.Errorf(`expected "200 Good test", got %s`, resp.Status)
	}
}

func TestBackend(t *testing.T) {
	// create a test backend
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "this is my body")
	}))

	// add the backend definition to the loaded VCL
	varnish := New().Backend("svr", svr.URL).Start()
	defer varnish.Close()

	resp, err := http.Get(varnish.URL)
	if err != nil {
		t.Error(err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Error(err)
		return
	}
	if string(body) != "this is my body" {
		t.Errorf(`expected "200 Good test", got %s`, body)
	}
}

func TestRouting(t *testing.T) {
	// create a test backend
	svrA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("server", "A")
	}))
	svrB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("server", "B")
	}))

	// add the backend definition to the loaded VCL
	varnish := New().
			Backend("svrA", svrA.URL).
			Backend("svrB", svrB.URL).
			VclString(`
				sub vcl_recv {
					if (req.url == "/A") {
						set req.backend_hint = svrA;
					} else {
						set req.backend_hint = svrB;
					}
				}
	`).Start()
	defer varnish.Close()

	resp, err := http.Get(varnish.URL + "/A")
	if err != nil {
		t.Error(err)
		return
	}
	resp.Body.Close()

	if resp.Header.Get("server") != "A" {
		t.Errorf(`expected "A", got %s`, resp.Header.Get("server"))
		return
	}

	resp, err = http.Get(varnish.URL + "/B")
	if err != nil {
		t.Error(err)
		return
	}
	resp.Body.Close()

	if resp.Header.Get("server") != "B" {
		t.Errorf(`expected "B", got %s`, resp.Header.Get("server"))
		return
	}
}
