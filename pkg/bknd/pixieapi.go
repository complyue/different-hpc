package bknd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/complyue/different-hpc/pkg/ccm"
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

	buf := bytes.NewBuffer(nil)
	ctx := make(map[string]interface{}, 20)
	ctx["ip"] = cnCfg.IP
	ctx["ipnum"] = fmt.Sprintf("%v", cnCfg.IPNum)
	for _, cfgItem := range cnCfg.CfgYaml {
		if cfgKey, ok := cfgItem.Key.(string); ok {
			if cfgVal, ok := cfgItem.Value.(string); ok {
				vt := template.Must(template.New(
					"Value of " + cfgKey,
				).Parse(cfgVal))
				buf.Reset()
				if err := vt.Execute(buf, ctx); err != nil {
					panic(err)
				}
				val := buf.String()
				ctx[cfgKey] = val

				if "kernel" == cfgKey {
					jsonResult[cfgKey] = val
				} else if "initrd" == cfgKey {
					jsonResult[cfgKey] = []string{val}
				}
			} else if cfgSeq, ok := cfgItem.Value.([]interface{}); ok {
				seqStrs := make([]string, 0, len(cfgSeq))
				for seqI, seqElem := range cfgSeq {
					if seqStr, ok := seqElem.(string); ok {
						vt := template.Must(template.New(
							fmt.Sprintf("Value of %s:%v", cfgKey, seqI+1),
						).Parse(seqStr))
						buf.Reset()
						if err := vt.Execute(buf, ctx); err != nil {
							panic(err)
						}
						val := buf.String()
						if len(val) > 0 {
							seqStrs = append(seqStrs, val)
						}
					}
				}
				ctx[cfgKey] = seqStrs

				if "cmdline" == cfgKey {
					jsonResult[cfgKey] = strings.Join(seqStrs, " ")
				} else if "initrd" == cfgKey {
					jsonResult[cfgKey] = seqStrs
				}
			}
		}
	}

	if err := json.NewEncoder(w).Encode(jsonResult); err != nil {
		panic(err)
	}
}
