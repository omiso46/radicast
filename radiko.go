package main

// api for radiko, rtmpdump and ffmpeg command parameter
// are taken from
// https://github.com/miyagawa/ripdiko
// https://gist.github.com/saiten/875864

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	radikoTimeLayout    = "20060102150405"
	authKey             = "bcd151073c03b352e1ef2fd66c32209da9ca0afa"
	auth1URL            = "https://radiko.jp/v2/api/auth1"
	auth2URL            = "https://radiko.jp/v2/api/auth2%s"
	auth2PRM            = "?radiko_session=%s"
	streamMultiURL      = "https://radiko.jp/v3/station/stream/pc_html5/%s.xml"
	localStationListURL = "https://radiko.jp/v3/station/list/%s.xml"
	fullStationListURL  = "https://radiko.jp/v3/station/region/full.xml"
	todayPrgramURL      = "https://radiko.jp/v3/program/station/date/%s/%s.xml"
	loginURL            = "https://radiko.jp/v4/api/member/login"
	logoutURL           = "https://radiko.jp/v4/api/member/logout"
	playlistURL         = "%s?station_id=%s&l=15&lsid=&type=c"
)

type LoginStatus struct {
	RadikoSession string `json:"radiko_session"`
	Status        string `json:"status"`
	AreaFree      string `json:"areafree"`
	Unpaid        string `json:"unpaid"`
	PaidMember    string `json:"paid_member"`
	MemberUkey    string `json:"member_ukey"`
}

type StreamURLItem struct {
	AreaFree          bool   `xml:"areafree,attr"`
	TimeFree          bool   `xml:"timefree,attr"`
	PlaylistCreateURL string `xml:"playlist_create_url"`
}

type StreamUrlData struct {
	XMLName xml.Name        `XML:"urls"`
	URL     []StreamURLItem `xml:"url"`
}

type FullStations struct {
	Stations []struct {
		AsciiName  string    `xml:"ascii_name,attr"`
		RegionID   string    `xml:"region_id,attr"`
		RegionName string    `xml:"region_name,attr"`
		Station    []Station `xml:"station"`
	} `xml:"stations"`
}

type LocalStations struct {
	AreaID   string    `xml:"area_id,attr"`
	AreaName string    `xml:"area_name,attr"`
	Station  []Station `xml:"station"`
}

type Station struct {
	ID        string `xml:"id"`
	Name      string `xml:"name"`
	AsciiName string `xml:"ascii_name"`
	Ruby      string `xml:"ruby"`
	AreaFree  string `xml:"areafree"`
	TimeFree  string `xml:"timefree"`
	AreaID    string `xml:"area_id"`
}

type StationInfo struct {
	StationID    string
	StationName  string
	StationArea  string
	LocalArea    string
	AreaFree     bool
	TimeFree     bool
	LocalStation bool
}

type RadikoPrograms struct {
	Stations struct {
		Station []struct {
			ID    string `xml:"id,attr"`
			Name  string `xml:"name"`
			Progs struct {
				Date string       `xml:"date"`
				Prog []RadikoProg `xml:"prog"`
			} `xml:"progs"`
		} `xml:"station"`
	} `xml:"stations"`
}

type RadikoProg struct {
	XMLName     xml.Name `xml:"prog"`
	ID          string   `xml:"id,attr"`
	MasterID    string   `xml:"master_id,attr"`
	Ft          string   `xml:"ft,attr"`
	To          string   `xml:"to,attr"`
	Ftl         string   `xml:"ftl,attr"`
	Tol         string   `xml:"tol,attr"`
	Dur         string   `xml:"dur,attr"`
	Title       string   `xml:"title"`
	URL         string   `xml:"url"`
	Desc        string   `xml:"desc"`
	Info        string   `xml:"info"`
	Pfm         string   `xml:"pfm"`
	Img         string   `xml:"img"`
	StationID   string   `xml:"StationID"`
	StationName string   `xml:"StationName"`
}

func (r *RadikoProg) FtTime() (time.Time, error) {
	return time.ParseInLocation(radikoTimeLayout, r.Ft, time.Local)
}

func (r *RadikoProg) ToTime() (time.Time, error) {
	return time.ParseInLocation(radikoTimeLayout, r.To, time.Local)
}

func (r *RadikoProg) Duration() (int64, error) {
	to, err := r.ToTime()
	if err != nil {
		return 0, err
	}
	return to.Unix() - time.Now().Unix(), nil
}

type RadikoResult struct {
	MedPath  string
	Prog     *RadikoProg
	Station  string
	RecStart string
	RecEnd   string
}

func (r *RadikoResult) Save(dir string) error {
	programDir := filepath.Join(dir, fmt.Sprintf("%s_%s", r.RecStart, r.Station))
	if err := os.MkdirAll(programDir, 0777); err != nil {
		return err
	}

	medPath := filepath.Join(programDir, "podcast.m4a")
	xmlPath := filepath.Join(programDir, "podcast.xml")

	imgName := "podcast" + filepath.Ext(r.Prog.Img)
	imgPath := filepath.Join(programDir, imgName)

	if err := RenameOrCopy(r.MedPath, medPath); err != nil {
		return err
	}

	if err := RenameOrCopy(filepath.Dir(r.MedPath)+"/"+imgName, imgPath); err != nil {
		return err
	}

	xmlFile, err := os.Create(xmlPath)
	if err != nil {
		return err
	}

	defer xmlFile.Close()

	enc := xml.NewEncoder(xmlFile)
	enc.Indent("", "    ")
	if err := enc.Encode(r.Prog); err != nil {
		return err
	}

	r.Log("SavedPath ", programDir)

	return nil
}

func (r *RadikoResult) Log(v ...interface{}) {
	log.Println("[radiko_result]", fmt.Sprint(v...))
}

type Radiko struct {
	Station     string
	Buffer      int64
	Converter   string
	TempDir     string
	Premium     bool
	StationInfo StationInfo
	Login       LoginStatus
	Result      *RadikoResult
}

func (r *Radiko) Run(ctx context.Context) error {
	results := r.run(ctx)
	switch len(results) {
	case 0:
		return fmt.Errorf("empty outputs")
	case 1:
		r.Result = results[0]
		return nil
	default:
		result, err := r.ConcatOutput(r.TempDir, results)
		if err != nil {
			return err
		}
		r.Result = result
		return nil
	}
}

func (r *Radiko) run(ctx context.Context) []*RadikoResult {
	errChan := make(chan error)
	retry := 0
	c := make(chan struct{}, 1)

	results := []*RadikoResult{}
	record := func() error {
		output := filepath.Join(r.TempDir, fmt.Sprintf("radiko_%d.m4a", retry))
		ret, err := r.record(ctx, output, r.Station, r.Buffer)
		if ret != nil {
			results = append(results, ret)
		}

		return err
	}

	c <- struct{}{}

	for {
		select {
		case <-c:
			r.Log("start record: ", r.Station)
			go func() {
				errChan <- record()
			}()
		case <-ctx.Done():
			if err := ctx.Err(); err != nil {
				r.Log("context err:", err)
			}

			select {
			case err := <-errChan:
				r.Log("err: ", err)
			case <-time.After(time.Second * 10):
				r.Log("timeout receive err chan")
			}
			return results
		case err := <-errChan:
			r.Log("finished: ", r.Station)
			if err == nil {
				return results
			}

			// TODO stop if recod program is changed.
			r.Log("got err: ", err)

			if retry < 2 {
				sec := time.Second * 2
				time.AfterFunc(sec, func() {
					c <- struct{}{}
				})
				r.Log("retry after ", sec)
				retry++
			} else {
				return results
			}
		}
	}
}

// http://superuser.com/questions/314239/how-to-join-merge-many-mp3-files
func (r *Radiko) ConcatOutput(dir string, results []*RadikoResult) (*RadikoResult, error) {
	output := filepath.Join(dir, "radiko_concat.m4a")

	outputs := []string{}
	for _, result := range results {
		outputs = append(outputs, result.MedPath)
	}

	args := []string{
		"-i",
		fmt.Sprintf("concat:%s", strings.Join(outputs, "|")),
		"-acodec",
		"copy",
		output,
	}

	cmd := exec.Command(r.Converter, args...)
	r.Log("ConcatCmd ", strings.Join(cmd.Args, " "))

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return &RadikoResult{
		MedPath:  output,
		Station:  results[0].Station,
		Prog:     results[0].Prog,
		RecStart: results[0].RecStart,
		RecEnd:   results[0].RecEnd,
	}, nil
}

func (r *Radiko) FullStationInfoMap(ctx context.Context) error {

	_, area, err := r.auth(ctx)
	if err != nil {
		return err
	}

	lu, err := url.Parse(fmt.Sprintf(localStationListURL, area))
	if err != nil {
		return err
	}

	lreq, err := http.NewRequest("GET", lu.String(), nil)
	if err != nil {
		return err
	}

	var localStations LocalStations
	err = r.httpDo(ctx, lreq, func(resp *http.Response, err error) error {
		if err != nil {
			return err
		}

		defer resp.Body.Close()
		if code := resp.StatusCode; code != 200 {
			return fmt.Errorf("not status code:200, got:%d", code)
		}

		return xml.NewDecoder(resp.Body).Decode(&localStations)
	})
	if err != nil {
		return err
	}

	tmpStationInfoMap := make(map[string]string)
	for _, station := range localStations.Station {
		tmpStationInfoMap[station.ID] = station.ID
	}

	fu, err := url.Parse(fullStationListURL)
	if err != nil {
		return err
	}

	freq, err := http.NewRequest("GET", fu.String(), nil)
	if err != nil {
		return err
	}

	var fullStations FullStations
	err = r.httpDo(ctx, freq, func(resp *http.Response, err error) error {
		if err != nil {
			return err
		}

		defer resp.Body.Close()
		if code := resp.StatusCode; code != 200 {
			return fmt.Errorf("not status code:200, got:%d", code)
		}

		return xml.NewDecoder(resp.Body).Decode(&fullStations)
	})
	if err != nil {
		return err
	}

	stationInfoMap = make(StationInfoMap)
	for _, stations := range fullStations.Stations {
		for _, station := range stations.Station {
			if _, ok := tmpStationInfoMap[station.ID]; !ok {
				// not local area station
				stationInfoMap[station.ID] = StationInfo{
					StationID:    station.ID,
					StationName:  station.Name,
					StationArea:  station.AreaID,
					LocalArea:    area,
					AreaFree:     true,
					TimeFree:     false,
					LocalStation: false,
				}
			} else {
				// local area station
				stationInfoMap[station.ID] = StationInfo{
					StationID:    station.ID,
					StationName:  station.Name,
					StationArea:  station.AreaID,
					LocalArea:    area,
					AreaFree:     false,
					TimeFree:     false,
					LocalStation: true,
				}
			}
		}
	}

	return nil

}

func (r *Radiko) stationTodayPrograms(ctx context.Context, station string) (*RadikoPrograms, error) {
	const layoutDate = "20060102"
	const layoutTime = "150405"

	timeNow := time.Now()
	nowTime := timeNow.Format(layoutTime)
	tmpDate := timeNow

	if nowTime >= "000000" && nowTime < "050000" {
		tmpDate = timeNow.AddDate(0, 0, -1)
	}

	u, err := url.Parse(fmt.Sprintf(todayPrgramURL, tmpDate.Format(layoutDate), station))
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	var progs RadikoPrograms
	err = r.httpDo(ctx, req, func(resp *http.Response, err error) error {
		if err != nil {
			return err
		}

		defer resp.Body.Close()
		if code := resp.StatusCode; code != 200 {
			return fmt.Errorf("not status code:200, got:%d", code)
		}

		return xml.NewDecoder(resp.Body).Decode(&progs)
	})
	if err != nil {
		return nil, err
	}

	return &progs, nil

}

func (r *Radiko) stationNowProgram(ctx context.Context, station string) (*RadikoProg, error) {
	progs, err := r.stationTodayPrograms(ctx, station)
	if err != nil {
		return nil, err
	}

	for _, s := range progs.Stations.Station {
		if s.ID == station {
			for _, prog := range s.Progs.Prog {
				ft, err := prog.FtTime()
				if err != nil {
					return nil, err
				}

				to, err := prog.ToTime()
				if err != nil {
					return nil, err
				}

				now := time.Now()
				if ft.Unix() <= now.Unix() && now.Unix() < to.Unix() {
					prog.StationID = r.StationInfo.StationID
					prog.StationName = r.StationInfo.StationName
					return &prog, nil
				}
			}
		}
	}

	return nil, errors.New("not found program")
}

func (r *Radiko) record(ctx context.Context, output string, station string, buffer int64) (*RadikoResult, error) {

	err := r.radikoLogin(ctx)
	if err != nil || r.Login.Status != "200" {
		return nil, err
	}

	if !r.Premium && !r.StationInfo.LocalStation {
		return nil, fmt.Errorf("not radikoPremium / station : %s", station)
	}

	authtoken, area, err := r.auth(ctx)
	if err != nil {
		return nil, err
	}

	prog, err := r.stationNowProgram(ctx, station)
	if err != nil {
		return nil, err
	}

	os.Mkdir(r.TempDir, 0777)

	r.Log("GetImg : ", prog.Img)
	img, err := http.Get(prog.Img)
	if err == nil {
		defer img.Body.Close()

		file, err := os.Create(filepath.Dir(output) + "/podcast" + filepath.Ext(prog.Img))
		if err == nil {
			defer file.Close()

			io.Copy(file, img.Body)

		}
	}

	r.Log("StartRecording : ", prog.Title)

	duration, err := prog.Duration()
	if err != nil {
		return nil, err
	}
	duration += buffer

	recStart := time.Now().Format(radikoTimeLayout)
	err = r.hlsDownload(ctx, authtoken, station, area, fmt.Sprint(duration), output)
	recEnd := time.Now().Format(radikoTimeLayout)

	if r.Login.RadikoSession != "" {
		_ = r.radikoLogout(ctx)
	}

	if _, fileErr := os.Stat(output); fileErr != nil {
		return nil, err
	}

	ret := &RadikoResult{
		MedPath:  output,
		Station:  station,
		Prog:     prog,
		RecStart: recStart,
		RecEnd:   recEnd,
	}

	return ret, err
}

func (r *Radiko) GetStreamURL(stationID string) (string, error) {

	u := fmt.Sprintf(streamMultiURL, stationID)
	rsp, err := http.Get(u)
	if err != nil {
		return "", err
	}
	defer rsp.Body.Close()

	b, err := io.ReadAll(rsp.Body)
	if err != nil {
		return "", err
	}

	urlData := StreamUrlData{}
	if err = xml.Unmarshal(b, &urlData); err != nil {
		return "", err
	}

	var streamURL string = ""
	for _, i := range urlData.URL {
		if (i.AreaFree == r.StationInfo.AreaFree) && (i.TimeFree == false) {
			streamURL = fmt.Sprintf(playlistURL, i.PlaylistCreateURL, stationID)
			r.Log("streamURL : ", streamURL)
			break
		}

	}

	return streamURL, err

}

func (r *Radiko) hlsDownload(ctx context.Context, authtoken string, station string, area string, sec string, output string) error {

	streamURL, err := r.GetStreamURL(station)
	hlsRecCmd := hlsFfmpegCmd(r.Converter, streamURL, authtoken, area, sec, output)
	if err != nil {
		return err
	}

	r.Log("hlsFfmpegCmd : ", strings.Join(hlsRecCmd.Args, " "))

	var errbuff bytes.Buffer
	hlsRecCmd.Stderr = &errbuff

	errChan := make(chan error)
	go func() {
		if err := hlsRecCmd.Run(); err != nil {
			r.Log("CmdRun err : " + errbuff.String())
			errChan <- err
			return
		}
		errChan <- nil

	}()

	select {
	case <-ctx.Done():
		err := <-errChan
		if err == nil {
			err = ctx.Err()
		}
		return err
	case err := <-errChan:
		return err
	}

}

// return authtoken, area, err
func (r *Radiko) auth(ctx context.Context) (string, string, error) {

	req, err := http.NewRequest("GET", auth1URL, nil)
	if err != nil {
		return "", "", err
	}

	// req.Header.Set("pragma", "no-cache")
	// req.Header.Set("User-Agent", "radiko/4.0.1")
	// req.Header.Set("Accept", "*/*")
	req.Header.Set("X-Radiko-App", "pc_html5")
	req.Header.Set("X-Radiko-App-Version", "0.0.1")
	req.Header.Set("X-Radiko-Device", "pc")
	req.Header.Set("X-Radiko-User", "dummy_user")

	var authtoken string
	var partialkey string

	err = r.httpDo(ctx, req, func(resp *http.Response, err error) error {
		if err != nil {
			return err
		}

		defer resp.Body.Close()

		authtoken = resp.Header.Get("X-Radiko-Authtoken")
		keylength := resp.Header.Get("X-Radiko-Keylength")
		keyoffset := resp.Header.Get("X-Radiko-Keyoffset")

		if authtoken == "" {
			return errors.New("auth token is empty")
		}

		if keylength == "" {
			return errors.New("keylength is empty")
		}

		if keyoffset == "" {
			return errors.New("keyoffset is empty")
		}

		keylengthI, err := strconv.ParseInt(keylength, 10, 64)
		if err != nil {
			return err
		}

		keyoffsetI, err := strconv.ParseInt(keyoffset, 10, 64)
		if err != nil {
			return err
		}

		partialkeyByt := authKey[keyoffsetI : keyoffsetI+keylengthI]
		partialkey = base64.StdEncoding.EncodeToString([]byte(partialkeyByt))

		return nil
	})

	if err != nil {
		return "", "", err
	}

	var auth2UrlParm string
	if r.Login.AreaFree == "1" {
		auth2UrlParm = fmt.Sprintf(auth2PRM, r.Login.RadikoSession)
	}

	auth2Uri := fmt.Sprintf(auth2URL, auth2UrlParm)
	req, err = http.NewRequest("GET", auth2Uri, nil)
	if err != nil {
		return "", "", err
	}

	// req.Header.Set("pragma", "no-cache")
	// req.Header.Set("User-Agent", "radiko/4.0.1")
	// req.Header.Set("Accept", "*/*")
	// req.Header.Set("X-Radiko-App", "pc_html5")
	// req.Header.Set("X-Radiko-App-Version", "0.0.1")
	req.Header.Set("X-Radiko-Device", "pc")
	req.Header.Set("X-Radiko-User", "dummy_user")
	req.Header.Set("X-Radiko-Authtoken", authtoken)
	req.Header.Set("X-Radiko-Partialkey", partialkey)

	var area string
	err = r.httpDo(ctx, req, func(resp *http.Response, err error) error {
		if err != nil {
			return err
		}

		defer resp.Body.Close()

		byt, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		matches := regexp.MustCompile("(.*),(.*),(.*)").FindAllStringSubmatch(string(byt), -1)
		if len(matches) == 1 && len(matches[0]) != 4 {
			return errors.New("failed to auth")
		}

		area = matches[0][1]

		return nil
	})

	if err != nil {
		return "", "", err
	}

	return authtoken, area, nil
}

func (r *Radiko) httpDo(ctx context.Context, req *http.Request, f func(*http.Response, error) error) error {
	r.Log(req.Method + " " + req.URL.String())

	errChan := make(chan error)

	go func() { errChan <- f(http.DefaultClient.Do(req)) }()

	select {
	case <-ctx.Done():
		http.DefaultTransport.(*http.Transport).CancelRequest(req)
		err := <-errChan
		if err == nil {
			err = ctx.Err()
		}
		return err
	case err := <-errChan:
		return err
	}
}

func (r *Radiko) Log(v ...interface{}) {
	log.Println("[radiko]", fmt.Sprint(v...))
}

func (r *Radiko) radikoLogin(ctx context.Context) error {

	r.Login.RadikoSession = ""
	r.Premium = false

	if *radikoMail == "" || *radikoPass == "" {
		// Not Premium User
		return nil
	}

	v := url.Values{}
	v.Set("mail", *radikoMail)
	v.Set("pass", *radikoPass)

	req, err := http.NewRequest("POST", loginURL, strings.NewReader(v.Encode()))
	if err != nil {
		r.Login.Status = "401"
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	err = r.httpDo(ctx, req, func(resp *http.Response, err error) error {
		if err != nil {
			r.Login.Status = "401"
			return err
		}

		defer resp.Body.Close()
		if code := resp.StatusCode; code != 200 {
			r.Login.Status = strconv.Itoa(code)
			return fmt.Errorf("Login Error : not status code:200, got:%d", code)
		}

		body, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(body, &(r.Login)); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	if r.Login.RadikoSession != "" {
		r.Premium = true
	}

	return nil
}

func (r Radiko) radikoLogout(ctx context.Context) error {
	v := url.Values{}
	v.Set("radiko_session", r.Login.RadikoSession)

	r.Login.RadikoSession = ""
	r.Premium = false

	req, err := http.NewRequest("POST", logoutURL, strings.NewReader(v.Encode()))
	if err != nil {
		return err
	}

	err = r.httpDo(ctx, req, func(resp *http.Response, err error) error {
		if err != nil {
			return err
		}

		defer resp.Body.Close()
		if code := resp.StatusCode; code != 200 {
			r.Login.Status = strconv.Itoa(code)
			return fmt.Errorf("Logout Error : not status code:200, got:%d", code)
		}

		return nil
	})

	if err != nil {
		return err
	}

	return nil
}
