package main

// api for radiko, ffmpeg command parameter
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
	"strconv"
	"strings"
	"syscall"
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

var japanLocation = time.FixedZone("Asia/Tokyo", 9*60*60)

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
	AreaFree     bool
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
	XMLName  xml.Name `xml:"prog"`
	ID       string   `xml:"id,attr"`
	MasterID string   `xml:"master_id,attr"`
	Ft       string   `xml:"ft,attr"`
	To       string   `xml:"to,attr"`
	Ftl      string   `xml:"ftl,attr"`
	Tol      string   `xml:"tol,attr"`
	Dur      string   `xml:"dur,attr"`
	Title    string   `xml:"title"`
	URL      string   `xml:"url"`
	Desc     string   `xml:"desc"`
	Info     string   `xml:"info"`
	Pfm      string   `xml:"pfm"`
	Img      string   `xml:"img"`
	ExtInfo  ExtInfo  `xml:"ExtInfo"`
}

type ExtInfo struct {
	StationID   string `xml:"StationID,attr"`
	StationName string `xml:"StationName,attr"`
	RecStart    string `xml:"RecStart,attr"`
	RecEnd      string `xml:"RedEnd,attr"`
}

func (r *RadikoProg) FtTime() (time.Time, error) {
	return time.ParseInLocation(radikoTimeLayout, r.Ft, japanLocation)
}

func (r *RadikoProg) ToTime() (time.Time, error) {
	return time.ParseInLocation(radikoTimeLayout, r.To, japanLocation)
}

func (r *RadikoProg) Duration() (int64, error) {
	to, err := r.ToTime()
	if err != nil {
		return 0, err
	}
	return to.Unix() - time.Now().Unix(), nil
}

type RadikoResult struct {
	MedPath string
	Station string
	Prog    *RadikoProg
}

func (r *RadikoResult) Save(dir string) error {
	programDir := filepath.Join(dir, fmt.Sprintf("%s_%s", r.Prog.ExtInfo.RecStart, r.Station))
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

	if filepath.Ext(r.Prog.Img) != "" {
		imgSource := filepath.Join(filepath.Dir(r.MedPath), imgName)
		if _, err := os.Stat(imgSource); err == nil {
			if err := RenameOrCopy(imgSource, imgPath); err != nil {
				r.Log("Could not save program image: ", err)
			}
		}
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
	ProgramSpec string
	Buffer      int64
	Converter   string
	TempDir     string
	Premium     bool
	RadikoMail  string
	RadikoPass  string
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
	results := []*RadikoResult{}
	for attempt := 0; ; attempt++ {
		output := filepath.Join(r.TempDir, fmt.Sprintf("radiko_%d.m4a", attempt))
		r.Log("start record: ", r.Station+" / "+r.ProgramSpec)
		result, err := r.record(ctx, output, r.Station, r.Buffer)
		if result != nil {
			results = append(results, result)
		}
		if ctx.Err() != nil {
			return results
		}
		if err == nil {
			return results
		}

		r.Log("got err: ", err)
		if strings.HasPrefix(err.Error(), "not radikoPremium") ||
			strings.HasPrefix(err.Error(), "Login error") ||
			attempt >= 2 {
			return results
		}

		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return results
		case <-timer.C:
			r.Log("retry after ", 2*time.Second)
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
		MedPath: output,
		Station: results[0].Station,
		Prog:    results[0].Prog,
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

	lreq, err := http.NewRequestWithContext(ctx, "GET", lu.String(), nil)
	if err != nil {
		return err
	}

	var localStations LocalStations
	err = r.httpDo(lreq, func(resp *http.Response, err error) error {
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

	freq, err := http.NewRequestWithContext(ctx, "GET", fu.String(), nil)
	if err != nil {
		return err
	}

	var fullStations FullStations
	err = r.httpDo(freq, func(resp *http.Response, err error) error {
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
					AreaFree:     true,
					LocalStation: false,
				}
			} else {
				// local area station
				stationInfoMap[station.ID] = StationInfo{
					StationID:    station.ID,
					StationName:  station.Name,
					StationArea:  station.AreaID,
					AreaFree:     false,
					LocalStation: true,
				}
			}
		}
	}

	return nil
}

func (r *Radiko) stationProgramsForDate(ctx context.Context, station string, day time.Time) (*RadikoPrograms, error) {
	const layoutDate = "20060102"

	u, err := url.Parse(fmt.Sprintf(todayPrgramURL, day.In(japanLocation).Format(layoutDate), station))
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	var progs RadikoPrograms
	err = r.httpDo(req, func(resp *http.Response, err error) error {
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

func (r *Radiko) stationTodayPrograms(ctx context.Context, station string) (*RadikoPrograms, error) {
	now := time.Now().In(japanLocation)
	if now.Hour() < 5 {
		now = now.AddDate(0, 0, -1)
	}

	return r.stationProgramsForDate(ctx, station, now)
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
				if !now.Before(ft) && now.Before(to) {
					prog.ExtInfo.StationID = r.StationInfo.StationID
					prog.ExtInfo.StationName = r.StationInfo.StationName
					return &prog, nil
				}
			}
		}
	}

	return nil, errors.New("not found program")
}

func (r *Radiko) record(ctx context.Context, output string, station string, buffer int64) (*RadikoResult, error) {

	if err := r.radikoLogin(ctx); err != nil {
		return nil, err
	}
	if r.Login.RadikoSession != "" {
		defer func() {
			if err := r.radikoLogout(ctx); err != nil {
				r.Log("Logout error: ", err)
			}
		}()
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

	r.Log("GetImg: ", prog.Img)
	if prog.Img != "" {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, prog.Img, nil)
		if reqErr == nil {
			img, imgErr := http.DefaultClient.Do(req)
			if imgErr == nil {
				if img.StatusCode == http.StatusOK {
					file, fileErr := os.Create(filepath.Join(filepath.Dir(output), "podcast"+filepath.Ext(prog.Img)))
					if fileErr == nil {
						if _, copyErr := io.Copy(file, img.Body); copyErr != nil {
							r.Log("Could not save program image: ", copyErr)
						}
						if closeErr := file.Close(); closeErr != nil {
							r.Log("Could not close program image: ", closeErr)
						}
					} else {
						r.Log("Could not create program image: ", fileErr)
					}
				}
				if closeErr := img.Body.Close(); closeErr != nil {
					r.Log("Could not close program image response: ", closeErr)
				}
			} else {
				r.Log("Could not download program image: ", imgErr)
			}
		} else {
			r.Log("Could not create program image request: ", reqErr)
		}
	}

	r.Log("StartRecording: ", prog.Title)

	duration, err := prog.Duration()
	if err != nil {
		return nil, err
	}
	duration += buffer

	prog.ExtInfo.RecStart = time.Now().Format(radikoTimeLayout)
	err = r.hlsDownload(ctx, authtoken, station, area, fmt.Sprint(duration), output)
	prog.ExtInfo.RecEnd = time.Now().Format(radikoTimeLayout)

	r.Log("EndRecording: ", prog.Title)

	if _, fileErr := os.Stat(output); fileErr != nil {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("recording output is unavailable: %w", fileErr)
	}

	ret := &RadikoResult{
		MedPath: output,
		Station: station,
		Prog:    prog,
	}

	return ret, err
}

func (r *Radiko) GetStreamURL(ctx context.Context, stationID string) (string, error) {

	u := fmt.Sprintf(streamMultiURL, stationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stream list returned status %d", rsp.StatusCode)
	}

	b, err := io.ReadAll(rsp.Body)
	if err != nil {
		return "", err
	}

	urlData := StreamUrlData{}
	if err = xml.Unmarshal(b, &urlData); err != nil {
		return "", err
	}

	for _, item := range urlData.URL {
		if item.AreaFree == r.StationInfo.AreaFree && !item.TimeFree {
			streamURL := fmt.Sprintf(playlistURL, item.PlaylistCreateURL, stationID)
			r.Log("streamURL: ", streamURL)
			return streamURL, nil
		}
	}
	return "", fmt.Errorf("no matching stream URL for station %s", stationID)
}

func (r *Radiko) hlsDownload(ctx context.Context, authtoken string, station string, area string, sec string, output string) error {

	streamURL, err := r.GetStreamURL(ctx, station)
	if err != nil {
		return err
	}
	hlsRecCmd := hlsFfmpegCmd(r.Converter, streamURL, authtoken, area, sec, output)
	r.Log("hlsFfmpegCmd: ", strings.Join(hlsRecCmd.Args, " "))

	var errbuff bytes.Buffer
	hlsRecCmd.Stderr = &errbuff
	hlsRecCmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := hlsRecCmd.Start(); err != nil {
		return err
	}

	errChan := make(chan error, 1)
	go func() {
		errChan <- hlsRecCmd.Wait()
	}()

	select {
	case <-ctx.Done():
		if hlsRecCmd.Process != nil {
			processGroup := -hlsRecCmd.Process.Pid
			r.Log("Gracefully stopping ffmpeg process group (PID: ", hlsRecCmd.Process.Pid, ")")
			if err := syscall.Kill(processGroup, syscall.SIGINT); err != nil && !errors.Is(err, syscall.ESRCH) {
				r.Log("Could not signal ffmpeg process group: ", err)
				if signalErr := hlsRecCmd.Process.Signal(os.Interrupt); signalErr != nil {
					r.Log("Could not interrupt ffmpeg process: ", signalErr)
				}
			}
			select {
			case err := <-errChan:
				if err != nil {
					r.Log("Process terminated with error: ", err)
				}
				return ctx.Err()
			case <-time.After(10 * time.Second):
				r.Log("ffmpeg did not stop after SIGINT; sending SIGTERM")
				if err := syscall.Kill(processGroup, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
					r.Log("Could not terminate ffmpeg process group: ", err)
				}
				select {
				case err := <-errChan:
					if err != nil {
						r.Log("Process terminated with error: ", err)
					}
					return ctx.Err()
				case <-time.After(5 * time.Second):
					r.Log("Force killing ffmpeg process group")
					if err := syscall.Kill(processGroup, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
						r.Log("Could not kill ffmpeg process group: ", err)
					}
					<-errChan
					return ctx.Err()
				}
			}
		}
		return ctx.Err()
	case err := <-errChan:
		return err
	}
}

// return authtoken, area, err
func (r *Radiko) auth(ctx context.Context) (string, string, error) {

	req, err := http.NewRequestWithContext(ctx, "GET", auth1URL, nil)
	if err != nil {
		return "", "", err
	}

	req.Header.Set("X-Radiko-App", "pc_html5")
	req.Header.Set("X-Radiko-App-Version", "0.0.1")
	req.Header.Set("X-Radiko-Device", "pc")
	req.Header.Set("X-Radiko-User", "dummy_user")

	var authtoken string
	var partialkey string

	err = r.httpDo(req, func(resp *http.Response, err error) error {
		if err != nil {
			return err
		}

		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("auth1 returned status %d", resp.StatusCode)
		}

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

		if keyoffsetI < 0 || keylengthI <= 0 ||
			keyoffsetI > int64(len(authKey)) ||
			keylengthI > int64(len(authKey))-keyoffsetI {
			return errors.New("invalid auth key range")
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
	req, err = http.NewRequestWithContext(ctx, "GET", auth2Uri, nil)
	if err != nil {
		return "", "", err
	}

	req.Header.Set("X-Radiko-Device", "pc")
	req.Header.Set("X-Radiko-User", "dummy_user")
	req.Header.Set("X-Radiko-Authtoken", authtoken)
	req.Header.Set("X-Radiko-Partialkey", partialkey)

	var area string
	err = r.httpDo(req, func(resp *http.Response, err error) error {
		if err != nil {
			return err
		}

		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("auth2 returned status %d", resp.StatusCode)
		}

		byt, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		parts := strings.Split(strings.TrimSpace(string(byt)), ",")
		if len(parts) != 3 || parts[0] == "" {
			return errors.New("failed to auth")
		}

		area = parts[0]

		return nil
	})

	if err != nil {
		return "", "", err
	}

	return authtoken, area, nil
}

func (r *Radiko) httpDo(req *http.Request, f func(*http.Response, error) error) error {
	r.Log(req.Method + " " + req.URL.String())

	return f(http.DefaultClient.Do(req))
}

func (r *Radiko) Log(v ...interface{}) {
	log.Println("[radiko]", fmt.Sprint(v...))
}

func (r *Radiko) radikoLogin(ctx context.Context) error {

	r.Login.RadikoSession = ""
	r.Premium = false

	if r.RadikoMail == "" || r.RadikoPass == "" {
		// Not Premium User
		return nil
	}

	v := url.Values{}
	v.Set("mail", r.RadikoMail)
	v.Set("pass", r.RadikoPass)

	req, err := http.NewRequestWithContext(ctx, "POST", loginURL, strings.NewReader(v.Encode()))
	if err != nil {
		r.Login.Status = "401"
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	err = r.httpDo(req, func(resp *http.Response, err error) error {
		if err != nil {
			r.Login.Status = "401"
			return err
		}

		defer resp.Body.Close()
		if code := resp.StatusCode; code != 200 {
			r.Login.Status = strconv.Itoa(code)
			return fmt.Errorf("Login error: not status code:200, got:%d", code)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
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

func (r *Radiko) radikoLogout(ctx context.Context) error {
	v := url.Values{}
	v.Set("radiko_session", r.Login.RadikoSession)

	r.Login.RadikoSession = ""
	r.Premium = false

	req, err := http.NewRequestWithContext(ctx, "POST", logoutURL, strings.NewReader(v.Encode()))
	if err != nil {
		return err
	}

	err = r.httpDo(req, func(resp *http.Response, err error) error {
		if err != nil {
			return err
		}

		defer resp.Body.Close()
		if code := resp.StatusCode; code != 200 {
			r.Login.Status = strconv.Itoa(code)
			return fmt.Errorf("Logout error: not status code:200, got:%d", code)
		}

		return nil
	})

	if err != nil {
		return err
	}

	return nil
}
