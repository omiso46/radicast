# radicast(改)
いまさらだけど、ちゃんとforkしないと…<br>
<br>
radicastをforkしてradcastをマージ<br>
さらに、エリアフリーに対応<br>
※configファイルにログインIDとパスワードを保持。<br>
　パスワードは無駄に暗号化します。気休め気休め<br>
<br>

## 必要パッケージ
* ffmpeg

## インストール
```
$ go install github.com/omiso46/radicast@v1.0.0
```
## 使い方
### 設定ファイル（エリアフリー）
```
$ radicast -setup -radikoMail yourmail@exsample.com -radikoPass yourpass > config.json
```
### 設定ファイル（フリープラン）
```
$ radicast -setup > config.json
```

### 編集
```
$ vim config.json

{
  "-RADIKO_MAIL-": [
    "yourmail@exsample.com"
  ],
  "-RADIKO_PASS-": [
    "b276f31c7d3c1862c991617334abe708b16c1dcc85c1f1cf5ceae1c15bb75572"
  ],
  "FMT": [
    "00 17 * * *"
  ]
}
```
cron specification is [here](https://godoc.org/github.com/robfig/cron#hdr-CRON_Expression_Format)

### 設定ファイルのリロード
```
$ kill -HUP nnn
```

## LICENSE
* MIT

## お約束
録音ファイルは個人使用の範囲内で。絶対！<br>
すべて自己責任で！！！<br>

---
---
# Original README

# radicast

* record radiko
* serve rss for podcast

## REQUIRE

* rtmpdump
* swftools
* ffmpeg or avconv
* or docker (see docker section)

## INSTALL

```
$ go get github.com/soh335/radicast
```

## USAGE

### SETUP CONFIG.JSON

```
$ radicast --setup > config.json
```

### EDIT CONFIG.JSON

```
$ vim config.json
$ cat config.json

{
  "FMT": [
    "0 0 17 * * *"
  ]
}
```

cron specification is [here](https://godoc.org/github.com/robfig/cron#hdr-CRON_Expression_Format)

### LAUNCH

```
$ radicast
$ curl 127.0.0.1:3355/rss # podcast rss
```

### RELOAD CONFIG.JSON

* reload config when receive HUP signal

## DOCKER

```
$ mkdir workspace
$ cd workspace
$ docker pull soh335/radicast
$ docker run --rm soh335/radicast:latest --setup > config.json
$ docker run --rm -p 3355:3355 -v `pwd`:/workspace soh335/radicast:latest --config /workspace/config.json --output /workspace/output
```

* [docker-hub](https://registry.hub.docker.com/u/soh335/radicast/)

## SEE ALSO

* [ripdiko](https://github.com/miyagawa/ripdiko)

## LICENSE

* MIT
