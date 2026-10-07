package main

import "golang.org/x/sys/unix"

// setCustomIconFlag は path の Finder 情報にカスタムアイコンの印を立てる。
//
// Finder 情報（32 バイト）の 8 バイト目からのフラグ（ビッグエンディアン）の
// 0x0400 がカスタムアイコンの印である。SetFile や xattr のコマンドは版や
// 入れ方で使える引数が違うため、システムコールで書く。
func setCustomIconFlag(path string) error {
	info := make([]byte, 32)
	info[8] = 0x04
	return unix.Setxattr(path, "com.apple.FinderInfo", info, 0)
}
