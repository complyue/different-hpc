package main

import (
	"flag"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/complyue/different-hpc/pkg/bknd"
	"github.com/complyue/hbi/pkg/errors"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
	yaml "gopkg.in/yaml.v2"
)

func main() {
	var err error
	defer func() {
		if e := recover(); e != nil {
			err = errors.RichError(e)
		}
		if err != nil {
			glog.Error(errors.RichError(err))
		}
	}()

	flag.Parse()

	type WebCfg struct {
		HTTP, HTTPS string
	}
	var webCfg WebCfg
	webRawYaml, err := ioutil.ReadFile("etc/web.yaml")
	if err != nil {
		cwd, _ := os.Getwd()
		panic(errors.Wrapf(err, "Can NOT read etc/web.yaml`, [%s] may not be the right directory ?\n", cwd))
	}
	err = yaml.Unmarshal(webRawYaml, &webCfg)
	if err != nil {
		return
	}

	router := mux.NewRouter()

	bknd.DefineApiRoutes(router)

	router.PathPrefix("/static/").Handler(
		http.StripPrefix("/static/", http.FileServer(http.Dir("./web/static"))),
	)

	bknd.DefinePageRoutes(router)

	srv := &http.Server{
		Handler:      router,
		Addr:         webCfg.HTTP,
		WriteTimeout: 30 * time.Second,
		ReadTimeout:  30 * time.Second,
	}

	addr := srv.Addr
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return
	}

	glog.Infof("Different HPC Control Center web serving at http://%s ...\n", ln.Addr())
	err = srv.Serve(tcpKeepAliveListener{ln.(*net.TCPListener)})
}

// following copied from std lib as unexported

// tcpKeepAliveListener sets TCP keep-alive timeouts on accepted
// connections. It's used by ListenAndServe and ListenAndServeTLS so
// dead TCP connections (e.g. closing laptop mid-download) eventually
// go away.
type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln tcpKeepAliveListener) Accept() (net.Conn, error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return nil, err
	}
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(3 * time.Minute)
	return tc, nil
}

func init() {
	// change glog default destination to stderr
	if glog.V(0) { // should always be true, mention glog so it defines its flags before we change them
		if err := flag.CommandLine.Set("logtostderr", "true"); nil != err {
			log.Printf("Failed changing glog default desitination, err: %s", err)
		}
	}
	flag.BoolVar(&bknd.DevMode, "dev", false, "Run in development mode.")
}
