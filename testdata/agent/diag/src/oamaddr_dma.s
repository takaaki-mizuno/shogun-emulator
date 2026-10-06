; OAMADDR が 0 でないまま OAM DMA を行う。
.include "common.inc"

.segment "CODE"
reset:
    init
    clear_ram
    lda #$10
    sta OAMADDR
    lda #$02
trigger:
    sta OAMDMA

forever:
    jmp forever

nmi:
    rti

irq:
    rti

.segment "VECTORS"
    .word nmi, reset, irq
