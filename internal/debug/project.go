package debug

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ROM と並べて置く開発中のプロジェクトのファイル（.dbg と Game State
// Definition）の読み込み（設計書 14 編 §14.11.2、§14.12.1）。

// GameStateDirName はデータディレクトリの下の Game State Definition の置き場所。
const GameStateDirName = "gamestate"

// ProjectPaths は ROM に対応するプロジェクトのファイルの場所。
type ProjectPaths struct {
	// Dbg は ROM と同じディレクトリの、拡張子を .dbg にしたファイル。
	Dbg string
	// GameStateLocal は ROM と同じディレクトリの <ROM 名>.gamestate.json。
	GameStateLocal string
	// GameStateData はデータディレクトリの gamestate/<rom-key>.json。
	GameStateData string
}

// ProjectPathsFor は ROM のパスとデータディレクトリと ROM の識別子から
// 場所を求める。romPath が空のときは ROM のディレクトリのファイルを持たない。
func ProjectPathsFor(romPath, dataDir, romKey string) ProjectPaths {
	var p ProjectPaths
	if romPath != "" {
		base := strings.TrimSuffix(romPath, filepath.Ext(romPath))
		p.Dbg = base + ".dbg"
		p.GameStateLocal = base + ".gamestate.json"
	}
	if dataDir != "" && romKey != "" {
		p.GameStateData = filepath.Join(dataDir, GameStateDirName, romKey+".json")
	}
	return p
}

// LoadProject は .dbg と Game State Definition を読み込んで syms に入れる。
// 知らせることを返す。
//
// .dbg は ROM より新しいときだけ読み込む。ビルドの途中で失敗し、古い
// デバッグ情報が新しい ROM の誤った位置を示すことを防ぐためである。
// Game State Definition は ROM のディレクトリのものを、データディレクトリの
// ものより優先する。開発中の ROM はビルドのたびにハッシュが変わるためである。
func LoadProject(syms *Symbols, romPath string, p ProjectPaths) []string {
	var notes []string
	syms.SetDbg(nil, "")
	if p.Dbg != "" {
		dbgInfo, dbgErr := os.Stat(p.Dbg)
		romInfo, romErr := os.Stat(romPath)
		switch {
		case dbgErr != nil:
			// .dbg が無い。知らせない。
		case romErr == nil && dbgInfo.ModTime().Before(romInfo.ModTime()):
			notes = append(notes, fmt.Sprintf("%s は ROM より古いため読み込んでいない（ビルドし直すか symbol.load で読み込む）", filepath.Base(p.Dbg)))
		default:
			info, err := LoadDbgFile(p.Dbg)
			if err != nil {
				notes = append(notes, fmt.Sprintf("%s を読めない: %v", filepath.Base(p.Dbg), err))
			} else {
				syms.SetDbg(info, p.Dbg)
			}
		}
	}
	def := NewGameStateDef()
	for _, path := range []string{p.GameStateLocal, p.GameStateData} {
		if path == "" {
			continue
		}
		d, err := LoadGameStateFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s を読めない: %v", filepath.Base(path), err))
			continue
		}
		def = d
		break
	}
	syms.SetGameState(def)
	syms.mu.Lock()
	syms.projectLoaded = true
	syms.mu.Unlock()
	return notes
}
