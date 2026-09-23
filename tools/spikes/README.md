# スパイク（技術検証コード）

`docs/research/09a_fyne_performance_spike.md` の実測に使ったコード。
**エミュレータ本体とは別モジュール**にしてあるので、本体の依存関係には影響しない。

環境を変えて再測定できるように残している（Windows / Linux での確認は未実施 → `09a` §7）。

## 実行方法

```bash
cd tools/spikes

# 1. 描画性能: 実際にペイントされた回数を数える
#    引数: <拡大率> <mode: max|vsync> <計測秒数>
go run ./fyne-render 3 max 6      # 3x (768x720)、Refresh を全力で呼ぶ → Fyne の上限を測る
go run ./fyne-render 6 vsync 6    # 6x (1536x1440)、60.088Hz の Ticker で呼ぶ

# 2. オーディオ: 20/35/50/100 ms バッファでアンダーランとレイテンシを測る
go run ./oto-latency

# 3. UI 機能: 別ウィンドウ、AppTabs、ネイティブメニュー、TextGrid のセル単位の色
go run ./fyne-ui

# 4. オーディオ駆動方式の耐久試験（各方式 60 秒 × 複数 = 数分かかる）
#    ティッカー駆動 vs バックプレッシャ方式で音切れ回数を比較する
go run ./audio-drift

# 4b. time.Ticker / time.Sleep がクロック源として使えるかの精度測定
go test -run Accuracy -v ./audio-drift
```

いずれも数秒で自動終了して結果を stdout に出す。

## 注意

- `fyne-render` と `fyne-ui` はウィンドウを開くので、ヘッドレス環境では動かない
- `oto-latency` と `audio-drift` は実際に 440 Hz の音を鳴らす（音量に注意）
- `audio-drift` は**意図的に音切れを起こす**ので、プチプチ鳴るのは正常
- 初回は Fyne のビルドに 30 秒ほどかかる
