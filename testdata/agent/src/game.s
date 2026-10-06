; Agent Interface のテスト用 ROM（設計書 14 編 §14.24、計画フェーズ 16 §3.1）。
;
; 自作のソースであり、既存のエミュレータやゲームのコードを使っていない。
; NMI ごとに次の規則で Game State の値を更新する。
;
;   frame_count  2 バイト。NMI ごとに 1 増える
;   mode         (frame_count >> 6) & 3。0 title、1 play、2 dead、3 clear
;   player_x     NMI ごとに 1 増える（8 bit で折り返す）
;   player_y     $80 のまま
;   score        3 バイトの BCD（上位の桁が先）。NMI ごとに 1 増える
;   flags        bit 0 = frame_count の bit 0、bit 7 = frame_count の bit 3
;   enemy_x      8 要素。enemy_x[i] = player_x + i * 16
;   cur_bank     frame_count の bit 0。$8000 に切り替える PRG バンク
;   bank_marker  切り替えたバンクの bank_entry が書く値（$A0 か $A1）

.include "macros.inc"

; 定数。.dbg では type=equ の Symbol になり、位置としては扱わない。
PLAYER_Y_START = $80

.segment "HEADER"
    .byte "NES", $1A
    .byte 8                 ; PRG 16 KiB × 8
    .byte 0                 ; CHR-RAM
    .byte $10               ; マッパー 1（MMC1）
    .byte $00
    .res 8, 0

.segment "ZEROPAGE"
mode:        .res 1
player_x:    .res 1
player_y:    .res 1
frame_count: .res 2
ptr:         .res 2

.segment "BSS"
score:       .res 3
flags:       .res 1
enemy_x:     .res 8
cur_bank:    .res 1
bank_marker: .res 1

; 2 つのバンクの $8000 に、別の名前のラベルを置く。
.segment "BANK0"
bank0_entry:
    lda #$A0
    sta bank_marker
    rts

.segment "BANK1"
bank1_entry:
    lda #$A1
    sta bank_marker
    rts

.segment "RODATA"
message:
    .byte "AGENT"

.segment "CODE"

.proc reset
    sei
    cld
    ldx #$FF
    txs
    mmc1_write $8000, $0E   ; PRG 16 KiB 切り替え、$C000 固定、垂直配置
    mmc1_write $E000, 0
    bit $2002
vblank1:
    bit $2002
    bpl vblank1
vblank2:
    bit $2002
    bpl vblank2
    lda #PLAYER_Y_START
    sta player_y
    lda #<message
    sta ptr
    lda #>message
    sta ptr+1
    lda #$80
    sta $2000
loop:
    jmp loop
.endproc

.proc nmi
    inc16 frame_count
    jsr update_mode
    jsr update_player
    jsr update_score
    jsr update_flags
    jsr switch_bank
    jsr $8000               ; 切り替えたバンクの bank_entry
    rti
.endproc

.proc update_mode
    lda frame_count
    lsr a
    lsr a
    lsr a
    lsr a
    lsr a
    lsr a
    sta mode
    lda frame_count+1
    asl a
    asl a
    ora mode
    and #3
    sta mode
    rts
.endproc

.proc update_player
    inc player_x
    lda player_x
    ldx #0
loop:
    sta enemy_x,x
    clc
    adc #16
    inx
    cpx #8
    bne loop
    rts
.endproc

; score（上位の桁が先の 3 バイトの BCD）に 1 を足す。
.proc update_score
    ldx #2
loop:
    lda score,x
    clc
    adc #1
    tay
    and #$0F
    cmp #$0A
    bcc done_digit
    tya
    and #$F0
    clc
    adc #$10
    cmp #$A0
    bcc store_carry_free
    lda #0
    sta score,x
    dex
    bpl loop
    rts
store_carry_free:
    sta score,x
    rts
done_digit:
    tya
    sta score,x
    rts
.endproc

.proc update_flags
    lda frame_count
    and #$01
    sta flags
    lda frame_count
    and #$08
    beq done
    lda flags
    ora #$80
    sta flags
done:
    rts
.endproc

.proc switch_bank
    lda frame_count
    and #1
    sta cur_bank
    mmc1_write_a $E000
    rts
.endproc

.proc irq
    rti
.endproc

.segment "VECTORS"
    .word nmi, reset, irq
