; S が $FF のときに取り出す。
.include "common.inc"

.segment "CODE"
reset:
    init
    clear_ram
trigger:
    pla

forever:
    jmp forever

nmi:
    rti

irq:
    rti

.segment "VECTORS"
    .word nmi, reset, irq
