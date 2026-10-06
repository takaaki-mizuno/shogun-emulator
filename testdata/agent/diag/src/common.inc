; Diagnostic のテスト用 ROM の共通部分（設計書 14 編 §14.24、計画フェーズ 20 §3.8）。
;
; 自作のソースであり、既存のエミュレータやゲームのコードを使っていない。
; 各 ROM は reset で準備をしてから対象の誤りを 1 回だけ起こし、forever の
; 無限ループ（JMP *）で止まる。

PPUCTRL   = $2000
PPUMASK   = $2001
PPUSTATUS = $2002
OAMADDR   = $2003
PPUADDR   = $2006
PPUDATA   = $2007
OAMDMA    = $4014

.segment "HEADER"
    .byte "NES", $1A
    .byte 1                 ; PRG 16 KiB
    .byte 0                 ; CHR-RAM
    .byte $00               ; マッパー 0（NROM）、水平ミラー
    .byte $00
    .res 8, 0

; init は割り込みを止め、スタックを置き、VBlank を 2 回待って PPU の起動を
; 待つ。RAM は読まない。
.macro init
    sei
    cld
    ldx #$FF
    txs
    bit PPUSTATUS
:   bit PPUSTATUS
    bpl :-
:   bit PPUSTATUS
    bpl :-
.endmacro

; clear_ram は内蔵 RAM をすべて 0 にする（読む前に書く）。
.macro clear_ram
    lda #0
    tax
:   sta $0000,x
    sta $0100,x
    sta $0200,x
    sta $0300,x
    sta $0400,x
    sta $0500,x
    sta $0600,x
    sta $0700,x
    inx
    bne :-
.endmacro

; wait_vblank_poll は $2002 の VBlank フラグが立つまで待つ。
.macro wait_vblank_poll
:   bit PPUSTATUS
    bpl :-
.endmacro
