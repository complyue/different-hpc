package ccm

import (
	"io/ioutil"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/complyue/hbigo/pkg/errors"
	"github.com/golang/glog"
	"gopkg.in/yaml.v2"
)

type PulseCfg struct {
	// user name used in ssh url
	SshUser string

	// don't repeat check too often
	CheckInterval time.Duration

	// confirm death only after this long
	DeathConfirm time.Duration
}

var pulseCfg *PulseCfg

func GetPulseCfg() *PulseCfg {
	// racing on this cfg loading is negligible to be prevented
	if nil == pulseCfg {
		cfgRawYaml, err := ioutil.ReadFile("etc/pulse.yaml")
		if err != nil {
			panic(err)
		}
		var cfgYaml yaml.MapSlice
		if err = yaml.Unmarshal(cfgRawYaml, &cfgYaml); err != nil {
			panic(err)
		}
		sshUser, checkInterval, deathConfirm := "root", "1h", "48h"
		for _, cfgItem := range cfgYaml {
			if cfgKey, ok := cfgItem.Key.(string); ok {
				if "sshUser" == cfgKey {
					sshUser = cfgItem.Value.(string)
				} else if "checkInterval" == cfgKey {
					checkInterval = cfgItem.Value.(string)
				} else if "deathConfirm" == cfgKey {
					deathConfirm = cfgItem.Value.(string)
				}
			}
		}
		CheckInterval, err := time.ParseDuration(checkInterval)
		if err != nil {
			panic(err)
		}
		DeathConfirm, err := time.ParseDuration(deathConfirm)
		if err != nil {
			panic(err)
		}
		pulseCfg = &PulseCfg{
			SshUser:       sshUser,
			CheckInterval: CheckInterval, DeathConfirm: DeathConfirm,
		}
	} else {
		// todo reload on cfg file modified
	}
	return pulseCfg
}

type IpAliveness struct {
	IP string

	AssumeAlive, CheckedAlive bool
	LastAlive, LastCheck      time.Time

	Cfg interface{}
}

var (
	aliveness       = make(map[string]IpAliveness)
	alivenessMutext sync.Mutex
)

func CareIpAliveness(ip string, AssumeAlive bool, cfg interface{}) {
	alivenessMutext.Lock()
	defer alivenessMutext.Unlock()

	var LastAlive time.Time
	if AssumeAlive {
		LastAlive = time.Now()
	}
	aliveness[ip] = IpAliveness{
		IP:          ip,
		AssumeAlive: AssumeAlive, LastAlive: LastAlive,
		CheckedAlive: false, LastCheck: time.Time{},
		Cfg: cfg,
	}
}

func CheckIpAlive(ip string) (bool, time.Time, interface{}) {
	alivenessMutext.Lock()
	defer alivenessMutext.Unlock()

	return _checkIpAlive(ip)
}

func _checkIpAlive(ip string) (bool, time.Time, interface{}) {

	pulseCfg := GetPulseCfg()
	knownState, caring := aliveness[ip]

	if caring && knownState.AssumeAlive {
		// assuming alive
		if time.Now().Before(knownState.LastAlive.Add(pulseCfg.CheckInterval)) {
			// assumption still valid
			return true, knownState.LastAlive, knownState.Cfg
		}

		// not actually alive after the configured interval, check should be carried out periodically

		if !knownState.LastCheck.IsZero() && time.Now().Before(knownState.LastCheck.Add(pulseCfg.CheckInterval)) {
			// no hurry to repeat another death check
			return true, knownState.LastAlive, knownState.Cfg
		}
	}

	now := time.Now()
	pingCmd := exec.Command("ping", "-c", "3", ip)
	pingCmd.Stdin, pingCmd.Stdout, pingCmd.Stderr = nil, nil, nil
	if err := pingCmd.Run(); err == nil {
		glog.V(1).Infof("IP [%s] is alive.", ip)
		// start/continue caring its aliveness as got positive result at this instant
		knownState = IpAliveness{
			IP:          ip,
			AssumeAlive: true, LastAlive: now,
			CheckedAlive: true, LastCheck: now,
			Cfg: knownState.Cfg,
		}
		aliveness[ip] = knownState
	} else if ee, ok := err.(*exec.ExitError); ok {
		glog.V(1).Infof("IP [%s] not alive, ping %+v", ip, ee)
		if caring {
			knownState.CheckedAlive = false
			knownState.LastCheck = now
			if knownState.AssumeAlive { // check if death can be confirmed now
				if now.After(knownState.LastAlive.Add(pulseCfg.DeathConfirm)) {
					// confirm death after the configured duration
					knownState = IpAliveness{
						IP:          ip,
						AssumeAlive: false, LastAlive: knownState.LastAlive,
						CheckedAlive: false, LastCheck: now,
						Cfg: nil,
					}
					aliveness[ip] = knownState
				} else {
					// not positive alive, but keep assumption for now
					return true, knownState.LastAlive, knownState.Cfg
				}
			}
			// not assuming alive, return affirmative dead conclusion
			return false, time.Time{}, nil
		}
	} else {
		panic(errors.Errorf("Unexpected error calling ping: %+v", err))
	}

	return knownState.AssumeAlive, knownState.LastAlive, knownState.Cfg
}

func ListCaredIPs() []IpAliveness {
	pulseCfg := GetPulseCfg()
	var caList []IpAliveness

	func() { // sync load
		caList = make([]IpAliveness, 0, len(aliveness))
		alivenessMutext.Lock()
		defer alivenessMutext.Unlock()

		checkThres := time.Now().Add(-pulseCfg.CheckInterval)
		for ip, ca := range aliveness {
			if ca.LastCheck.Before(checkThres) {
				if alive, lastAlive, cfg := _checkIpAlive(ip); alive && cfg != ca.Cfg {
					glog.Warningf("ip=[%s] alive up to %v as another cfg:\n%+v\n - vs -\n%+v",
						ip, lastAlive, cfg, ca.Cfg)
					continue
				}
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
