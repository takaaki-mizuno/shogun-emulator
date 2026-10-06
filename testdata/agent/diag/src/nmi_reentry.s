; NMI の処理の途中で次の NMI を受ける（1 回だけ）。
.include "common.inc"

.segment "ZEROPAGE"
nmi_depth: .res 1
nmi_inner: .res 1

.segment "CODE"
reset:
    init
    clear_ram
    lda #$80
    sta PPUCTRL

forever:
    jmp forever

nmi:
    lda nmi_depth
    bne inner
    inc nmi_depth       ; 1 回目: 次の NMI が来るまで RTI しない
:   lda nmi_inner
    beq :-
    lda #2
    sta nmi_depth
    rti
inner:
    cmp #1
    bne done
trigger:
    inc nmi_inner       ; 2 回目（入れ子）
done:
    rti

irq:
    rti


.segment "VECTORS"
    .word nmi, reset, irq
