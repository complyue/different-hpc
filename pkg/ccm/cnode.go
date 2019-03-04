package ccm

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/complyue/hbigo/pkg/errors"
	"github.com/golang/glog"
	"gopkg.in/yaml.v2"
)

type ComputeNodeCfg struct {
	Mac      string
	FileName string
	FileTime time.Time

	IP    string
	IPNum int

	CfgYaml yaml.MapSlice
}

const (
	cnodesDir = "etc/cnodes"
)

var (
	knownComputeNodeCfgs map[string]ComputeNodeCfg
	mutexComputeNodeCfgs sync.Mutex
)

func PrepareComputeNodeCfg(mac string) (ComputeNodeCfg, error) {
	mutexComputeNodeCfgs.Lock()
	defer mutexComputeNodeCfgs.Unlock()

	if nil == knownComputeNodeCfgs {
		// do initial loading cfg for all known compute nodes
		loadingCfgs := make(map[string]ComputeNodeCfg, 255)
		dirf, err := os.Open(cnodesDir)
		if err != nil {
			panic(errors.Errorf("Error reading dir: "+cnodesDir+"\n%+v", err))
		}
		files, err := dirf.Readdir(500)
		if err != nil && err != io.EOF {
			panic(errors.Errorf("Error listing dir: "+cnodesDir+"\n%+v", err))
		}
		for _, fi := range files {
			fn := fi.Name()
			if fi.IsDir() {
				continue
			}
			switch fn[0] {
			case '.':
				fallthrough
			case '_':
				fallthrough
			case '~':
				fallthrough
			case '!':
				continue
			}
			if !strings.HasSuffix(fn, ".yaml") {
				continue
			}

			fileName := "etc/cnodes/" + fn
			// if a single cfg file is to cause panic, ignore it with proper log, other things continue
			func() {
				defer func() {
					if e := recover(); e != nil {
						glog.Errorf("Error loading compute node cfg file [%s]\n%+v", fileName, e)
					}
				}()
				rawYaml, err := ioutil.ReadFile(fileName)
				if err != nil {
					panic(err)
				}
				var cfgYaml yaml.MapSlice
				err = yaml.Unmarshal(rawYaml, &cfgYaml)
				if err != nil {
					panic(err)
				}
				var cfgMac, ip string
				ipNum := 0
				for _, cfgItem := range cfgYaml {
					if cfgKey, ok := cfgItem.Key.(string); ok {
						if "mac" == cfgKey {
							cfgMac = cfgItem.Value.(string)
						} else if "ip" == cfgKey {
							ip = cfgItem.Value.(string)
						} else if "ipnum" == cfgKey {
							ipNum = int(cfgItem.Value.(int))
						}
					}
				}
				loadingCfgs[mac] = ComputeNodeCfg{
					Mac: cfgMac, FileName: fileName,
					FileTime: fi.ModTime(),
					IP:       ip, IPNum: ipNum,
					CfgYaml: cfgYaml,
				}
				// assume alive since initial load, by sole existance of a node's cfg file
				CareIpAliveness(ip, true)
			}()
		}
		// only assign to global var after finished loading at all
		knownComputeNodeCfgs = loadingCfgs
	}

	if cfg, ok := knownComputeNodeCfgs[mac]; ok {
		// already loaded, check reload in case file modified after last load
		if fi, err := os.Stat(cfg.FileName); err == nil {
			if fi.ModTime() == cfg.FileTime {
				return cfg, nil
			}
		}
	}

	macKey := strings.Replace(mac, ":", "-", -1)
	fileName := "etc/cnodes/" + macKey + ".yaml"

	if fi, err := os.Stat(fileName); err == nil {
		// file exists, either manually created or modified,
		// do a fresh load
		rawYaml, err := ioutil.ReadFile(fileName)
		if err != nil {
			panic(err)
		}
		var cfgYaml yaml.MapSlice
		err = yaml.Unmarshal(rawYaml, &cfgYaml)
		if err != nil {
			panic(err)
		}
		var cfgMac, ip string
		ipNum := 0
		for _, cfgItem := range cfgYaml {
			if cfgKey, ok := cfgItem.Key.(string); ok {
				if "mac" == cfgKey {
					cfgMac = cfgItem.Value.(string)
				} else if "ip" == cfgKey {
					ip = cfgItem.Value.(string)
				} else if "ipnum" == cfgKey {
					ipNum = int(cfgItem.Value.(int))
				}
			}
		}
		if cfgMac != mac {
			panic(errors.Errorf(
				"Invalid mac=[%s] in config for mac=[%s], file=[%s]",
				cfgMac, mac, fileName,
			))
		}
		if len(ip) <= 0 {
			panic(errors.Errorf(
				"Invalid ip=[%s] in config for mac=[%s], file=[%s]",
				ip, mac, fileName,
			))
		}
		cfg := ComputeNodeCfg{
			Mac: mac, FileName: fileName,
			FileTime: fi.ModTime(),
			IP:       ip, IPNum: ipNum,
			CfgYaml: cfgYaml,
		}
		knownComputeNodeCfgs[mac] = cfg
		CareIpAliveness(ip, true)
		return cfg, nil
	}

	// no cfg yet, auto assign an IP and create the cfg
	glog.Infof("Compute node with mac=[%s] has no config yet, generating ...", mac)

	// todo: cache the template cfg, reload on mod time change
	tmplFileName := "etc/cnode.yaml"
	tmplRawYaml, err := ioutil.ReadFile(tmplFileName)
	if err != nil {
		panic(err)
	}
	var tmplYaml yaml.MapSlice
	err = yaml.Unmarshal(tmplRawYaml, &tmplYaml)
	if err != nil {
		panic(err)
	}

	var cfgYaml yaml.MapSlice
	var (
		ipnPrefix string
		ipnRange  []interface{}
	)
	for _, cfgItem := range tmplYaml {
		if cfgKey, ok := cfgItem.Key.(string); ok && "autoip" == cfgKey {
			for _, ci1 := range cfgItem.Value.(yaml.MapSlice) {
				if "prefix" == ci1.Key.(string) {
					ipnPrefix = ci1.Value.(string)
				} else if "range" == ci1.Key.(string) {
					ipnRange = ci1.Value.([]interface{})
				}
			}
			continue
		}

		// inherit into compute node's config
		cfgYaml = append(cfgYaml, cfgItem)
	}

	// auto assign ip
	type deadIP struct {
		IP        string
		IPNum     int
		LastAlive time.Time
	}
	var deadIPs []deadIP
	ipAssigned, ip, ipNum, aliveCnt := false, "", 0, 0
	for ipnRI := 0; ipnRI < len(ipnRange); ipnRI += 2 {
		ipnStart := int(ipnRange[ipnRI].(int))
		ipnEnd := int(ipnRange[ipnRI+1].(int))
		for ipNum = ipnStart; ipNum <= ipnEnd; ipNum++ {
			ip = fmt.Sprintf("%s%v", ipnPrefix, ipNum)
			alive, lastAliveTime := CheckIpAlive(ip)
			if alive {
				aliveCnt++
			} else {
				if lastAliveTime.IsZero() {
					// got a never alive ip, use it
					glog.Infof("Assigned next available ip=[%s] for mac=[%s]", ip, mac)
					ipAssigned = true
					break
				} else {
					deadIPs = append(deadIPs, deadIP{ip, ipNum, lastAliveTime})
				}
			}
		}
	}
	if !ipAssigned && len(deadIPs) > 0 {
		// sort to find the IP with earlest known alive time for reuse
		sort.Slice(deadIPs, func(i, j int) bool {
			return deadIPs[i].LastAlive.Before(deadIPs[j].LastAlive)
		})
		reuseIP := deadIPs[0]
		glog.Warningf("Assigning ip=%s known alive till %v,"+
			" the old server may insult IP conflicts if booted again.",
			reuseIP.IP, reuseIP.LastAlive)
		ip, ipNum = reuseIP.IP, reuseIP.IPNum
		ipAssigned = true
	}
	if !ipAssigned {
		panic(errors.Errorf("No available IP in configured range, all %v occupied.", aliveCnt))
	}

	// put assgined IP at top of generated YAML
	cfgYaml = append(yaml.MapSlice{
		yaml.MapItem{"mac", mac},
		yaml.MapItem{"ip", ip},
		yaml.MapItem{"ipnum", ipNum},
	}, cfgYaml...)

	// save config file
	cfgRawYaml, err := yaml.Marshal(cfgYaml)
	ioutil.WriteFile(fileName, cfgRawYaml, 0644)
	glog.Infof("Configuration for compute node mac=[%s] written to file [%s]", mac, fileName)

	// load file mod time
	fi, err := os.Stat(fileName)
	if err != nil {
		panic(err)
	}

	// record the cfg, mark it alive even before booted
	cfg := ComputeNodeCfg{
		Mac: mac, FileName: fileName,
		FileTime: fi.ModTime(),
		IP:       ip, IPNum: ipNum,
		CfgYaml: cfgYaml,
	}
	knownComputeNodeCfgs[mac] = cfg
	CareIpAliveness(cfg.IP, true)

	return cfg, nil
}
