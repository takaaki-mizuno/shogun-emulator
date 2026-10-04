package main

// Windows の実行ファイルへ埋め込むアイコンとバージョン情報のリソースを作る
// （設計書 13 編 §13.4）。resource_windows_<arch>.syso を書き、Windows 以外の
// ビルドでは名前により無視される。
//
// SHOGUN_WIN_VERSION には "1.2.3.0" の形の版を渡す。tools/build.sh がタグから
// 決めて設定する。空のときは versioninfo.json の 0.0.0.0 を使う。
//
//go:generate go tool goversioninfo -platform-specific -propagate-ver-strings -file-version=${SHOGUN_WIN_VERSION} -product-version=${SHOGUN_WIN_VERSION} versioninfo.json
