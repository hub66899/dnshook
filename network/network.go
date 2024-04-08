package network

import (
	"bytes"
	"context"
	"fmt"
	"github.com/pkg/errors"
	"log"
	"os/exec"
	"strings"
	"sync"
	"text/template"
	"time"
)

const tableTmp = `
table ip shield_link {

    set no_vpn_domain_ip_set {
        type ipv4_addr;
    }

    set no_vpn_ip_set {
        type ipv4_addr;flags interval;
    }

    chain prerouting {
        type filter hook prerouting priority 0;
        {{.}}
    }

    chain select_export {
        ip daddr @no_vpn_ip_set accept
        ip daddr @no_vpn_domain_ip_set accept
        jump vpn
    }

    chain vpn {
        reject
    }
}
`

type Interface struct {
	Name   string `yaml:"name"`
	Weight int    `yaml:"weight"`
	Mark   string `yaml:"mark"`
}

type Config struct {
	VpnInterfaces      []Interface `yaml:"vpn-interfaces"`
	LanInterfaces      []string    `yaml:"lan-interfaces"`
	NoVpnIps           []string    `yaml:"no-vpn-ips"`
	PingAddresses      []string    `yaml:"ping-addresses"`
	PingTimeoutSeconds int         `yaml:"ping-timeout-seconds"`
}

var (
	vpnInterfaces = map[string]*ethernet{}
	cancel        func()
)

func Init(conf Config) error {
	var v string
	if len(conf.LanInterfaces) == 1 {
		v = fmt.Sprintf("iifname %s jump select_export", conf.LanInterfaces[0])
	} else if len(conf.LanInterfaces) > 1 {
		v = fmt.Sprintf("iifname { %s } jump select_export", strings.Join(conf.LanInterfaces, ","))
	}
	pingTimeout := time.Duration(conf.PingTimeoutSeconds) * time.Second
	//初始化两个map
	for _, vpnIf := range conf.VpnInterfaces {
		e := &ethernet{
			Interface:       vpnIf,
			pingTimeout:     pingTimeout,
			pingAddr:        conf.PingAddresses,
			onStatusChanged: setVpnChainRules,
		}
		vpnInterfaces[vpnIf.Name] = e
	}

	//创建nftable
	tmp, err := template.New("table").Parse(tableTmp)
	if err != nil {
		return errors.WithStack(err)
	}
	var buf bytes.Buffer
	err = tmp.Execute(&buf, v)
	if err != nil {
		return errors.WithStack(err)
	}
	log.Println(buf.String())
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(buf.String())
	if err = runCmd(cmd); err != nil {
		return err
	}

	if len(conf.NoVpnIps) > 0 {
		cmd = exec.Command("nft", "add", "element", "ip", "shield_link", "no_vpn_ip_set", fmt.Sprintf("{ %s }", strings.Join(conf.NoVpnIps, ",")))
		if err = runCmd(cmd); err != nil {
			return err
		}
	}

	//初始化nft规则
	ctx, c := context.WithCancel(context.Background())
	cancel = c
	wg := sync.WaitGroup{}
	for _, e := range vpnInterfaces {
		wg.Add(1)
		e := e
		go func() {
			e.keepCheck(ctx)
			wg.Done()
		}()
	}
	wg.Wait()
	if err = setVpnChainRules(); err != nil {
		log.Printf("set vpn chain rule error : %+v", err)
	}
	return nil
}

var clearIprouteCommands []*exec.Cmd

func clearRouteRules() error {
	for _, command := range clearIprouteCommands {
		if err := runCmd(command); err != nil {
			log.Printf("clear ip route rule error %+v\n", err)
			continue
		}
	}
	clearIprouteCommands = nil
	return nil
}

func AddNoVpnDomainIp(ips ...string) error {
	if len(ips) == 0 {
		return nil
	}
	cmd := exec.Command("nft", "add", "element", "ip", "shield_link", "no_vpn_domain_ip_set", fmt.Sprintf("{ %s }", strings.Join(ips, ",")))
	return runCmd(cmd)
}

func DelNoVpnDomainIp(ips ...string) error {
	if len(ips) == 0 {
		return nil
	}
	cmd := exec.Command("nft", "delete", "element", "ip", "shield_link", "no_vpn_domain_ip_set", fmt.Sprintf("{ %s }", strings.Join(ips, ",")))
	return runCmd(cmd)
}

func FlushNoVpnDomainIp() error {
	cmd := exec.Command("nft", "flush", "set", "ip", "shield_link", "no_vpn_domain_ip_set")
	return runCmd(cmd)
}

func setVpnChainRules() error {
	//clear
	{
		cmd := exec.Command("nft", "flush", "chain", "ip", "shield_link", "vpn")
		if err := runCmd(cmd); err != nil {
			return err
		}
	}
	var list []*ethernet
	total := 0
	for _, e := range vpnInterfaces {
		if e.status == available {
			list = append(list, e)
			weight := e.Weight
			if weight < 1 {
				weight = 1
			}
			total += weight
		}
	}
	if len(list) == 0 {
		cmd := exec.Command("nft", "add", "rule", "ip", "shield_link", "vpn", "reject")
		return runCmd(cmd)
	}
	if len(list) == 1 {
		cmd := exec.Command("nft", "add", "rule", "ip", "shield_link", "vpn", "meta", "mark", "set", list[0].Mark)
		return runCmd(cmd)
	}

	current := 0
	betweenArr := make([]string, len(list))
	for i, s := range list {
		start := current
		end := 100
		weight := s.Weight
		if weight < 1 {
			weight = 1
		}
		if i+1 < len(list) {
			end = int((float64(weight) / float64(total)) * 100)
		}
		current = end + 1
		betweenArr[i] = fmt.Sprintf("%d-%d : %s", start, end, s.Mark)
	}
	between := fmt.Sprintf("{ %s }", strings.Join(betweenArr, ","))
	cmd := exec.Command("nft", "add", "rule", "ip", "shield_link", "vpn", "ct", "state", "established,related", "meta", "mark", "set", "ct", "mark")
	if err := runCmd(cmd); err != nil {
		return err
	}
	cmd = exec.Command("nft", "add", "rule", "ip", "shield_link", "vpn", "ct", "status", "new", "meta", "mark", "set", "numgen", "inc", "mod", "100", "map", between)
	return runCmd(cmd)
}

func ClearAll() error {
	cancel()
	if err := clearRouteRules(); err != nil {
		return err
	}
	cmd := exec.Command("nft", "delete", "table", "ip", "shield_link")
	return runCmd(cmd)
}

func runCmd(cmd *exec.Cmd) error {
	output, err := cmd.CombinedOutput() // 获取命令的输出和错误
	if err != nil {
		return errors.Wrapf(err, "failed to execute cmd '%s', output: %s", cmd.String(), string(output))
	}
	return nil
}
