package emu

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// exportChunk は書き出しで 1 回に進めるフレーム数。進み具合の更新と取り消しの
// 確認の単位になる。
const exportChunk = 60

// VideoExport は操作の記録から動画を書き出す指定（設計書 08 編 §8.8.4）。
type VideoExport struct {
	ROMPath, MoviePath, OutPath string
	Palette                     *video.Palette
	Scale                       int
	Overscan                    video.Overscan
	// Progress は進み具合を知らせる。nil のとき知らせない。呼び出し側の
	// ゴルーチンから呼ぶ。
	Progress func(done, total uint64)
}

// ExportVideo は表示中のエミュレータとは別の Emulator で操作の記録を再生し、
// 動画を書き出す。音声デバイスは開かず、速度の上限なしで進める。
// 取り消したとき・失敗したときは書きかけのファイルを消す。
func (e *Emulator) ExportVideo(ctx context.Context, x VideoExport) error {
	tmp, err := os.MkdirTemp("", "shogun-export-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	cfg := e.cfg
	cfg.Audio.Enabled = false
	cfg.NewPacer = func(*region.Region) Pacer { return NewNoPacer() }
	cfg.StartPaused = true
	cfg.Notify, cfg.OnBreak, cfg.LoadSymbols = nil, nil, nil
	// 利用者のセーブデータと名前の保存先を読み書きしない（Agent Interface の
	// headless の Instance と同じ扱い）。
	cfg.Paths.SaveDir = tmp
	cfg.SymbolsDir = tmp
	// オーバーレイ（改造パッチ）は ROM へ当てるために読み込む必要があるが、
	// 書き戻し先（closeVideo 前の子 Emulator の Stop で storeOverlay が
	// 無条件に走る）が利用者の実データを向いたままだと、記録のたびに
	// オーバーレイが新規作成・上書き・削除されてしまう（設計書 08 編
	// §8.8.4: 利用者の状態を変えない）。読み込みの再現性は保ちつつ書き込み
	// だけを隔離するため、実際の保存先の中身を一時ディレクトリへ複製し、
	// 子にはそちらを使わせる。
	patchesDir := filepath.Join(tmp, "patches")
	if err := os.MkdirAll(patchesDir, 0o755); err != nil {
		return err
	}
	if err := copyPatchesDir(e.cfg.Dirs.PatchesDir(e.cfg.PatchesDir), patchesDir); err != nil {
		return err
	}
	cfg.PatchesDir = patchesDir
	// 常時トレースの書き出し先も一時ディレクトリへ向ける。
	cfg.TraceDir = tmp
	child := New(cfg)
	child.Start()
	defer child.Stop()

	if err := child.LoadROM(x.ROMPath); err != nil {
		return err
	}
	if err := child.PlayMovieFilePaused(x.MoviePath); err != nil {
		return err
	}
	total := child.Status().Movie.Total
	if err := child.StartRecordingVideo(x.OutPath, x.Palette, x.Scale, x.Overscan); err != nil {
		return err
	}
	abort := func(err error) error {
		// closeVideo(true) は常に nil を返す実装だが、将来 Abort() が
		// エラーを返すようになっても静かに握りつぶさないよう合わせて返す。
		if aerr := child.apply(cmdStopVideo{abort: true, done: make(chan error, 1)}); aerr != nil {
			err = errors.Join(err, aerr)
		}
		return err
	}
	for done := uint64(0); done < total; {
		if err := ctx.Err(); err != nil {
			return abort(err)
		}
		n := min(uint64(exportChunk), total-done)
		child.StepFrames(int(n))
		child.WithMachine(func(*nes.NES) {})
		done += n
		if err := child.DesyncError(); err != nil {
			return abort(err)
		}
		if child.Status().Video.Error != "" {
			// 書き込みに失敗して録画が止まった。ファイルは消してある。
			// 残りを進めても無駄なので、ここで覚えたエラーを受け取って返す。
			return child.StopRecordingVideo()
		}
		if x.Progress != nil {
			x.Progress(done, total)
		}
	}
	if err := ctx.Err(); err != nil {
		return abort(err)
	}
	return child.StopRecordingVideo()
}

// copyPatchesDir は src の直下にある通常ファイルだけを dst へ複製する。
// オーバーレイの保存先を隔離するときに、既存のオーバーレイを子
// Emulator が読み込めるようにするために使う。src が無いときは何もしない
// （複製先を空のままにする）。
func copyPatchesDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, ent := range entries {
		if !ent.Type().IsRegular() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, ent.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, ent.Name()), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
