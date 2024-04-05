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

    set ignore_daddr {
        type ipv4_addr;flags interval;
    }

    chain prerouting {
        type filter hook prerouting priority 0;
        {{.}}
    }

    chain select_export {
        ip daddr @ignore_daddr accept
        ip daddr @no_vpn_domain_ip_set jump wan
        jump vpn
    }

    chain vpn {
    }

    chain wan {
    }
}
`

type Interface struct {
	Name   string `yaml:"name"`
	Weight int    `yaml:"weight"`
}

type Config struct {
	Vpn                []Interface `yaml:"vpn"`
	Wan                []Interface `yaml:"wan"`
	Lan                []string    `yaml:"lan"`
	IgnoreAddr         []string    `yaml:"ignore-addr"`
	PingAddr           []string    `yaml:"ping-addr"`
	PingTimeoutSeconds int         `yaml:"ping-timeout-seconds"`
}

var (
	vpnInterfaces = map[string]*ethernet{}
	wanInterfaces = map[string]*ethernet{}
	cancel        func()
)

func Init(conf Config) error {
	var v string
	if len(conf.Lan) == 1 {
		v = fmt.Sprintf("iifname %s jump select_export", conf.Lan[0])
	} else if len(conf.Lan) > 1 {
		v = fmt.Sprintf("iifname { %s } jump select_export", strings.Join(conf.Lan, ","))
	}

	n := 1000

	//初始化两个map
	for _, i2 := range conf.Vpn {
		n++
		e := &ethernet{
			Interface:       i2,
			mark:            fmt.Sprintf("%#x", n),
			table:           fmt.Sprintf("%d", n),
			pingTimeout:     time.Duration(conf.PingTimeoutSeconds) * time.Second,
			pingAddr:        conf.PingAddr,
			onStatusChanged: setVpnChainRules,
		}
		vpnInterfaces[i2.Name] = e
	}
	for _, i2 := range conf.Wan {
		n++
		e := &ethernet{
			Interface:       i2,
			mark:            fmt.Sprintf("%#x", n),
			table:           fmt.Sprintf("%d", n),
			pingTimeout:     time.Duration(conf.PingTimeoutSeconds) * time.Second,
			pingAddr:        conf.PingAddr,
			onStatusChanged: setWanChainRules,
		}
		wanInterfaces[i2.Name] = e
	}

	//路由规则
	if err := setRouteRules(); err != nil {
		return err
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

	if len(conf.IgnoreAddr) > 0 {
		cmd = exec.Command("nft", "add", "element", "ip", "shield_link", "ignore_daddr", fmt.Sprintf("{ %s }", strings.Join(conf.IgnoreAddr, ",")))
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
	for _, e := range wanInterfaces {
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
	if err = setWanChainRules(); err != nil {
		log.Printf("set wan chain rule error : %+v", err)
	}
	return nil
}

func setRouteRules() error {
	for _, i := range wanInterfaces {
		cmd := exec.Command("ip", "route", "replace", "default", "dev", i.Name, "table", i.table)
		if err := runCmd(cmd); err != nil {
			return err
		}
		cmd = exec.Command("ip", "rule", "add", "fwmark", i.mark, "lookup", i.table)
		if err := runCmd(cmd); err != nil {
			return err
		}
		clearIprouteCommands = append(clearIprouteCommands,
			exec.Command("ip", "route", "del", "default", "dev", i.Name, "table", i.table),
			exec.Command("ip", "rule", "del", "fwmark", i.mark, "lookup", i.table),
		)
	}
	for _, i := range vpnInterfaces {
		cmd := exec.Command("ip", "route", "replace", "default", "dev", i.Name, "table", i.table)
		if err := runCmd(cmd); err != nil {
			return err
		}
		cmd = exec.Command("ip", "rule", "add", "fwmark", i.mark, "lookup", i.table)
		if err := runCmd(cmd); err != nil {
			return err
		}
		clearIprouteCommands = append(clearIprouteCommands,
			exec.Command("ip", "route", "del", "default", "dev", i.Name, "table", i.table),
			exec.Command("ip", "rule", "del", "fwmark", i.mark, "lookup", i.table),
		)
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
		cmd := exec.Command("nft", "add", "rule", "ip", "shield_link", "vpn", "meta", "mark", "set", list[0].mark)
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
		betweenArr[i] = fmt.Sprintf("%d-%d : %s", start, end, s.mark)
	}
	between := fmt.Sprintf("{ %s }", strings.Join(betweenArr, ","))
	cmd := exec.Command("nft", "add", "rule", "ip", "shield_link", "vpn", "ct", "state", "established,related", "meta", "mark", "set", "ct", "mark")
	if err := runCmd(cmd); err != nil {
		return err
	}
	cmd = exec.Command("nft", "add", "rule", "ip", "shield_link", "vpn", "ct", "status", "new", "meta", "mark", "set", "numgen", "inc", "mod", "100", "map", between)
	return runCmd(cmd)
}

func setWanChainRules() error {
	//clear
	{
		cmd := exec.Command("nft", "flush", "chain", "ip", "shield_link", "wan")
		if err := runCmd(cmd); err != nil {
			return err
		}
	}
	var list []*ethernet
	total := 0
	for _, e := range wanInterfaces {
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
		return nil
	}
	if len(list) == 1 {
		cmd := exec.Command("nft", "add", "rule", "ip", "shield_link", "wan", "meta", "mark", "set", list[0].mark)
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
		betweenArr[i] = fmt.Sprintf("%d-%d : %s", start, end, s.mark)
	}
	between := fmt.Sprintf("{ %s }", strings.Join(betweenArr, ","))
	cmd := exec.Command("nft", "add", "rule", "ip", "shield_link", "wan", "ct", "state", "established,related", "meta", "mark", "set", "ct", "mark")
	if err := runCmd(cmd); err != nil {
		return err
	}
	cmd = exec.Command("nft", "add", "rule", "ip", "shield_link", "wan", "ct", "status", "new", "meta", "mark", "set", "numgen", "inc", "mod", "100", "map", between)
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
