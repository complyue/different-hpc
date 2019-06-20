package ccm

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/complyue/hbi/pkg/errors"
	"github.com/golang/glog"
	"gopkg.in/yaml.v2"
)

type PulseCfg struct {
	// user name used in ssh url
	SshUser string `yaml:"sshUser"`

	// ping with this many packets
	PingCount int `yaml:"pingCount"`

	// don't repeat check too often
	CheckInterval time.Duration `yaml:"checkInterval"`

	// confirm death only after this long
	DeathConfirm time.Duration `yaml:"deathConfirm"`

	// forget after this long
	ForgetDead time.Duration `yaml:"forgetDead"`
}

var pulseCfg *PulseCfg

func GetPulseCfg() *PulseCfg {
	// racing on this cfg loading is negligible to be prevented
	if nil == pulseCfg {
		cfgRawYaml, err := ioutil.ReadFile("etc/pulse.yaml")
		if err != nil {
			panic(err)
		}
		var cfgYaml PulseCfg
		if err = yaml.Unmarshal(cfgRawYaml, &cfgYaml); err != nil {
			panic(err)
		}
		pulseCfg = &cfgYaml
	} else {
		// todo reload on cfg file modified
	}
	return pulseCfg
}

type IpAliveness struct {
	IP string

	AssumeAlive, CheckedAlive bool
	LastAlive, LastCheck      time.Time

	Cfgs []*ComputeNodeCfg
}

var (
	aliveness       = make(map[string]IpAliveness)
	alivenessMutext sync.Mutex
	aliveCheckQueue = make(chan string, 500)
)

func ForgetCfg(cfg *ComputeNodeCfg) {
	cfgData := cfg.Inflate()
	ip := cfgData["ip"].(string)

	alivenessMutext.Lock()
	defer alivenessMutext.Unlock()

	knownState, caring := aliveness[ip]
	if !caring {
		return
	}
	for ci, c := range knownState.Cfgs {
		if c.Mac == cfg.Mac {
			// found the matching MAC record, remove it
			knownState.Cfgs = append(knownState.Cfgs[:ci], knownState.Cfgs[ci+1:]...)
			break
		}
	}
	if len(knownState.Cfgs) < 1 {
		// no more config associated with this ip
		delete(aliveness, ip)
	}
}

func CareIpAliveness(ip string, AssumeAlive bool, cfg *ComputeNodeCfg) {
	alivenessMutext.Lock()
	defer alivenessMutext.Unlock()

	knownState, caring := aliveness[ip]
	if !caring {
		knownState.IP = ip
	}

	if AssumeAlive {
		knownState.AssumeAlive, knownState.LastAlive = true, time.Now()
	}

	replacedCfg := false
	for ci, c := range knownState.Cfgs {
		if c.Mac == cfg.Mac {
			knownState.Cfgs[ci] = cfg
			replacedCfg = true
			break
		}
	}
	if !replacedCfg {
		knownState.Cfgs = append(knownState.Cfgs, cfg)
	}

	aliveness[ip] = knownState

	aliveCheckQueue <- ip
}

func init() {
	go func() {
		for {
			var (
				ip     = <-aliveCheckQueue
				a2c    IpAliveness
				caring bool
			)
			func() {
				alivenessMutext.Lock()
				defer alivenessMutext.Unlock()

				a2c, caring = aliveness[ip]
			}()
			if !caring {
				glog.Warningf("Not caring ip=[%s] anymore.", ip)
				continue
			}

			pulseCfg := GetPulseCfg()
			now := time.Now()
			if now.Before(a2c.LastCheck.Add(pulseCfg.CheckInterval)) {
				// not repeating check within interval
				if now.Sub(a2c.LastCheck) < time.Duration(pulseCfg.PingCount)*time.Second {
					// very near to next check time
					if len(aliveCheckQueue)*2 < cap(aliveCheckQueue) {
						// and queue is not much crowded
						aliveCheckQueue <- ip // schedule another check
					}
				}
				continue
			}

			glog.V(1).Infof("Pinging ip=[%s] for alive check ...", ip)
			pingCmd := exec.Command("ping", "-c", fmt.Sprintf("%d", pulseCfg.PingCount), ip)
			pingCmd.Stdin, pingCmd.Stdout, pingCmd.Stderr = nil, nil, nil
			if err := pingCmd.Run(); err == nil {
				glog.V(1).Infof("IP [%s] is alive.", ip)
				// start/continue caring its aliveness as got positive result at this instant
				a2c.CheckedAlive, a2c.LastCheck = true, now
				a2c.AssumeAlive, a2c.LastAlive = true, now
			} else if ee, ok := err.(*exec.ExitError); ok {
				glog.V(1).Infof("IP [%s] not alive, ping %+v", ip, ee)
				a2c.CheckedAlive, a2c.LastCheck = false, now
				if a2c.AssumeAlive { // check if death can be confirmed now
					if now.After(a2c.LastAlive.Add(pulseCfg.DeathConfirm)) {
						// confirm death after the configured duration
						a2c.AssumeAlive = false
					} else {
						// not positive alive, but keep assumption for now
					}
				}
				if !a2c.AssumeAlive && now.After(a2c.LastAlive.Add(pulseCfg.ForgetDead)) {
					// forget about this IP
					func() {
						alivenessMutext.Lock()
						defer alivenessMutext.Unlock()

						delete(aliveness, ip)
					}()
				}
			} else {
				glog.Errorf("Unexpected error calling ping: %+v", err)
				continue
			}

			func() {
				alivenessMutext.Lock()
				defer alivenessMutext.Unlock()

				caringA2C, caring := aliveness[ip]
				if caring {
					// avoid overwriting with a stale cfg object
					// a2c.Cfg may have been invalidated during checking without alivenessMutext locked
					a2c.Cfgs = caringA2C.Cfgs
				}
				aliveness[ip] = a2c
			}()
		}
	}()
}

func CheckIpAlive(ip string) (bool, time.Time, []*ComputeNodeCfg) {
	pulseCfg := GetPulseCfg()
	knownState, caring := aliveness[ip]
	now := time.Now()

	if caring { // check should be carried out periodically
		if !now.Before(knownState.LastAlive.Add(pulseCfg.CheckInterval)) {
			aliveCheckQueue <- ip
		}
	}

	if caring && knownState.AssumeAlive { // still assuming alive
		return true, knownState.LastAlive, knownState.Cfgs
	}

	pingCmd := exec.Command("ping", "-c", fmt.Sprintf("%d", pulseCfg.PingCount), ip)
	pingCmd.Stdin, pingCmd.Stdout, pingCmd.Stderr = nil, nil, nil
	if err := pingCmd.Run(); err == nil {
		glog.V(1).Infof("IP [%s] is alive.", ip)
		// got positive result at this instant
		// start/continue caring its aliveness as
		knownState = IpAliveness{
			IP:          ip,
			AssumeAlive: true, LastAlive: now,
			CheckedAlive: true, LastCheck: now,
			Cfgs: knownState.Cfgs,
		}
	} else if ee, ok := err.(*exec.ExitError); ok {
		glog.V(1).Infof("IP [%s] not alive, ping %+v", ip, ee)
		if caring {
			knownState.CheckedAlive, knownState.LastCheck = false, now
			// the dedicated checker goro will confirm its death
		} else {
			// not on record, available to be assigned
			return false, time.Time{}, nil
		}
	} else {
		panic(errors.Errorf("Unexpected error calling ping: %+v", err))
	}

	func() {
		alivenessMutext.Lock()
		defer alivenessMutext.Unlock()

		aliveness[ip] = knownState
	}()

	return knownState.AssumeAlive, knownState.LastAlive, knownState.Cfgs
}

func ListCaredIPs() []IpAliveness {
	pulseCfg := GetPulseCfg()
	var caList []IpAliveness

	func() { // sync load
		caList = make([]IpAliveness, 0, len(aliveness))
		alivenessMutext.Lock()
		defer alivenessMutext.Unlock()

		checkThres := time.Now().Add(-pulseCfg.CheckInterval)
		for _, ca := range aliveness {
			if !ca.LastCheck.After(checkThres) {
				// schedule a new check for it
				aliveCheckQueue <- ca.IP
			}
			caList = append(caList, ca)
		}
	}()

	// sort with most recent first, after load
	sort.Slice(caList, func(i, j int) bool {
		if caList[i].LastAlive == caList[j].LastAlive {
			return caList[i].LastCheck.After(caList[j].LastCheck)
		}
		return caList[i].LastAlive.After(caList[j].LastAlive)
	})

	return caList
}
