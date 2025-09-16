package varnishtest

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

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
	conn     net.Conn
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
	sock, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}

	name := fmt.Sprintf("/tmp/varnishtest-go.%s", uuid.NewString())

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
		"-M", sock.Addr().String(),
	}
	for _, p := range uv.parameters {
		args = append(args, p.name, p.value)
	}

	cmd := exec.Command("varnishd",
		args...,
	)

	err = cmd.Start()
	if err != nil {
		log.Fatal(err)
	}

	// wait for the cli message
	conn, err := sock.Accept()
	if err != nil {
		panic(err)
	}

	varnish := Varnish{
		cmd:  cmd,
		name: name,
		conn: conn,
	}
	status, nonce, err := varnish.readCliMessage()
	if status != 107 {
		panic("status should have been 107")
	}
	fmt.Printf("nonce is: %s\n", nonce)
	if len(nonce) < 32 {
		panic("nonce too short")
	}

	secret, err := os.ReadFile(filepath.Join(name, "_.secret"))
	if err != nil {
		log.Fatal(err)
	}
	hasher := sha256.New()
	hasher.Write(nonce[:32])
	hasher.Write([]byte("\n"))
	hasher.Write(secret)
	hasher.Write(nonce[:32])
	hasher.Write([]byte("\n"))

	status, _, err = varnish.Adm("auth", hex.EncodeToString(hasher.Sum(nil)))
	if status != 200 {
		panic("status should have been 200")
	}
	if err != nil {
		log.Fatal(err)
        }

	var byt []byte
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
		_, byt, err = varnish.Adm("vcl.inline", "vcl1 << XXYYZZ\n", vcl, "\nXXYYZZ")
		if err != nil {
			log.Fatalf("%s\n%s\n", string(byt), err)
		}
	} else {
		_, byt, err = varnish.Adm("vcl.load", "vcl1", uv.vclString)
		if err != nil {
			log.Fatalf("%s\n%s\n", string(byt), err)
		}
	}

	_, byt, err = varnish.Adm("vcl.use", "vcl1")
	if err != nil {
		log.Fatalf("%s\n%s\n", string(byt), err)
	}
	_, byt, err = varnish.Adm("start")
	if err != nil {
		log.Fatalf("%s\n%s\n", string(byt), err)
	}

	varnish.WaitRunning()

	return varnish
}

func (varnish *Varnish) WaitRunning() error {
	status, resp, err := varnish.Adm("status")
	for {
		if err != nil {
			return err
		}
		if status != 200 {
			return fmt.Errorf(`"status" request got a %d response:\n%s`, status, string(resp))
		}
		if string(resp) == "Child in state stopped" {
			return fmt.Errorf("Child stopped before running")
		}
		if string(resp) == "Child in state running\n" {
			status, resp, err = varnish.Adm("debug.listen_address")
			if err != nil {
				return err
			}

			var name string
			var addr string
			var port int
			_, err := fmt.Sscanf(string(resp), "%s %s %d\n", &name, &addr, &port)
			if err != nil {
				return err
			}
			// FIXME: IPv6
			varnish.URL = fmt.Sprintf("http://%s:%d", addr, port)
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}

func (varnish *Varnish) readCliMessage() (int, []byte, error) {
	status := 0
	sz := 0
	_, err := fmt.Fscanf(varnish.conn, "%d %d\n", &status, &sz)
	if err != nil {
		return 0, []byte{}, err
	}
	buf := make([]byte, sz + 1)

	_, err = varnish.conn.Read(buf)
	if err != nil {
		return 0, []byte{}, err
	}

	return status, buf, nil
}

func (varnish *Varnish) Adm(args ...string) (int, []byte, error) {
	_, err := varnish.conn.Write([]byte(strings.Join(args, " ") + "\n"))
	if err != nil {
		return 0, []byte{}, err
	}
	return varnish.readCliMessage()
}

func (varnish *Varnish) Close() {
	varnish.Adm("stop")
	varnish.conn.Close()

	if err := varnish.cmd.Process.Kill(); err != nil {
		log.Printf("failed to kill process: %s\n", err)
	}

	os.RemoveAll(varnish.name)
}
