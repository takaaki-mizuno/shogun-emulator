#!/bin/sh
# Diagnostic とプロファイルのテスト用 ROM をビルドする（計画フェーズ 20 §3.8）。
#
# ビルドした .nes と .dbg はリポジトリに含める。CI に cc65 を入れずに
# テストを動かすためである。ソースを変えたら、このスクリプトで作り直す。
set -eu
cd "$(dirname "$0")/src"
for s in *.s; do
    name="${s%.s}"
    ca65 -g -o "../$name.o" "$s"
    ld65 -C nrom.cfg --dbgfile "../$name.dbg" -o "../$name.nes" "../$name.o"
    rm -f "../$name.o"
done
