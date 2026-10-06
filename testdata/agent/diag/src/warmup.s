; PPU が書き込みを受け付ける前に $2000 へ書く。
.include "common.inc"

.segment "CODE"
reset:
    lda #0
trigger:
    sta PPUCTRL
    init

forever:
    jmp forever

nmi:
    rti

irq:
    rti

.segment "VECTORS"
    .word nmi, reset, irq
