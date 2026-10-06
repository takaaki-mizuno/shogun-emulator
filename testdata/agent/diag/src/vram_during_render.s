; 描画中に $2007 を読む。
.include "common.inc"

.segment "CODE"
reset:
    init
    clear_ram
    lda #$1E            ; 背景とスプライトを表示する
    sta PPUMASK
    wait_vblank_poll
    ldx #0              ; VBlank の後、描画中に入るまで待つ（約 3500 サイクル）
    ldy #3
:   dex
    bne :-
    dey
    bne :-
trigger:
    lda PPUDATA

forever:
    jmp forever

nmi:
    rti

irq:
    rti

.segment "VECTORS"
    .word nmi, reset, irq
