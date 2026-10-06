; 対照の ROM。Diagnostic を 1 件も起こさない。NMI ごとに update を呼び、main はアイドルループで待つ。
.include "common.inc"

.segment "ZEROPAGE"
nmi_count:  .res 1
frame_work: .res 1

.segment "CODE"
reset:
    init
    clear_ram
    lda #$3F            ; パレット 0 を通常の色にする
    sta PPUADDR
    lda #$00
    sta PPUADDR
    lda #$0F
    sta PPUDATA
    lda #$80            ; NMI を有効にする
    sta PPUCTRL
main:
    jsr wait_nmi
    jsr update
    jmp main

forever:
    jmp forever

nmi:
    pha
    inc nmi_count
    pla
    rti

; wait_nmi は次の NMI まで待つ（アイドルループ）。
wait_nmi:
    lda nmi_count
:   cmp nmi_count
    beq :-
    rts

; update は毎フレーム 1 回呼ばれる処理。
update:
    inc frame_work
    jsr helper
    rts

helper:
    lda frame_work
    rts

irq:
    rti


.segment "VECTORS"
    .word nmi, reset, irq
