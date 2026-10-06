; S が $00 のときに積み込む。
.include "common.inc"

.segment "CODE"
reset:
    init
    clear_ram
    ldx #$00
    txs
trigger:
    pha
    ldx #$FF
    txs

forever:
    jmp forever

nmi:
    rti

irq:
    rti

.segment "VECTORS"
    .word nmi, reset, irq
