package dnsserver

import (
	"dnshook/network"
	"fmt"
	"github.com/miekg/dns"
	cache2 "github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	"log"
	"net"
	"regexp"
	"strings"
	"time"
)

type Config struct {
	Upstreams    []string `yaml:"upstreams"`
	NoVpnDomains []string
	Port         int    `yaml:"port"`
	DataFile     string `yaml:"data-file"`
}

var (
	expiration = time.Hour * 24 * 2
	cache      *cache2.Cache
	server     *dns.Server
	dataFile   string
)

func Start(conf Config) error {
	dataFile = conf.DataFile
	reg, err := regexp.Compile(strings.Join(conf.NoVpnDomains, "|"))
	if err != nil {
		return errors.WithStack(err)
	}
	cache = cache2.New(expiration, time.Hour)
	cache.OnEvicted(func(s string, i interface{}) {
		if err = network.DelNoVpnDomainIp(s); err != nil {
			log.Printf("%v\n", err)
		}
	})
	if err = cache.LoadFile(dataFile); err != nil {
		log.Printf("failed to load cache file : %v", err)
		return nil
	}
	var ips []string
	for ip := range cache.Items() {
		ips = append(ips, ip)
	}
	if len(ips) > 0 {
		if err = network.AddNoVpnDomainIp(ips...); err != nil {
			return err
		}
	}
	dns.HandleFunc(".", func(w dns.ResponseWriter, r *dns.Msg) {
		client := &dns.Client{}
		var msg *dns.Msg
		for _, upstream := range conf.Upstreams {
			response, _, err := client.Exchange(r, upstream)
			if err == nil {
				msg = response
				break
			}
			log.Printf("Failed to forward query to upstream %s: %v", upstream, err)
		}
		if msg == nil {
			// 如果转发失败，返回服务器失败响应
			m := new(dns.Msg)
			m.SetRcode(r, dns.RcodeServerFailure)
			_ = w.WriteMsg(m)
			return
		}

		//是否在名单
		if reg.MatchString(r.Question[0].Name) {
			for _, ans := range msg.Answer {
				if a, ok := ans.(*dns.A); ok {
					if err := addIp(a.A); err != nil {
						log.Printf("add ip %s failed:%v", a.A.String(), err)
					}
				}
			}
		}

		if err := w.WriteMsg(msg); err != nil {
			log.Printf("Failed to write response: %v", err)
		}
	})
	server = &dns.Server{Addr: fmt.Sprintf(":%d", conf.Port), Net: "udp"}
	return errors.WithStack(server.ListenAndServe())
}

func Stop() error {
	if err := cache.SaveFile(dataFile); err != nil {
		log.Printf("save cache file error:%v", err)
	}
	return errors.WithStack(server.Shutdown())
}

func addIp(ip net.IP) error {
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("ip %s invalid", ip)
	}
	ipStr := ip.To4().String()
	_, exist := cache.Get(ipStr)
	defer func() {
		cache.Set(ipStr, "", expiration)
	}()
	if exist {
		return nil
	}
	log.Printf("added ip %s\n", ipStr)
	return network.AddNoVpnDomainIp(ipStr)
}
