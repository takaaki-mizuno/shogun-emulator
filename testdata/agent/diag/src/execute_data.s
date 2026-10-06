; RODATA に置いた JMP を実行する。
.include "common.inc"

.segment "CODE"
reset:
    init
    clear_ram
    jmp data_code

forever:
    jmp forever

nmi:
    rti

irq:
    rti

.segment "RODATA"
data_code:
    .byte $4C, <forever, >forever   ; JMP forever（データのセグメントにある）

.segment "VECTORS"
    .word nmi, reset, irq
