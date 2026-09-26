# radicast(改)
いまさらだけど、ちゃんとforkしないと…<br>

radicastをforkしてradcastをマージ<br>
エリアフリーに対応<br>
　configファイルにログインIDとパスワードを保持<br>
　パスワードは無駄に暗号化。気休め気休め<br>
番組表検索を実装<br>
　"title:"を前置して番組名を指定<br>
　当日番組表(05:00〜29:00)から部分一致検索でヒットする番組をすべて録音<br>
　番組延長等には追従できないので諦めて…<br>

## 必要パッケージ
* ffmpeg

## インストール
```
$ go install github.com/omiso46/radicast@v1.2.0
```
※@latestだと@v2+incomp...を取得してしまうので直接指定でよろしく

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
  "XYZ": [
    "00 17 * * *",
    "title:番組名"
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


## 履歴
v1.2.0 番組表検索機能を実装<br>
v1.1.0 BugFix<br>
v1.0.5 Copilotに最適化を依頼<br>
v1.0.4 録音中のプロセス停止がうまくいかない件に対応(thx.Copilot)<br>
v1.0.3 Podcast用出力情報を一部変更<br>
v1.0.2 非推奨メソッド(CancelRequest)を除外したり、その他諸々改修<br>
v1.0.1 Podcast用出力情報を一部変更<br>
v1.0.0 radicastをforkし、radcastをマージしてエリアフリーにも対応<br>
幻のv2 エリアフリー対応版をv2にしたかったけど上手く設定できなかった<br>
