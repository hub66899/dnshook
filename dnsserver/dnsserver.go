package dnsserver

import (
	"context"
	"dnshook/network"
	"dnshook/pkg/config"
	"dnshook/pkg/shutdown"
	"fmt"
	"github.com/miekg/dns"
	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"log"
	"net"
	"strings"
	"time"
)

type Config struct {
	Upstreams    []string `yaml:"upstreams"`
	NoVpnDomains []string `yaml:"no-vpn-domains"`
	Port         int      `yaml:"port"`
}

var defaultConfig = Config{
	Upstreams:    []string{"8.8.8.8:53", "1.1.1.1:53", "8.8.4.4:53"},
	NoVpnDomains: []string{"cip.cc", "chatgpt", "spotify", "netflix", "figma", "google", "youtube", "netflix", "facebook", "instagram", "apple", "openai", "github", "cloudflare", "notion", "ubuntu", "docker", "golang", "maven", "npmjs"},
	Port:         5353,
}

const (
	dataFile   = "/etc/vpnmanager/data"
	configFile = "/etc/vpnmanager/dns.yml"
)

var (
	expiration = time.Hour * 24 * 2
	cacheData  *cache.Cache
	server     *dns.Server
	conf       *Config
)

func isNoVpnDomain(domain string) bool {
	for _, d := range conf.NoVpnDomains {
		if strings.Contains(domain, d) {
			return true
		}
	}
	return false
}

func GetNoVpnIPs() []string {
	var ips []string
	for ip := range cacheData.Items() {
		ips = append(ips, ip)
	}
	return ips
}

func Start() error {
	if conf == nil {
		c := config.LocalYamlConfig[Config](configFile, defaultConfig)
		cf := c.Get()
		conf = &cf
		if err := c.Watch(func(c Config) {
			logrus.Info("config changed")
			conf = &c
		}); err != nil {
			logrus.WithError(err).Error("watch config failed")
		}
	}
	if cacheData == nil {
		cacheData = cache.New(expiration, time.Hour)
		cacheData.OnEvicted(func(s string, i interface{}) {
			if err := network.DelNoVpnDomainIp(s); err != nil {
				logrus.WithError(err).Error("del no vpn domain ip failed")
			}
		})
		if err := cacheData.LoadFile(dataFile); err != nil {
			logrus.WithError(err).Error("load cache file failed")
		}
	}

	if server == nil {
		dns.HandleFunc(".", func(w dns.ResponseWriter, r *dns.Msg) {
			client := &dns.Client{}
			var msg *dns.Msg
			for _, upstream := range conf.Upstreams {
				response, _, err := client.Exchange(r, upstream)
				if err == nil {
					msg = response
					break
				}
				logrus.WithError(err).WithField("upstream", upstream).Error("Failed to forward query to upstream")
			}
			if msg == nil {
				// 如果转发失败，返回服务器失败响应
				m := new(dns.Msg)
				m.SetRcode(r, dns.RcodeServerFailure)
				_ = w.WriteMsg(m)
				return
			}
			name := r.Question[0].Name
			if isNoVpnDomain(name) {
				logrus.WithField("domain", name).Info("no vpn domain")
				for _, ans := range msg.Answer {
					switch a := ans.(type) {
					case *dns.A:
						if err := addIp(a.A); err != nil {
							logrus.WithError(err).WithField("ip", a.A.String()).Error("add ip failed")
						}
					case *dns.HTTPS:
						for _, value := range a.Value {
							if value.Key() == dns.SVCB_IPV4HINT {
								ipv4Hints := strings.Split(value.String(), ",")
								for _, hint := range ipv4Hints {
									ip := net.ParseIP(hint)
									if ip != nil {
										if err := addIp(ip); err != nil {
											logrus.WithError(err).WithField("ip", ip.String()).Error("add ip failed")
										}
									}
								}
							}
						}
					}
					logrus.Info(ans.String())
				}
			}

			if err := w.WriteMsg(msg); err != nil {
				log.Printf("Failed to write response: %v", err)
			}
		})
		server = &dns.Server{Addr: fmt.Sprintf(":%d", conf.Port), Net: "udp"}
		shutdown.OnShutdown(func(ctx context.Context) error {
			if err := cacheData.SaveFile(dataFile); err != nil {
				logrus.WithError(err).Error("save cache file failed")
			}
			return server.Shutdown()
		})
	}
	return errors.WithStack(server.ListenAndServe())
}

func addIp(ip net.IP) error {
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("ip %s invalid", ip)
	}
	ipStr := ip.To4().String()
	_, exist := cacheData.Get(ipStr)
	defer func() {
		cacheData.Set(ipStr, "", expiration)
	}()
	if exist {
		return nil
	}
	logrus.WithField("ip", ipStr).Info("added ip")
	return network.AddNoVpnDomainIp(ipStr)
}
