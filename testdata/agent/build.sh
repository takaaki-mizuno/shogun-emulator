#!/bin/sh
# Agent Interface のテスト用 ROM をビルドする（計画フェーズ 16 §3.1）。
#
# ビルドした game.nes と game.dbg はリポジトリに含める。CI に cc65 を入れずに
# テストを動かすためである。ソースを変えたら、このスクリプトで作り直す。
#
# .dbg のファイル名を相対パスにするため、src ディレクトリでビルドする。
set -eu
cd "$(dirname "$0")/src"
ca65 -g -o ../game.o game.s
ld65 -C game.cfg --dbgfile ../game.dbg -o ../game.nes ../game.o
rm -f ../game.o
