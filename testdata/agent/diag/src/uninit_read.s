; 書き込みの無い RAM を読む。
.include "common.inc"

.segment "CODE"
reset:
    init
trigger:
    lda $0400

forever:
    jmp forever

nmi:
    rti

irq:
    rti

.segment "VECTORS"
    .word nmi, reset, irq
