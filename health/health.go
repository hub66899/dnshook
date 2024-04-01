package health

import (
	"bufio"
	"context"
	"dnshook/rule"
	"github.com/pkg/errors"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Interface struct {
	Name   string `yaml:"name"`
	Weight int    `yaml:"weight"`
}

type Config struct {
	Vpn                []Interface `yaml:"vpn"`
	Wan                []Interface `yaml:"wan"`
	PingAddr           []string    `yaml:"ping-addr"`
	PingTimeoutSeconds int         `yaml:"ping-timeout-seconds"`
}

var (
	pingAddr    []string
	pingTimeout time.Duration
)

var (
	ctx  context.Context
	stop func()
)

func Start(conf Config) error {
	Stop()
	pingAddr = conf.PingAddr
	pingTimeout = time.Second * time.Duration(conf.PingTimeoutSeconds)
	ctx, stop = context.WithCancel(context.Background())
	vpnNetwork := map[string]*network{}
	wanNetwork := map[string]*network{}

	updateRule := func() {
		outputInterfaces := rule.OutputInterfaces{}
		for _, n := range vpnNetwork {
			if n.currentStatus == available {
				outputInterfaces.Vpn = append(outputInterfaces.Vpn, rule.Interface{
					Name:   n.Name,
					Weight: n.Weight,
				})
			}
		}
		for _, n := range wanNetwork {
			if n.currentStatus == available {
				outputInterfaces.Wan = append(outputInterfaces.Wan, rule.Interface{
					Name:   n.Name,
					Weight: n.Weight,
				})
			}
		}
		log.Printf("output interfaces changed")
		if err := rule.SetOutputInterfaces(outputInterfaces); err != nil {
			log.Printf("set output interfaces error:%v \n", err)
		}
	}
	wg := sync.WaitGroup{}

	for _, i := range conf.Wan {
		wg.Add(1)
		n := &network{
			onStatusChanged: func(_ status) {
				updateRule()
			},
			currentStatus: available,
			Interface:     i,
		}
		wanNetwork[i.Name] = n
		go func() {
			n.start()
			wg.Done()
		}()
	}
	for _, i := range conf.Vpn {
		wg.Add(1)
		n := &network{
			onStatusChanged: func(_ status) {
				updateRule()
			},
			currentStatus: available,
			Interface:     i,
		}
		vpnNetwork[i.Name] = n
		go func() {
			n.start()
			wg.Done()
		}()
	}
	log.Printf("total %d vpn network interfaces", len(vpnNetwork))
	log.Printf("total %d wan network interfaces", len(wanNetwork))
	wg.Wait()
	updateRule()
	return nil
}

func Stop() {
	if stop != nil {
		stop()
	}
}

type status int8

const (
	available   status = 1
	unavailable status = 2
)

type network struct {
	Interface
	currentStatus   status
	onStatusChanged func(status)
}

// 開始後不允許停止
func (l *network) start() {
	initCtx, done := context.WithTimeout(ctx, time.Second*5)
	defer done()

	go func() {
		i := 0 //ping地址index
		failedTimes := 0
		for {
			addr := pingAddr[i]
			ch := make(chan string, 1)
			go func() {
				for _ = range ch {
					failedTimes = 0
					if l.currentStatus != available {
						l.currentStatus = available
						if l.onStatusChanged != nil {
							l.onStatusChanged(available)
						}
					}
					if initCtx.Err() == nil {
						done()
					}
				}
			}()
			err := ping(ch, l.Name, addr)
			close(ch)
			if err != nil {
				log.Printf("failed to ping %v", err)
				failedTimes++
				if l.currentStatus != unavailable && failedTimes > 2 {
					l.currentStatus = unavailable
					if l.onStatusChanged != nil {
						l.onStatusChanged(unavailable)
					}
				}
				time.Sleep(time.Second * 2)
				i++
				if i == len(pingAddr) {
					i = 0
				}
			}
		}
	}()
	<-initCtx.Done()
}

func ping(ch chan<- string, oifname string, address string) error {
	pingCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(pingCtx, "ping", "-I", oifname, address)
	defer cancel()

	timeout := time.NewTimer(pingTimeout)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return errors.WithStack(err)
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			text := scanner.Text()
			if isPingSuccess(text) {
				ch <- scanner.Text()
				timeout.Reset(pingTimeout)
			} else {
				log.Printf("ping result: %s\n", text)
			}
		}
	}()

	if err = cmd.Start(); err != nil {
		return errors.WithStack(err)
	}
	defer cmd.Wait()

	select {
	case <-timeout.C:
		cancel()
		return errors.New("timeout")
	case <-pingCtx.Done():
		timeout.Stop()
		return nil
	}
}

var successPattern = regexp.MustCompile(`\d+ bytes from .+: icmp_seq=\d+ ttl=\d+ time=.+ ms`)

func isPingSuccess(line string) bool {
	// 检查成功的关键词
	successKeywords := []string{"bytes from", "time=", "icmp_seq="}
	for _, keyword := range successKeywords {
		if strings.Contains(line, keyword) {
			return true
		}
	}
	return successPattern.MatchString(line)
}
