package bknd

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/complyue/different-hpc/pkg/ccm"
	"github.com/complyue/hbigo/pkg/errors"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
)

func pixieApi(w http.ResponseWriter, r *http.Request) {
	glog.V(1).Infof("Serving boot config for %s", filepath.Base(r.URL.Path))

	vars := mux.Vars(r)
	mac := vars["mac"]

	cnCfg, err := ccm.PrepareComputeNodeCfg(mac)
	if err != nil {
		panic(err)
	}

	jsonResult := make(map[string]interface{}, 5)

	cfgData := cnCfg.Inflate()
	switch kernel := cfgData["kernel"].(type) {
	case string:
		jsonResult["kernel"] = kernel
	default:
		panic(errors.Errorf("Invalid kernel of type %T - %#v", kernel, kernel))
	}
	switch initrd := cfgData["initrd"].(type) {
	case []string:
		jsonResult["initrd"] = initrd
	case string:
		jsonResult["initrd"] = []string{initrd}
	default:
		panic(errors.Errorf("Invalid initrd of type %T - %#v", initrd, initrd))
	}
	switch cmdline := cfgData["cmdline"].(type) {
	case []string:
		jsonResult["cmdline"] = strings.Join(cmdline, " ")
	case string:
		jsonResult["cmdline"] = cmdline
	default:
		panic(errors.Errorf("Invalid cmdline of type %T - %#v", cmdline, cmdline))
	}

	if err := json.NewEncoder(w).Encode(jsonResult); err != nil {
		panic(err)
	}
}
