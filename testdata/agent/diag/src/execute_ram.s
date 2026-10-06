; RAM に置いた RTS を実行する。
.include "common.inc"

.segment "CODE"
reset:
    init
    clear_ram
    lda #$60            ; RTS
    sta $0200
trigger:
    jsr $0200

forever:
    jmp forever

nmi:
    rti

irq:
    rti

.segment "VECTORS"
    .word nmi, reset, irq
