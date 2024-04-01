package main

import (
	"dnshook/config"
	"dnshook/dnsserver"
	"dnshook/health"
	"dnshook/rule"
	"log"
	"os"
	"os/signal"
	"path"
	"syscall"
)

type Config struct {
	Dns               dnsserver.Config `yaml:"dns"`
	Health            health.Config    `yaml:"health"`
	NoVpnIps          []string         `yaml:"no-vpn-ips"`
	ControlInterfaces []string         `yaml:"control-interfaces"`
}

const (
	confDir = "/etc/shieldlink"
)

var defaultConfig = Config{
	Dns: dnsserver.Config{
		Upstreams:    []string{"8.8.8.8:53", "1.1.1.1:53", "8.8.4.4:53"},
		NoVpnDomains: []string{"google", "apple", "openai", "github", "cloudflare", "notion", "ubuntu", "docker", "golang"},
		Port:         5353,
		DataFile:     path.Join(confDir, "dns.data"),
	},
	Health: health.Config{
		PingAddr:           []string{"8.8.8.8", "cloudflare.com"},
		PingTimeoutSeconds: 5,
		Vpn: []health.Interface{
			{Name: "vpn1", Weight: 1},
		},
		Wan: []health.Interface{
			{Name: "wan1", Weight: 1},
		},
	},
}

func main() {
	var conf Config
	{
		c, err := config.LocalYamlConfig[Config](path.Join(confDir, "config.yml"), defaultConfig)
		if err != nil {
			log.Fatalf("failed to init cache: %v", err)
		}
		conf = *c.GetConfig()
	}
	if err := rule.Init(); err != nil {
		log.Fatalf("Failed to init rule: %v\n", err)
	}
	if err := rule.AddNoVpnIp(conf.NoVpnIps...); err != nil {
		log.Fatalf("Failed to add no vpn ips: %v\n", err)
	}
	if err := rule.AddControlEthernet(conf.ControlInterfaces...); err != nil {
		log.Fatalf("Failed to add control ethernet: %v\n", err)
	}
	go func() {
		if err := dnsserver.Start(conf.Dns); err != nil {
			log.Fatalf("Failed to start dns server: %v\n", err)
		}
	}()
	if err := health.Start(conf.Health); err != nil {
		log.Fatalf("Failed to start health service: %v\n", err)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
	if err := rule.ClearAll(); err != nil {
		log.Printf("Failed to clear rules: %v\n", err)
	}
	if err := dnsserver.Stop(); err != nil {
		log.Printf("Failed to stop dns server: %v", err)
	}
	health.Stop()
	log.Printf("Service closed")
}
