package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type Radicast struct {
	reloadChan         chan struct{}
	saveChan           chan *Radiko
	configPath         string
	cron               *cron.Cron
	titleSearchEntries map[string]map[cron.EntryID]struct{}
	m                  sync.Mutex
	wg                 sync.WaitGroup
	ctx                context.Context
	cancel             context.CancelFunc
	host               string
	port               string
	title              string
	output             string
	buffer             int64
	converter          string
	radikoMail         string
	radikoPass         string
	server             *Server
}

func titleSearchKey(station string, title string, start time.Time) string {
	return station + "::" + strings.ToLower(title) + "::" + start.Format(time.RFC3339)
}

type StationInfoMap map[string]StationInfo

func parseTitleSpec(spec string) (string, bool) {
	trimmed := strings.TrimSpace(spec)
	prefix := "title:"
	if !strings.HasPrefix(strings.ToLower(trimmed), prefix) {
		return "", false
	}

	query := strings.TrimSpace(trimmed[len(prefix):])
	if query == "" {
		return "", false
	}
	return query, true
}

func titleContainsQuery(programTitle string, query string) bool {
	if query == "" {
		return false
	}
	return strings.Contains(strings.ToLower(programTitle), strings.ToLower(query))
}

func cronSpecFromProgramTime(programTime time.Time) string {
	return fmt.Sprintf("%d %d %d %d *", programTime.Minute(), programTime.Hour(), programTime.Day(), programTime.Month())
}

func dateKeyInLocation(t time.Time, loc *time.Location) string {
	if loc == nil {
		return t.Format("2006-01-02")
	}
	return t.In(loc).Format("2006-01-02")
}

func previousDateKey(t time.Time, loc *time.Location) string {
	return dateKeyInLocation(t.AddDate(0, 0, -1), loc)
}

var stationInfoMap StationInfoMap

func NewRadicast(path string, host string, port string, title string, output string, buffer int64, converter string) *Radicast {
	ctx, cancel := context.WithCancel(context.Background())

	r := &Radicast{
		reloadChan: make(chan struct{}),
		saveChan:   make(chan *Radiko, 1),
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

func (r *Radicast) recordAtStation(station string, programSpec string) {
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
		ProgramSpec: programSpec,
		Buffer:      r.buffer,
		Converter:   r.converter,
		TempDir:     dir,
		Premium:     false,
		RadikoMail:  r.radikoMail,
		RadikoPass:  r.radikoPass,
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
}

func (r *Radicast) addTitleSearchEntry(searchDate string, id cron.EntryID) {
	if r.titleSearchEntries == nil {
		r.titleSearchEntries = make(map[string]map[cron.EntryID]struct{})
	}
	if _, ok := r.titleSearchEntries[searchDate]; !ok {
		r.titleSearchEntries[searchDate] = make(map[cron.EntryID]struct{})
	}
	r.titleSearchEntries[searchDate][id] = struct{}{}
}

func (r *Radicast) removeTitleSearchEntry(searchDate string, id cron.EntryID) {
	r.Log("removeTitleSearchEntry: ", searchDate, " / ", id)
	if r.cron != nil {
		r.cron.Remove(id)
	}
	ids, ok := r.titleSearchEntries[searchDate]
	if !ok {
		return
	}
	delete(ids, id)
	if len(ids) == 0 {
		delete(r.titleSearchEntries, searchDate)
	}
}

func (r *Radicast) runTitleSearch(c *cron.Cron, station string, title string, when time.Time) {
	jst, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		jst = time.Local
	}
	searchDate := dateKeyInLocation(when, jst)

	radiko := &Radiko{}
	progs, err := radiko.stationProgramsForDate(r.ctx, station, when)
	if err != nil {
		r.Log(err)
		return
	}

	for _, s := range progs.Stations.Station {
		if s.ID != station {
			continue
		}
		for i := range s.Progs.Prog {
			prog := s.Progs.Prog[i]
			if !titleContainsQuery(prog.Title, title) {
				continue
			}

			start, err := prog.FtTime()
			if err != nil {
				r.Log(err)
				continue
			}
			if !start.After(when) {
				continue
			}

			progCopy := prog
			spec := cronSpecFromProgramTime(start)
			targetID := cron.EntryID(0)
			entryFunc := func() {
				r.recordAtStation(station, titleSearchKey(station, progCopy.Title, start))
				r.removeTitleSearchEntry(searchDate, targetID)
			}
			targetID, err = c.AddFunc(spec, entryFunc)
			if err != nil {
				r.Log("failed to add title schedule: ", station, " / ", progCopy.Title, " / ", spec, " / ", err)
				continue
			}
			r.addTitleSearchEntry(searchDate, targetID)
			r.Log("title search found: ", station, " / ", progCopy.Title, " / ", spec)
		}
	}
}

func (r *Radicast) scheduleTitleSearch(c *cron.Cron, station string, title string) error {
	_, err := c.AddFunc("45 4 * * *", func() {
		r.runTitleSearch(c, station, title, time.Now())
	})
	if err != nil {
		return err
	}

	r.runTitleSearch(c, station, title, time.Now())
	r.Log("station: ", station, " / title: ", title)
	return nil
}

func (r *Radicast) ReloadConfig() error {
	r.m.Lock()
	defer r.m.Unlock()

	if r.cron != nil {
		r.cron.Stop()
		r.Log("stop previous cron")
	}

	config, err := LoadConfig(r.configPath)
	if err != nil {
		return err
	}

	r.radikoMail = config.RadikoMail
	if config.RadikoPass != "" {
		radikoPass, err := DecryptAES(config.RadikoPass)
		if err != nil {
			return err
		}
		r.radikoPass = radikoPass
	} else {
		r.radikoPass = ""
	}

	r.titleSearchEntries = make(map[string]map[cron.EntryID]struct{})

	c := cron.New()
	for station, specs := range config.Stations {
		for _, spec := range specs {
			if query, ok := parseTitleSpec(spec); ok {
				if err := r.scheduleTitleSearch(c, station, query); err != nil {
					return err
				}
				continue
			}

			if err := func(station string, spec string) error {
				r.Log("station: ", station, " / spec: ", spec)
				_, err := c.AddFunc(spec, func() {
					r.recordAtStation(station, "cron : "+spec)
				})
				return err
			}(station, spec); err != nil {
				return err
			}
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
