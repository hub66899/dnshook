package main

import (
	"dnshook/dnsserver"
	"dnshook/network"
	"dnshook/pkg/shutdown"
	"github.com/sirupsen/logrus"
)

func main() {
	go func() {
		if err := dnsserver.Start(); err != nil {
			logrus.WithError(err).Fatal("Failed to start dns server")
		}
	}()
	go func() {
		if err := network.Start(dnsserver.GetNoVpnIPs); err != nil {
			logrus.WithError(err).Fatal("Failed to start network")
		}
	}()
	logrus.Info("Started")
	shutdown.Wait()
}
