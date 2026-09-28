package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
)

type AppConfig struct {
	RadikoMail string
	RadikoPass string
	Stations   map[string][]string
}

func LoadConfig(path string) (*AppConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	var c AppConfig
	if err := json.NewDecoder(f).Decode(&c); err != nil {
		return nil, err
	}

	return &c, nil
}

func (c *AppConfig) UnmarshalJSON(data []byte) error {
	var raw map[string][]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	c.Stations = make(map[string][]string)
	for key, value := range raw {
		switch key {
		case "-RADIKO_MAIL-":
			if len(value) > 0 {
				c.RadikoMail = value[0]
			}
		case "-RADIKO_PASS-":
			if len(value) > 0 {
				c.RadikoPass = value[0]
			}
		default:
			c.Stations[key] = value
		}
	}

	return nil
}

func (c AppConfig) MarshalJSON() ([]byte, error) {
	raw := make(map[string][]string, len(c.Stations)+2)
	if c.RadikoMail != "" {
		raw["-RADIKO_MAIL-"] = []string{c.RadikoMail}
	}
	if c.RadikoPass != "" {
		raw["-RADIKO_PASS-"] = []string{c.RadikoPass}
	}
	for key, value := range c.Stations {
		raw[key] = value
	}

	return json.Marshal(raw)
}

func SetupConfig(ctx context.Context, radikoMail string, radikoPass string) error {
	r := &Radiko{}
	err := r.FullStationInfoMap(ctx)
	if err != nil {
		return err
	}

	c := AppConfig{
		Stations: make(map[string][]string),
	}

	var radikoPremium bool
	if radikoMail != "" && radikoPass != "" {
		encPass, err := EncryptAES(radikoPass)
		if err != nil {
			return err
		}
		// RadikoPremium Settings
		c.RadikoMail = radikoMail
		c.RadikoPass = encPass
		radikoPremium = true
	}

	for _, station := range stationInfoMap {
		if radikoPremium || !station.AreaFree {
			c.Stations[station.StationID] = []string{}
		}
	}

	byt, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	if _, err := io.Copy(os.Stdout, bytes.NewReader(byt)); err != nil {
		return err
	}

	return nil
}
