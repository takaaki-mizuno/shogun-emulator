// Package mp4rec はエミュレーションの画と音を MP4 に書き出す
// （設計書 08 編 §8.8）。
//
// 映像は Motion JPEG、音声は無圧縮の PCM とする。Go だけで書け、
// macOS の QuickTime Player で再生できる。internal/video と mp4ff だけを
// 参照する（設計書 01 編 §1.4）。
package mp4rec
