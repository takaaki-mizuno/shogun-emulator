package movie

import (
	"fmt"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// Frame は再生する 1 フレーム分の指示。
type Frame struct {
	// Buttons はポート 1 と 2 の押下状態。
	Buttons [2]uint8
	// Reset と HardReset はこのフレームの開始時に行う操作。
	Reset     bool
	HardReset bool
	// Checksum はこのフレームで照合するハッシュ。持たないとき nil。
	Checksum *[8]uint8
	// Interventions はこのフレームの介入。サイクル数の昇順。
	Interventions []Record
}

// Player はムービーを再生する。
type Player struct {
	movie *Movie
	// pos は次に読むレコードの位置。
	pos int
	// frame は次に再生するフレーム番号。
	frame uint64
}

// NewPlayer は再生を始める。
func NewPlayer(m *Movie) *Player { return &Player{movie: m} }

// Header はムービーのヘッダを返す。
func (p *Player) Header() *Header { return &p.movie.Header }

// Frame は次に再生するフレーム番号を返す。
func (p *Player) Frame() uint64 { return p.frame }

// TotalFrames はムービーの総フレーム数を返す。
func (p *Player) TotalFrames() uint64 { return p.movie.Header.TotalFrames }

// Done は再生が終わったかを返す。
func (p *Player) Done() bool { return p.pos >= len(p.movie.Records) }

// BeginFrame は次のフレームの指示を返す。
//
// レコードを順に読み、イベントを集めてから入力レコード 1 つを消費する。
// 入力レコードの直後にチェックサムがあれば、それも返す。
func (p *Player) BeginFrame() (Frame, bool) {
	var f Frame
	for p.pos < len(p.movie.Records) {
		rec := p.movie.Records[p.pos]
		switch rec.Kind {
		case KindReset:
			f.Reset = true
			p.pos++
		case KindHardReset:
			f.HardReset = true
			p.pos++
		case KindChecksum:
			// 入力レコードの前に現れるチェックサムは、前のフレームの
			// ものを読み残した場合だけである。読み飛ばす。
			p.pos++
		case KindInput:
			f.Buttons = rec.Buttons
			p.pos++
			if p.pos < len(p.movie.Records) && p.movie.Records[p.pos].Kind == KindChecksum {
				h := p.movie.Records[p.pos].Hash
				f.Checksum = &h
				p.pos++
			}
			// 入力の後に置いた介入は、このフレームのもの。
			for p.pos < len(p.movie.Records) && p.movie.Records[p.pos].Kind.IsIntervention() {
				f.Interventions = append(f.Interventions, p.movie.Records[p.pos])
				p.pos++
			}
			p.frame++
			return f, true
		default:
			// 入力より前に現れる介入は無い。読み飛ばす。
			p.pos++
		}
	}
	return f, false
}

// VerifyHeader は再生できるムービーかを照合する。
//
// ROM・マッパー・リージョンの不一致はエラーとする。別の ROM のムービーを
// 再生しても意味のある結果にならない。エミュレータのバージョンの違いは
// 警告として返し、再生は続ける（設計書 08 編 §8.7.4）。
func (p *Player) VerifyHeader(want *Header) (warning string, err error) {
	h := &p.movie.Header
	if h.ROMHash != want.ROMHash {
		return "", fmt.Errorf("movie: ROM ハッシュが一致しない（期待 %x、実際 %x）",
			want.ROMHash[:4], h.ROMHash[:4])
	}
	if h.Mapper != want.Mapper || h.Submapper != want.Submapper {
		return "", fmt.Errorf("movie: マッパーが違う（期待 %d.%d、実際 %d.%d）",
			want.Mapper, want.Submapper, h.Mapper, h.Submapper)
	}
	if h.Region != want.Region {
		return "", fmt.Errorf("movie: リージョンが違う（期待 %q、実際 %q）", want.Region, h.Region)
	}
	if h.Version != want.Version || h.Commit != want.Commit {
		return fmt.Sprintf("movie: 記録時のエミュレータが違う（記録 %s/%s、現在 %s/%s）",
			h.Version, h.Commit, want.Version, want.Commit), nil
	}
	return "", nil
}

// Init はムービーが指定する電源投入時の初期状態を返す。
func (p *Player) Init() state.Init { return p.movie.Header.Init }

// DesyncError は再生中にチェックサムが一致しなかったことを表す。
type DesyncError struct {
	// Frame は一致しなかったフレーム番号。
	Frame uint64
	// Want と Got は記録されたハッシュと実際のハッシュ。
	Want [8]uint8
	Got  [8]uint8
}

// Error は人が読める説明を返す。
func (e *DesyncError) Error() string {
	return fmt.Sprintf("movie: フレーム %d で状態が一致しない（記録 %x、実際 %x）",
		e.Frame, e.Want, e.Got)
}
