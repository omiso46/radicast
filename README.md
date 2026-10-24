# radicast(改)
いまさらだけど、ちゃんとforkしないと…<br>

radicastをforkしてradcastをマージ<br>
エリアフリーに対応<br>
　configファイルにログインIDとパスワードを保持<br>
　パスワードは無駄に暗号化。気休め気休め<br>
番組表検索を実装<br>
　"title:"を前置して番組名を指定<br>
　04:58に当日番組表(05:00〜29:00)から部分一致検索でヒットする番組をすべて録音<br>
　番組変更には追従できないので諦めて…<br>

## 必要パッケージ
* ffmpeg

## インストール
```bash
$ go install github.com/omiso46/radicast@v1.2.1
```
※@latestだと@v2+incomp...を取得してしまうので直接指定でよろしく

## 使い方
```
Usage of radicast:
  -host string
      host (default "0.0.0.0")
      // Podcast用ホストアドレス
  -port string
      port (default "8000")
      // Podcast用ポート番号
  -title string
      title (default "radicast")
      // Podcast用タイトル
  -config string
      path of config.json (default "config.json")
      // 設定ファイルパス
  -buffer int
      buffer for recording (default 60)
      // 録音バッファ（秒） 遅延に合わせて秒数を設定するのが良い
  -output string
      output (default "output")
      // 録音ファイル出力先パス
      // 直下に「radicast.png」配置でPodcast用アイコンに
  -setup
      initialize json configuration
      // 設定ファイル生成
  -radikoMail string
      *use only setup* radiko premium login MailAddress
      // ＊-setupと同時使用＊ ラジコプレミアム：メールアドレス
  -radikoPass string
      *use only setup* radiko premium login Password
      // ＊-setupと同時使用＊ ラジコプレミアム：パスワード
```

### 設定ファイル生成
* エリアフリー
```bash
$ radicast -setup -radikoMail yourmail@exsample.com -radikoPass yourpass > config.json
```
* フリープラン
```bash
$ radicast -setup > config.json
```

### 編集
```
$ vim config.json
```
```json
{
  "-RADIKO_MAIL-": [ ※ エリアフリーの場合にのみ設定（セットアップで生成）
    "yourmail@exsample.com"
  ],
  "-RADIKO_PASS-": [ ※ エリアフリーの場合にのみ設定（セットアップで生成）
    "b276f31c7d3c1862c991617334abe708b16c1dcc85c1f1cf5ceae1c15bb75572"
  ],
  "XYZ": [ ※ 局ID
    "00 17 * * *", ※ 時間指定録音の場合（cron書式：分 時 日 月 曜）
    "title:番組名" ※ 番組名検索の場合
  ]
}
```

### 設定ファイルのリロード
```bash
$ kill -HUP nnn
```

## LICENSE
* MIT

## お約束
録音ファイルは個人使用の範囲内で。絶対！<br>
すべて自己責任で！！！<br>

## 履歴
v1.2.1 番組表検索の実施時刻を変更（04:58実施）と録音強制終了時にその録音を成立させるように修正<br>
v1.2.0 番組表検索機能を実装＆チョコっと改修<br>
v1.1.0 BugFix<br>
v1.0.5 Copilotに最適化を依頼<br>
v1.0.4 録音中のプロセス停止がうまくいかない件に対応<br>
v1.0.3 Podcast用出力情報を一部変更<br>
v1.0.2 非推奨メソッド(CancelRequest)を除外したり、その他諸々改修<br>
v1.0.1 Podcast用出力情報を一部変更<br>
v1.0.0 radicastをforkし、radcastをマージしてエリアフリーにも対応<br>
幻のv2 エリアフリー対応版をv2にしたかったけど上手く設定できなかった<br>
