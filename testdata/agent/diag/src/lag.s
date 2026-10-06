; プロファイルのテスト用。wait_vblank で NMI を待ち、4 フレームに 1 回、1 フレームを超える処理（heavy）をする。
.include "common.inc"

.segment "ZEROPAGE"
nmi_flag: .res 1
frame:    .res 1

.segment "CODE"
reset:
    init
    clear_ram
    lda #$80
    sta PPUCTRL
main:
    jsr wait_vblank
    jsr update
    lda frame
    and #3
    bne main
    jsr heavy
    jmp main

forever:
    jmp forever

nmi:
    pha
    inc nmi_flag
    pla
    rti

; wait_vblank は NMI まで待つ（アイドルループ）。
wait_vblank:
    lda #0
    sta nmi_flag
:   lda nmi_flag
    beq :-
    rts

update:
    inc frame
    rts

; heavy は約 40000 サイクルかかる（1 フレームは約 29780 サイクル）。
heavy:
    ldy #32
:   ldx #0
:   dex
    bne :-
    dey
    bne :--
    rts

irq:
    rti


.segment "VECTORS"
    .word nmi, reset, irq
