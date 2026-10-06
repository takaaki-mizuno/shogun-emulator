package agent

import (
	"encoding/json"
	"fmt"
)

// OwnerKind は Control の持ち主の種類（設計書 14 編 §14.4.1）。
type OwnerKind uint8

// Control の持ち主の種類。
const (
	// OwnerNone は誰も持っていないことを表す（headless の Instance で、
	// 持ち主の接続が返したとき）。
	OwnerNone OwnerKind = iota
	// OwnerHuman は GUI の人間が持っていることを表す。
	OwnerHuman
	// OwnerConn は接続が持っていることを表す。
	OwnerConn
)

var ownerNames = []string{"none", "human", "agent"}

// String は持ち主の種類の名前を返す。
func (k OwnerKind) String() string { return ownerNames[k] }

// Mode は進行モード（設計書 14 編 §14.4.2）。
type Mode uint8

// 進行モード。
const (
	// ModeAgentPaced はエージェントの進行要求の間だけ進むことを表す。
	ModeAgentPaced Mode = iota
	// ModeRealTime は実時間で走ることを表す。
	ModeRealTime
)

var modeNames = []string{"agent_paced", "real_time"}

// String はモードの名前を返す。
func (m Mode) String() string { return modeNames[m] }

// MarshalJSON はモードを名前で書く。
func (m Mode) MarshalJSON() ([]byte, error) { return json.Marshal(m.String()) }

// UnmarshalJSON は名前からモードを読む。
func (m *Mode) UnmarshalJSON(b []byte) error {
	i, err := nameIndex(b, modeNames)
	*m = Mode(i)
	return err
}

// nameIndex は JSON の文字列が names の何番目かを返す。
func nameIndex(b []byte, names []string) (int, error) {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return 0, err
	}
	for i, n := range names {
		if n == s {
			return i, nil
		}
	}
	return 0, fmt.Errorf("agent: 知らない名前 %q", s)
}

// controlState は Instance の Control。Instance.mu で守る。
type controlState struct {
	owner OwnerKind
	conn  *Conn
	mode  Mode
}

// ControlStatus は control.status と instance.list で返す Control の状態。
type ControlStatus struct {
	Owner  string `json:"owner"`
	Conn   ConnID `json:"conn,omitempty"`
	Client string `json:"client,omitempty"`
	Mode   Mode   `json:"mode"`
}

// status は Control の状態を返す。Instance.mu を持って呼ぶ。
func (c *controlState) status() ControlStatus {
	s := ControlStatus{Owner: c.owner.String(), Mode: c.mode}
	if c.conn != nil {
		s.Conn = c.conn.ID
		s.Client = c.conn.Client()
	}
	return s
}

// heldBy は conn が Control を持っているかを返す。Instance.mu を持って呼ぶ。
func (c *controlState) heldBy(conn *Conn) bool {
	return c.owner == OwnerConn && c.conn == conn
}
