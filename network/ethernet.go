package network

import (
	"bufio"
	"context"
	"github.com/pkg/errors"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type ethernet struct {
	Interface
	mark            string
	table           string
	status          status
	onStatusChanged func() error
	pingAddr        []string
	pingTimeout     time.Duration
}

type status int8

const (
	available   status = 1
	unavailable status = 2
)

func (l *ethernet) keepCheck(ctx context.Context) {
	initCtx, done := context.WithTimeout(ctx, time.Second*5)
	defer done()

	go func() {
		i := 0 //ping地址index
		failedTimes := 0
		for {
			if ctx.Err() != nil {
				return
			}
			addr := l.pingAddr[i]
			ch := make(chan string, 1)
			go func() {
				for _ = range ch {
					failedTimes = 0
					if l.status != available {
						l.status = available
						if l.onStatusChanged != nil && initCtx.Err() != nil {
							if err := l.onStatusChanged(); err != nil {
								log.Printf("call back error: %v", err)
							}
						}
					}
					if initCtx.Err() == nil {
						done()
					}
				}
			}()
			err := ping(ctx, ch, l.Name, addr, l.pingTimeout)
			close(ch)
			if err != nil {
				log.Printf("failed to ping %v", err)
				failedTimes++
				if l.status != unavailable && failedTimes > 2 {
					l.status = unavailable
					if l.onStatusChanged != nil && initCtx.Err() != nil {
						if err = l.onStatusChanged(); err != nil {
							log.Printf("call back error: %v", err)
						}
					}
				}
				time.Sleep(time.Second * 2)
				i++
				if i == len(l.pingAddr) {
					i = 0
				}
			}
		}
	}()
	<-initCtx.Done()
}

func ping(ctx context.Context, ch chan<- string, oifname string, address string, pingTimeout time.Duration) error {
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
