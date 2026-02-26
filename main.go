package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
)

var (
	host       = flag.String("host", "0.0.0.0", "host")
	port       = flag.String("port", "8000", "port")
	buffer     = flag.Int64("buffer", 60, "buffer for recording")
	output     = flag.String("output", "output", "output")
	title      = flag.String("title", "radicast", "title")
	configPath = flag.String("config", "config.json", "path of config.json")
	setup      = flag.Bool("setup", false, "initialize json configuration")
	radikoMail = flag.String("radikoMail", "", "*use only setup* radiko premium login MailAddress")
	radikoPass = flag.String("radikoPass", "", "*use only setup* radiko premium login Password")
)

func main() {
	flag.Parse()

	if *setup {
		runSetup()
		return
	}

	if err := runRadicast(); err != nil {
		log.Fatal(err)
	}
}

func runRadicast() error {

	converter, err := lookConverterCommand()
	if err != nil {
		return err
	}

	r := NewRadicast(*configPath, *host, *port, *title, *output, *buffer, converter)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, os.Kill, syscall.SIGHUP)

	go func() {
		for {
			s := <-signalChan
			r.Log("got signal:", s)
			switch s {
			case syscall.SIGHUP:
				r.ReloadConfig()
			default:
				r.Stop()
			}
		}
	}()

	return r.Run()
}

func runSetup() {
	ctx, cancel := context.WithCancel(context.Background())

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, os.Kill)

	go func() {
		s := <-signalChan
		log.Println("got signal:", s)
		cancel()
	}()

	if *radikoMail != "" && *radikoPass != "" {
		// login check
		radiko := &Radiko{}
		err := radiko.radikoLogin(ctx)
		if radiko.Login.RadikoSession != "" || err == nil {
			// login check OK
			err = radiko.radikoLogout(ctx)
		} else {
			// login check NG
			log.Fatal("Login Error ! CHECK MAIL&PASS")
			return
		}
	}

	if err := SetupConfig(ctx); err != nil {
		log.Fatal(err)
	}

}

func EncryptAES(plainText string) (string, error) {
	// authKeyの上32桁をキー
	key := []byte(authKey[0:32])

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	cipherText := gcm.Seal(nonce, nonce, []byte(plainText), nil)
	return base64.StdEncoding.EncodeToString(cipherText), nil
}

func DecryptAES(cipherText string) (string, error) {
	// authKeyの上32桁をキー
	key := []byte(authKey[0:32])

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	data, err := base64.StdEncoding.DecodeString(cipherText)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("不正暗号")
	}

	nonce, cipherTextBytes := data[:nonceSize], data[nonceSize:]

	plainText, err := gcm.Open(nil, nonce, cipherTextBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plainText), nil
}
