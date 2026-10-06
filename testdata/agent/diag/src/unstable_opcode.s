; 不安定な非公式命令 XAA（$8B）を実行する。
.include "common.inc"

.segment "CODE"
reset:
    init
    clear_ram
    lda #$FF
    ldx #$0F
trigger:
    .byte $8B, $33      ; XAA #$33

forever:
    jmp forever

nmi:
    rti

irq:
    rti

.segment "VECTORS"
    .word nmi, reset, irq
