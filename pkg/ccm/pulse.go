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
	SshUser string `yaml:"sshUser"`

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

	Cfg interface{}
}

var (
	aliveness       = make(map[string]IpAliveness)
	alivenessMutext sync.Mutex
	aliveCheckQueue = make(chan IpAliveness, 500)
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

func init() {
	go func() {

		for {
			a2c := <-aliveCheckQueue

			pulseCfg := GetPulseCfg()
			now := time.Now()
			if now.Before(a2c.LastCheck.Add(pulseCfg.CheckInterval)) {
				// not repeating check within interval
				continue
			}

			glog.V(1).Infof("Pinging ip=[%s] for alive check ...", a2c.IP)
			pingCmd := exec.Command("ping", "-c", "3", a2c.IP)
			pingCmd.Stdin, pingCmd.Stdout, pingCmd.Stderr = nil, nil, nil
			if err := pingCmd.Run(); err == nil {
				glog.V(1).Infof("IP [%s] is alive.", a2c.IP)
				// start/continue caring its aliveness as got positive result at this instant
				a2c.CheckedAlive, a2c.LastCheck = true, now
				a2c.AssumeAlive, a2c.LastAlive = true, now
			} else if ee, ok := err.(*exec.ExitError); ok {
				glog.V(1).Infof("IP [%s] not alive, ping %+v", a2c.IP, ee)
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

						delete(aliveness, a2c.IP)
					}()
				}
			} else {
				glog.Errorf("Unexpected error calling ping: %+v", err)
				continue
			}

			func() {
				alivenessMutext.Lock()
				defer alivenessMutext.Unlock()

				aliveness[a2c.IP] = a2c
			}()
		}

	}()
}

func CheckIpAlive(ip string) (bool, time.Time, interface{}) {
	pulseCfg := GetPulseCfg()
	knownState, caring := aliveness[ip]
	now := time.Now()

	if caring { // check should be carried out periodically
		if !now.Before(knownState.LastAlive.Add(pulseCfg.CheckInterval)) {
			aliveCheckQueue <- knownState
		}
	}

	if caring && knownState.AssumeAlive { // still assuming alive
		return true, knownState.LastAlive, knownState.Cfg
	}

	pingCmd := exec.Command("ping", "-c", "3", ip)
	pingCmd.Stdin, pingCmd.Stdout, pingCmd.Stderr = nil, nil, nil
	if err := pingCmd.Run(); err == nil {
		glog.V(1).Infof("IP [%s] is alive.", ip)
		// got positive result at this instant
		// start/continue caring its aliveness as
		knownState = IpAliveness{
			IP:          ip,
			AssumeAlive: true, LastAlive: now,
			CheckedAlive: true, LastCheck: now,
			Cfg: knownState.Cfg,
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
		for _, ca := range aliveness {
			if !ca.LastCheck.After(checkThres) {
				// schedule a new check for it
				aliveCheckQueue <- ca
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
