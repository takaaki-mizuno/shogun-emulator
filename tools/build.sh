#!/usr/bin/env sh
# 将軍エミュレータをビルドする。
#
# 使い方:
#   tools/build.sh                 現在のプラットフォーム向けにリリースビルド
#   tools/build.sh debug           シンボルを残したデバッグビルド
#   GOOS=windows GOARCH=amd64 tools/build.sh
#
# バージョン情報は次の優先順で決める。
#   1. 環境変数 SHOGUN_VERSION / SHOGUN_COMMIT / SHOGUN_BUILD_DATE
#   2. git から取得した値
#   3. 既定値（version.go の初期値をそのまま使う）
#
# git が無い環境、リポジトリでない場所でもビルドが失敗しないようにしてある。
# 配布物はタグから作るが、ソースアーカイブからのビルドも成立させる。

set -eu

mode="${1:-release}"

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"

goos=$(go env GOOS)
goarch=$(go env GOARCH)

# --- バージョン情報 ---------------------------------------------------------

# 版とコミットの両方を環境変数で渡したときは git を呼ばない。
have_git=0
if [ -z "${SHOGUN_VERSION:-}" ] || [ -z "${SHOGUN_COMMIT:-}" ]; then
	if command -v git >/dev/null 2>&1 && git rev-parse --git-dir >/dev/null 2>&1; then
		have_git=1
	fi
fi

version="${SHOGUN_VERSION:-}"
if [ -z "$version" ] && [ "$have_git" -eq 1 ]; then
	version=$(git describe --tags --always --dirty 2>/dev/null || true)
fi

commit="${SHOGUN_COMMIT:-}"
if [ -z "$commit" ] && [ "$have_git" -eq 1 ]; then
	commit=$(git rev-parse --short HEAD 2>/dev/null || true)
fi

build_date="${SHOGUN_BUILD_DATE:-}"
if [ -z "$build_date" ]; then
	# SOURCE_DATE_EPOCH があればそれを使う。再現可能なビルドのため。
	if [ -n "${SOURCE_DATE_EPOCH:-}" ]; then
		build_date=$(date -u -r "$SOURCE_DATE_EPOCH" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null ||
			date -u -d "@$SOURCE_DATE_EPOCH" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || true)
	else
		build_date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
	fi
fi

ldflags=""
[ -n "$version" ] && ldflags="$ldflags -X main.version=$version"
[ -n "$commit" ] && ldflags="$ldflags -X main.commit=$commit"
[ -n "$build_date" ] && ldflags="$ldflags -X main.buildDate=$build_date"

# --- モードごとのフラグ -----------------------------------------------------

case "$mode" in
release)
	# -s -w はシンボルテーブルと DWARF を除く
	ldflags="-s -w$ldflags"
	;;
debug)
	# デバッガで追えるようにシンボルを残す
	;;
*)
	echo "不明なモード: ${mode}（release または debug）" >&2
	exit 2
	;;
esac

# Windows の GUI バイナリはコンソールウィンドウを開かない。
# コマンドラインから起動したときは実行時にコンソールへ接続する。
if [ "$goos" = "windows" ]; then
	ldflags="$ldflags -H windowsgui"
fi

# Windows の実行ファイルへアイコンとバージョン情報を埋め込む（設計書 13 編 §13.4）。
# 版は "1.2.3.0" の形で渡す。タグが v1.2.3 の形でないときは 0.0.0.0 とする。
if [ "$goos" = "windows" ]; then
	win_version=$(printf '%s' "${version:-}" | sed -n 's/^v\{0,1\}\([0-9][0-9]*\)\.\([0-9][0-9]*\)\.\([0-9][0-9]*\).*/\1.\2.\3.0/p')
	[ -z "$win_version" ] && win_version="0.0.0.0"
	echo "リソース: ${win_version}"
	SHOGUN_WIN_VERSION="$win_version" go generate ./cmd/shogun
fi

# --- 出力先 ----------------------------------------------------------------

outdir="dist/${goos}_${goarch}"
out="$outdir/shogun"
[ "$goos" = "windows" ] && out="$out.exe"
mkdir -p "$outdir"

# 変数のあとに全角文字が続くときは ${} で囲む。シェルが名前の一部と
# 解釈することがある。
echo "ビルド: ${out}（${mode}, ${version:-dev}）"
go build -trimpath -ldflags "$ldflags" -o "$out" ./cmd/shogun
echo "完了: $out"
