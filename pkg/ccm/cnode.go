package ccm

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/complyue/hbi/pkg/errors"
	"github.com/golang/glog"
	"gopkg.in/yaml.v2"
)

type ComputeNodeCfg struct {
	Mac string

	GuiType, GuiHref string

	FileName string
	FileTime time.Time

	RawYaml string
	CfgYaml yaml.MapSlice
}

func (cfg *ComputeNodeCfg) Inflate() map[string]interface{} {
	ctx := make(map[string]interface{}, 20)
	buf := bytes.NewBuffer(nil)
	for _, cfgItem := range cfg.CfgYaml {
		if cfgKey, ok := cfgItem.Key.(string); ok {
			switch cfgVal := cfgItem.Value.(type) {
			case int:
				ctx[cfgKey] = cfgVal
			case float64:
				ctx[cfgKey] = cfgVal
			case string:
				vt := template.Must(template.New("Value of " + cfgKey).Parse(cfgVal))
				buf.Reset()
				if err := vt.Execute(buf, ctx); err != nil {
					panic(err)
				}
				val := buf.String()
				ctx[cfgKey] = val
			case []interface{}:
				seqStrs := make([]string, 0, len(cfgVal))
				for seqI, seqElem := range cfgVal {
					if seqStr, ok := seqElem.(string); ok {
						vt := template.Must(template.New(fmt.Sprintf(
							"Value of %s:%v", cfgKey, seqI+1)).Parse(seqStr))
						buf.Reset()
						if err := vt.Execute(buf, ctx); err != nil {
							panic(err)
						}
						val := buf.String()
						seqStrs = append(seqStrs, val)
					}
				}
				ctx[cfgKey] = seqStrs
			}

		}
	}
	return ctx
}

const (
	cnodesDir = "etc/cnodes"
)

func LoadComputeNodeCfg(fileName string, mac string) (*ComputeNodeCfg, error) {
	fi, err := os.Stat(fileName)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	// file exists, either manually created or modified,
	// do a fresh load
	rawYaml, err := ioutil.ReadFile(fileName)
	if err != nil {
		return nil, err
	}
	var cfgYaml yaml.MapSlice
	err = yaml.Unmarshal(rawYaml, &cfgYaml)
	if err != nil {
		return nil, err
	}
	var cfgMac, ip string
	var guiType, guiHref string
	for _, cfgItem := range cfgYaml {
		if cfgKey, ok := cfgItem.Key.(string); ok {
			if "mac" == cfgKey {
				cfgMac = cfgItem.Value.(string)
			} else if "ip" == cfgKey {
				ip = cfgItem.Value.(string)
			} else if "guiHref" == cfgKey {
				guiHref = cfgItem.Value.(string)
			} else if "guiType" == cfgKey {
				guiType = cfgItem.Value.(string)
			}
		}
	}

	var problem error
	if len(mac) > 0 && cfgMac != mac {
		problem = errors.Errorf(
			"invalid mac=[%s] vs [%s] in config file [%s]",
			cfgMac, mac, fileName,
		)
	}
	if len(ip) <= 0 {
		problem = errors.Errorf(
			"no ip in config file [%s]",
			fileName,
		)
	}
	if problem != nil {
		glog.Warningf("Problem detected: %+v", problem)
		d, f := filepath.Split(fileName)
		bogonFileName := fmt.Sprintf("%s~%s.bogon-%s", d, f, time.Now().Format("20060102150405"))
		glog.Infof("Renaming bogus config file from [%s] to [%s] ...",
			fileName, bogonFileName)
		if err = os.Rename(fileName, bogonFileName); err != nil {
			return nil, err
		}
		return nil, nil
	}

	cfg := &ComputeNodeCfg{
		Mac:     cfgMac,
		GuiType: guiType, GuiHref: guiHref,
		FileName: fileName, FileTime: fi.ModTime(),
		RawYaml: string(rawYaml), CfgYaml: cfgYaml,
	}
	return cfg, nil
}

var (
	knownComputeNodeCfgs map[string]*ComputeNodeCfg
	mutexComputeNodeCfgs sync.Mutex
)

func ReloadComputeNodeCfg(fileName string) (*ComputeNodeCfg, error) {
	cfg, err := LoadComputeNodeCfg(fileName, "")
	if err != nil {
		return cfg, err
	}

	mutexComputeNodeCfgs.Lock()
	defer mutexComputeNodeCfgs.Unlock()

	knownComputeNodeCfgs[cfg.Mac] = cfg
	return cfg, nil
}

func _getComputeNodeCfgs() map[string]*ComputeNodeCfg {
	if nil == knownComputeNodeCfgs {
		// do initial loading cfg for all known compute nodes
		loadingCfgs := make(map[string]*ComputeNodeCfg, 255)
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
				if cfg, err := LoadComputeNodeCfg(fileName, ""); err != nil {
					panic(err)
				} else if cfg != nil {
					loadingCfgs[cfg.Mac] = cfg
					// assume alive since initial load, by sole existance of a node's cfg file
					CareIpAliveness(cfg.Inflate()["ip"].(string), true, cfg)
				}
			}()
		}
		// only assign to global var after finished loading at all
		knownComputeNodeCfgs = loadingCfgs
	}

	return knownComputeNodeCfgs
}

func GetComputeNodeCfgs() []ComputeNodeCfg {
	mutexComputeNodeCfgs.Lock()
	defer mutexComputeNodeCfgs.Unlock()

	_getComputeNodeCfgs()

	cfgs := make([]ComputeNodeCfg, 0, len(knownComputeNodeCfgs))
	for _, cfg := range knownComputeNodeCfgs {
		cfgs = append(cfgs, *cfg)
	}
	return cfgs
}

func PrepareComputeNodeCfg(mac string) (*ComputeNodeCfg, error) {
	mutexComputeNodeCfgs.Lock()
	defer mutexComputeNodeCfgs.Unlock()

	_getComputeNodeCfgs()

	if cfg, ok := knownComputeNodeCfgs[mac]; ok {
		// already loaded
		// check reload in case file modified after last load
		if fi, err := os.Stat(cfg.FileName); err == nil {
			if cfg.Mac != mac {
				glog.Errorf("Config file [%s] contains invalid mac=[%s] vs [%s] ?!",
					cfg.FileName, cfg.Mac, mac)
				d, f := filepath.Split(cfg.FileName)
				bogonFileName := fmt.Sprintf("%s~%s.bogon-%s", d, f, time.Now().Format("20060102150405"))
				glog.Infof("Renaming bogus config file from [%s] to [%s] ...",
					cfg.FileName, bogonFileName)
				if err := os.Rename(cfg.FileName, bogonFileName); err != nil {
					panic(err)
				}
				delete(knownComputeNodeCfgs, mac)
				glog.Warningf("A new configuration will be generated for mac=[%s]", mac)
			} else if fi.ModTime() == cfg.FileTime {
				// file not modified since last load
				return cfg, nil
			} else {
				// file changed, fallthrough to do a fresh load
			}
		} else {
			glog.Warningf("Config file [%s] for mac=[%s] deleted ?", cfg.FileName, cfg.Mac)
		}
	}

	macKey := strings.Replace(mac, ":", "-", -1)
	fileName := "etc/cnodes/" + macKey + ".yaml"
	if cfg, err := LoadComputeNodeCfg(fileName, mac); err != nil {
		panic(err)
	} else if cfg != nil {
		knownComputeNodeCfgs[mac] = cfg
		CareIpAliveness(cfg.Inflate()["ip"].(string), true, cfg)
		return cfg, nil
	}

	// no cfg yet, auto assign an IP and create the cfg
	glog.Infof("Generating config for compute node with mac=[%s] ...", mac)

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
		LastCfg   interface{}
	}
	var deadIPs []deadIP
	ipAssigned, ip, ipNum, aliveCnt := false, "", 0, 0
	for ipnRI := 0; ipnRI < len(ipnRange); ipnRI += 2 {
		ipnStart := int(ipnRange[ipnRI].(int))
		ipnEnd := int(ipnRange[ipnRI+1].(int))
		for ipNum = ipnStart; ipNum <= ipnEnd; ipNum++ {
			ip = fmt.Sprintf("%s%v", ipnPrefix, ipNum)
			alive, lastAliveTime, aliveCfg := CheckIpAlive(ip)
			if alive {
				aliveCnt++
			} else {
				if lastAliveTime.IsZero() {
					// got a never alive ip, use it
					glog.Infof("Assigned next available ip=[%s] for mac=[%s]", ip, mac)
					ipAssigned = true
					break
				} else {
					deadIPs = append(deadIPs, deadIP{ip, ipNum, lastAliveTime, aliveCfg})
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
		switch deadCfg := reuseIP.LastCfg.(type) {
		case *ComputeNodeCfg:
			d, f := filepath.Split(deadCfg.FileName)
			corpseFileName := fmt.Sprintf("%s~%s.corpse-%s", d, f, time.Now().Format("20060102150405"))
			glog.Infof("To reuse ip=[%s], the old config file is to be renamed from [%s] to [%s] ...",
				reuseIP.IP, deadCfg.FileName, corpseFileName)
			if err := os.Rename(deadCfg.FileName, corpseFileName); err != nil {
				panic(err)
			}
			delete(knownComputeNodeCfgs, deadCfg.Mac)
			glog.Warningf("Reusing ip=[%s] from [%s], which has been renamed to [%s]",
				reuseIP.IP, deadCfg.FileName, corpseFileName)
		default:
			glog.Warningf("ip=[%s] not bound to any known config ?! cfg type=%T", reuseIP.IP, deadCfg)
			glog.Warningf("Reusing ip=[%s]", ip)
		}
		ip, ipNum = reuseIP.IP, reuseIP.IPNum
		ipAssigned = true
	}
	if !ipAssigned {
		panic(errors.Errorf("No available IP in configured range, all %v occupied.", aliveCnt))
	}

	// put assgined IP at top of generated YAML
	cfgYaml = append(yaml.MapSlice{
		yaml.MapItem{"generated", time.Now().Format("2006-01-02T15:04:05Z07:00")},
		yaml.MapItem{"mac", mac},
		yaml.MapItem{"ip", ip},
		yaml.MapItem{"ipnum", ipNum},
	}, cfgYaml...)

	// save config file
	rawYaml, err := yaml.Marshal(cfgYaml)
	ioutil.WriteFile(fileName, rawYaml, 0644)
	glog.Infof("Configuration for compute node mac=[%s] written to file [%s]", mac, fileName)

	// load file mod time
	fi, err := os.Stat(fileName)
	if err != nil {
		panic(err)
	}

	// record the cfg, mark it alive even before booted
	cfg := &ComputeNodeCfg{
		Mac: mac, FileName: fileName, FileTime: fi.ModTime(),
		RawYaml: string(rawYaml), CfgYaml: cfgYaml,
	}
	knownComputeNodeCfgs[mac] = cfg
	CareIpAliveness(ip, true, cfg)

	return cfg, nil
}
