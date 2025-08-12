package varnishtest

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"

	"github.com/google/uuid"
)

type parameter struct {
	name  string
	value string
}

type backend struct {
	name string
	host string
	port string
	ssl  bool
}

type UnstartedVarnish struct {
	vclIsFile  bool
	vclString  string
	vclVersion string

	parameters []parameter
	backends   []backend
}

type Varnish struct {
	URL string // base URL of form http://ipaddr:port with no trailing slash

	client *http.Client
	cmd    *exec.Cmd
	name   string
}

type socket struct {
	Endpoint string `json:"Endpoint"`
}

func New() *UnstartedVarnish {
	uv := &UnstartedVarnish{}
	uv.Vcl41()
	return uv
}

func (uv *UnstartedVarnish) VclString(s string) *UnstartedVarnish {
	uv.vclIsFile = false
	uv.vclString = s
	return uv
}

func (uv *UnstartedVarnish) VclFile(s string) *UnstartedVarnish {
	uv.vclIsFile = true
	uv.vclString = s
	return uv
}

func (uv *UnstartedVarnish) Parameter(name string, value string) *UnstartedVarnish {
	uv.parameters = append(uv.parameters, parameter{name: name, value: value})
	return uv
}

func (uv *UnstartedVarnish) Vcl41() *UnstartedVarnish {
	uv.vclVersion = "vcl 4.1;\n\n"
	return uv
}

func (uv *UnstartedVarnish) Vcl40() *UnstartedVarnish {
	uv.vclVersion = "vcl 4.0;\n\n"
	return uv
}

func (uv *UnstartedVarnish) VCLVersion() *UnstartedVarnish {
	uv.vclVersion = ""
	return uv
}

func (uv *UnstartedVarnish) Backend(name string, urlRaw string) *UnstartedVarnish {
	u, err := url.Parse(urlRaw)
	if err != nil {
		log.Fatal(err)
	}

	ssl := false
	port := u.Port()

	if u.Scheme == "https" {
		ssl = true
		if port == "" {
			port = "443"
		}
	} else if port == "" {
		port = "80"
	}

	host := u.Hostname()

	uv.backends = append(uv.backends, backend{
		name,
		host,
		port,
		ssl,
	})
	return uv
}

func (uv *UnstartedVarnish) Start() Varnish {
	name := uuid.NewString()

	args := []string{
		"-F",
		"-f", "",
		"-n", name,
		"-a", "127.0.0.1:0",
		"-p", "auto_restart=off",
		"-p", "syslog_cli_traffic=off",
		"-p", "thread_pool_min=10",
		"-p", "debug=+vtc_mode",
		"-p", "vsl_mask=+Debug,+H2RxHdr,+H2RxBody",
		"-p", "h2_initial_window_size=1m",
		"-p", "h2_rx_window_low_water=64k",
	}
	for _, p := range uv.parameters {
		args = append(args, p.name, p.value)
	}

	cmd := exec.Command("varnishd",
		args...,
	)

	varnish := Varnish{
		cmd:  cmd,
		name: name,
	}

	err := cmd.Start()
	if err != nil {
		log.Fatal(err)
	}

	// wait for Varnish to come online
	byt, err := varnish.Adm("ping")
	if err != nil {
		log.Fatalf("%s\n%s\n", string(byt), err)
	}

	if !uv.vclIsFile {
		backendString := ""
		for _, b := range uv.backends {
			backendString += fmt.Sprintf(`backend %s {
	.host = "%s";
	.port = "%s";
	.host_header = "%s";
}
`, b.name, b.host, b.port, b.host)
		}

		vcl := fmt.Sprintf("%s%s%s", uv.vclVersion, backendString, uv.vclString)
		byt, err = varnish.Adm("vcl.inline", "vcl1 << XXYYZZ\n", vcl, "\nXXYYZZ")
		if err != nil {
			log.Fatalf("%s\n%s\n", string(byt), err)
		}
	} else {
		byt, err = varnish.Adm("vcl.load", "vcl1", uv.vclString)
		if err != nil {
			log.Fatalf("%s\n%s\n", string(byt), err)
		}
	}

	byt, err = varnish.Adm("vcl.use", "vcl1")
	if err != nil {
		log.Fatalf("%s\n%s\n", string(byt), err)
	}
	byt, err = varnish.Adm("start")
	if err != nil {
		log.Fatalf("%s\n%s\n", string(byt), err)
	}

	// find a port
	byt, err = varnish.Adm("socket.list", "-j")
	if err != nil {
		log.Fatalf("%s\n%s\n", string(byt), err)
	}

	// one day, `varnishadm` will output proper JSON
	var fullResponse any
	err = json.Unmarshal(byt, &fullResponse)
	if err != nil {
		log.Fatal(err)
	}
	varnish.URL = fmt.Sprintf("http://%v", fullResponse.([]any)[3].([]any)[0].(map[string]any)["endpoint"])

	return varnish
}

func (varnish *Varnish) Adm(args ...string) ([]byte, error) {
	args = append([]string{"-n", varnish.name}, args...)
	c := exec.Command("varnishadm", args...)

	byt, err := c.Output()
	return byt, err
}

func (varnish *Varnish) Close() {
	varnish.Adm("stop")

	if err := varnish.cmd.Process.Kill(); err != nil {
		log.Printf("failed to kill process: %s\n", err)
	}

	os.RemoveAll(varnish.name)
}
