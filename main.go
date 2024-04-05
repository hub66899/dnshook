package main

import (
	"dnshook/config"
	"dnshook/dnsserver"
	"dnshook/network"
	"log"
	"os"
	"os/signal"
	"path"
	"syscall"
)

type Config struct {
	Dns     dnsserver.Config `yaml:"dns"`
	Network network.Config   `yaml:"network"`
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
	Network: network.Config{
		Vpn: []network.Interface{
			{Name: "vpn1", Weight: 1},
		},
		Wan: []network.Interface{
			{Name: "eth3", Weight: 1},
		},
		Lan:                []string{"eth0"},
		IgnoreAddr:         []string{"192.168.0.0/16"},
		PingAddr:           []string{"8.8.8.8", "cloudflare.com"},
		PingTimeoutSeconds: 5,
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
	if err := network.Init(conf.Network); err != nil {
		log.Fatalf("Failed to init rule: %+v\n", err)
	}
	go func() {
		if err := dnsserver.Start(conf.Dns); err != nil {
			log.Fatalf("Failed to start dns server: %+v\n", err)
		}
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
	if err := network.ClearAll(); err != nil {
		log.Printf("Failed to clear rules: %v\n", err)
	}
	if err := dnsserver.Stop(); err != nil {
		log.Printf("Failed to stop dns server: %v", err)
	}
	log.Printf("Service closed")
}
