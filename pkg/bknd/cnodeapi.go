package bknd

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/complyue/different-hpc/pkg/ccm"
	"github.com/golang/glog"
)

func cnodeSaveCfg(w http.ResponseWriter, r *http.Request) {
	req := struct {
		FileName  string
		AfterEdit string
		PreEdit   string
	}{}
	jsonDecoder := json.NewDecoder(r.Body)
	jsonDecoder.Decode(&req)

	jsonResult := make(map[string]interface{}, 5)
	func() {
		defer func() {
			if e := recover(); e != nil {
				glog.Errorf("Error saving compute node config file [%s]:\n+%v", req.FileName, e)
				jsonResult["err"] = fmt.Sprintf("Unexpected error: %+v", e)
			}
		}()

		oldCfg, err := ccm.LoadComputeNodeCfg(req.FileName, "")
		if err != nil {
			panic(err)
		}
		if oldCfg == nil {
			// file disappeared
			glog.Warningf("Config file [%s] disappeared.", req.FileName)
		} else {
			if len(req.PreEdit) > 0 && req.PreEdit != oldCfg.RawYaml {
				jsonResult["err"] = fmt.Sprintf("Config file has changed!")
				return
			}
			ccm.ForgetIp(oldCfg.Inflate()["ip"].(string))
		}

		if err := ioutil.WriteFile(req.FileName, ([]byte)(req.AfterEdit), 0644); err != nil {
			glog.Errorf("Error saving compute node config file [%s]:\n%+v", req.FileName, err)
			jsonResult["err"] = fmt.Sprintf("Failed saving config file: %+v", err)
		}

		if cfg, err := ccm.ReloadComputeNodeCfg(req.FileName); err != nil {
			panic(err)
		} else {
			ccm.CareIpAliveness(cfg.Inflate()["ip"].(string), false, cfg)
		}
	}()
	if err := json.NewEncoder(w).Encode(jsonResult); err != nil {
		panic(err)
	}
}
