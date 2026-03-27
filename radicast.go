package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type Radicast struct {
	reloadChan chan struct{}
	saveChan   chan *Radiko
	configPath string
	cron       *cron.Cron
	m          sync.Mutex
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	host       string
	port       string
	title      string
	output     string
	buffer     int64
	converter  string
	server     *Server
}

type StationInfoMap map[string]StationInfo

var stationInfoMap StationInfoMap

func NewRadicast(path string, host string, port string, title string, output string, buffer int64, converter string) *Radicast {
	ctx, cancel := context.WithCancel(context.Background())

	r := &Radicast{
		reloadChan: make(chan struct{}),
		saveChan:   make(chan *Radiko),
		configPath: path,
		ctx:        ctx,
		cancel:     cancel,
		host:       host,
		port:       port,
		title:      title,
		output:     output,
		buffer:     buffer,
		converter:  converter,
	}
	return r
}

func (r *Radicast) Run() error {

	t := &Radiko{}
	err := t.FullStationInfoMap(r.ctx)
	if err != nil {
		return err
	}

	if err := r.ReloadConfig(); err != nil {
		return err
	}

	if _, err := os.Stat(r.output); err != nil {
		if err := os.MkdirAll(r.output, 0777); err != nil {
			return err
		}
	}

	r.server = &Server{
		Output: r.output,
		Title:  r.title,
		Addr:   net.JoinHostPort(r.host, r.port),
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		if err := r.server.Run(); err != nil {
			r.Log(err)
			r.Stop()
		}
	}()

	for {
		select {
		case <-r.ctx.Done():
			done := make(chan struct{})
			go func() {
				r.wg.Wait()
				close(done)
			}()

			select {
			case <-done:
				return r.ctx.Err()
			case <-time.After(time.Second * 15):
				r.Log("Timeout waiting for shutdown")
				return r.ctx.Err()
			}
		case <-r.reloadChan:
			if err := r.ReloadConfig(); err != nil {
				r.Log(err)
			}
		// if same program is recorded, write files as parallely and may occure error. so write file as serially by channel.
		case radiko := <-r.saveChan:
			func() {
				defer os.RemoveAll(radiko.TempDir)
				if err := radiko.Result.Save(r.output); err != nil {
					r.Log(err)
				}
			}()
		}
	}
}

func (r *Radicast) Stop() {
	if r.server != nil {
		r.server.Shutdown()
	}

	if r.cron != nil {
		r.cron.Stop()
	}

	r.cancel()
}

func (r *Radicast) ReloadConfig() error {
	r.m.Lock()
	defer r.m.Unlock()

	if r.cron != nil {
		r.cron.Stop()
		r.Log("stop previous cron")
	}

	config, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}

	c := cron.New()
	for station, specs := range config {

		if station == "-RADIKO_MAIL-" {
			*radikoMail = specs[0]
			continue
		}
		if station == "-RADIKO_PASS-" {
			decPass, err := DecryptAES(specs[0])
			if err == nil {
				*radikoPass = decPass
			}
			continue
		}

		for _, spec := range specs {
			func(station string, spec string) {
				r.Log("station: ", station, " / spec: ", spec)
				c.AddFunc(spec, func() {
					r.wg.Add(1)
					defer r.wg.Done()

					dir, err := os.MkdirTemp("", "radiko")
					if err != nil {
						r.Log(err)
						return
					}

					stationInfo := stationInfoMap[station]
					radiko := &Radiko{
						Station:     station,
						Buffer:      r.buffer,
						Converter:   r.converter,
						TempDir:     dir,
						Premium:     false,
						StationInfo: stationInfo,
						Login: LoginStatus{
							Status:   "200",
							AreaFree: "0",
						},
					}

					if err := radiko.Run(r.ctx); err != nil {
						os.RemoveAll(radiko.TempDir)
						r.Log(err)
						return
					}

					r.saveChan <- radiko
				})
			}(station, spec)
		}
	}
	c.Start()
	r.cron = c
	r.Log("start new cron")

	return nil
}

func (r *Radicast) Log(v ...interface{}) {
	log.Println("[radicast]", fmt.Sprint(v...))
}
