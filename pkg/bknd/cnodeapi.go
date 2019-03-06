package bknd

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

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

		if fi, err := os.Stat(req.FileName); err == nil {
			// file exists
			rawYaml, err := ioutil.ReadFile(req.FileName)
			if err != nil {
				panic(err)
			}
			if len(req.PreEdit) > 0 && req.PreEdit != string(rawYaml) {
				jsonResult["err"] = fmt.Sprintf("Config file has changed at %v", fi.ModTime)
				return
			}
		} else {
			// file disappeared
			glog.Warningf("Config file [%s] disappeared.", req.FileName)
		}

		if err := ioutil.WriteFile(req.FileName, ([]byte)(req.AfterEdit), 0644); err != nil {
			glog.Errorf("Error saving compute node config file [%s]:\n%+v", req.FileName, err)
			jsonResult["err"] = fmt.Sprintf("Failed saving config file: %+v", err)
		}
	}()
	if err := json.NewEncoder(w).Encode(jsonResult); err != nil {
		panic(err)
	}
}
