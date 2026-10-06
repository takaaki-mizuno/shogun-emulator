; 色 $0D をパレットへ書く。
.include "common.inc"

.segment "CODE"
reset:
    init
    clear_ram
    lda #$3F
    sta PPUADDR
    lda #$01
    sta PPUADDR
    lda #$0D
trigger:
    sta PPUDATA

forever:
    jmp forever

nmi:
    rti

irq:
    rti

.segment "VECTORS"
    .word nmi, reset, irq
