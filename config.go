package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
)

type Config map[string][]string

func LoadConfig(path string) (Config, error) {
	f, err := os.Open(path)

	if err != nil {
		return nil, err
	}

	var c Config
	if err := json.NewDecoder(f).Decode(&c); err != nil {
		return nil, err
	}

	return c, nil
}

func SetupConfig(ctx context.Context) error {

	r := &Radiko{}
	err := r.FullStationInfoMap(ctx)
	if err != nil {
		return err
	}

	c := Config{}

	var radikoPremium bool
	if *radikoMail != "" && *radikoPass != "" {
		encPass, err := EncryptAES(*radikoPass)
		if err == nil {
			// RadikoPremium Settings
			c["-RADIKO_MAIL-"] = []string{*radikoMail}
			c["-RADIKO_PASS-"] = []string{encPass}
			radikoPremium = true
		}
	}

	for _, station := range stationInfoMap {
		if radikoPremium || !station.AreaFree {
			c[station.StationID] = []string{}
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
