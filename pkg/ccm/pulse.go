package ccm

import (
	"io/ioutil"
	"os/exec"
	"sync"
	"time"

	"github.com/complyue/hbigo/pkg/errors"
	"github.com/golang/glog"
	"gopkg.in/yaml.v2"
)

type PulseCfg struct {
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
		checkInterval, deathConfirm := "1h", "48h"
		for _, cfgItem := range cfgYaml {
			if cfgKey, ok := cfgItem.Key.(string); ok {
				if "checkInterval" == cfgKey {
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
			CheckInterval: CheckInterval, DeathConfirm: DeathConfirm,
		}
	} else {
		// todo reload on cfg file modified
	}
	return pulseCfg
}

type checkedAliveness struct {
	assumeAlive, checkedAlive bool
	lastAlive, lastCheck      time.Time
}

var (
	aliveness       = make(map[string]checkedAliveness)
	alivenessMutext sync.Mutex
)

func CareIpAliveness(ip string, assumeAlive bool) {
	alivenessMutext.Lock()
	defer alivenessMutext.Unlock()

	var lastAlive time.Time
	if assumeAlive {
		lastAlive = time.Now()
	}
	aliveness[ip] = checkedAliveness{
		assumeAlive: assumeAlive, lastAlive: lastAlive,
		checkedAlive: assumeAlive, lastCheck: time.Time{},
	}
}

func CheckIpAlive(ip string) (bool, time.Time) {
	alivenessMutext.Lock()
	defer alivenessMutext.Unlock()

	pulseCfg := GetPulseCfg()
	knownState, caring := aliveness[ip]

	if caring && knownState.assumeAlive {
		// assuming alive
		if time.Now().Before(knownState.lastAlive.Add(pulseCfg.CheckInterval)) {
			// assumption still valid
			return true, knownState.lastAlive
		}

		// not actually alive after the configured interval, check should be carried out periodically

		if !knownState.lastCheck.IsZero() && time.Now().Before(knownState.lastCheck.Add(pulseCfg.CheckInterval)) {
			// no hurry to repeat another death check
			return true, knownState.lastAlive
		}
	}

	now := time.Now()
	pingCmd := exec.Command("ping", "-c", "3")
	pingCmd.Stdin, pingCmd.Stdout, pingCmd.Stderr = nil, nil, nil
	if err := pingCmd.Run(); err == nil {
		glog.V(1).Infof("IP [%s] is alive.")
		// start/continue caring its aliveness as got positive result at this instant
		knownState = checkedAliveness{
			assumeAlive: true, lastAlive: now,
			checkedAlive: true, lastCheck: now,
		}
		aliveness[ip] = knownState
	} else if ee, ok := err.(*exec.ExitError); ok {
		glog.V(1).Infof("IP [%s] not alive, ping result: %v", ip, ee)
		if caring && knownState.assumeAlive {
			if now.After(knownState.lastAlive.Add(pulseCfg.DeathConfirm)) {
				// confirm death after the configured duration
				knownState = checkedAliveness{
					assumeAlive: false, lastAlive: knownState.lastAlive,
					checkedAlive: false, lastCheck: now,
				}
				aliveness[ip] = knownState
			} else {
				// not positive alive, but keep assumption for now
			}
		} else {
			// not assuming alive, return affirmative dead conclusion
			return false, time.Time{}
		}
	} else {
		panic(errors.Errorf("Unexpected error calling ping: %+v", err))
	}

	return knownState.assumeAlive, knownState.lastAlive
}
