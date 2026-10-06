; テスト用 ROM のマクロ。ソース行の対応で、マクロの展開の行より呼び出した
; 行を優先することを確かめるために使う（設計書 14 編 §14.11.3）。

; 2 バイトの値に 1 を足す。
.macro inc16 addr
    .local skip
    inc addr
    bne skip
    inc addr+1
skip:
.endmacro

; MMC1 のレジスタ reg に即値 value を 5 回の書き込みで送る。
.macro mmc1_write reg, value
    lda #value
    mmc1_write_a reg
.endmacro

; MMC1 のレジスタ reg に A の下位 5 bit を送る。
.macro mmc1_write_a reg
    sta reg
    lsr a
    sta reg
    lsr a
    sta reg
    lsr a
    sta reg
    lsr a
    sta reg
.endmacro
