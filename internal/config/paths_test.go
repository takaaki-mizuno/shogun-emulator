package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakePlatform は OS ごとの標準のディレクトリを与える。
func fakePlatform(goos string, env map[string]string) platform {
	home := "/home/u"
	cfg, cache := "", ""
	switch goos {
	case "darwin":
		home = "/Users/u"
		cfg, cache = home+"/Library/Application Support", home+"/Library/Caches"
	case "windows":
		home = `C:\Users\u`
		cfg, cache = `C:\Users\u\AppData\Roaming`, `C:\Users\u\AppData\Local`
	case "linux":
		cfg, cache = home+"/.config", home+"/.cache"
	}
	return platform{
		goos:          goos,
		getenv:        func(k string) string { return env[k] },
		home:          func() (string, error) { return home, nil },
		userConfigDir: func() (string, error) { return cfg, nil },
		userCacheDir:  func() (string, error) { return cache, nil },
		executableDir: func() (string, error) { return "/opt/shogun", nil },
		mkdirAll:      func(string, os.FileMode) error { return nil },
	}
}

// TestStandardPathsPerOS は 3 つの OS の保存先が設計書 11 編 §11.2 の表どおり
// であることを確かめる。
func TestStandardPathsPerOS(t *testing.T) {
	j := filepath.Join
	cases := []struct {
		goos string
		env  map[string]string
		want Paths
	}{
		{"darwin", nil, Paths{
			Config:      j("/Users/u/Library/Application Support", "ShogunEmulator"),
			Data:        j("/Users/u/Library/Application Support", "ShogunEmulator"),
			Cache:       j("/Users/u/Library/Caches", "ShogunEmulator"),
			Logs:        j("/Users/u", "Library", "Logs", "ShogunEmulator"),
			Screenshots: j("/Users/u", "Pictures", "ShogunEmulator"),
			Videos:      j("/Users/u", "Movies", "ShogunEmulator"),
		}},
		{"windows", nil, Paths{
			Config:      j(`C:\Users\u\AppData\Roaming`, "ShogunEmulator"),
			Data:        j(`C:\Users\u\AppData\Roaming`, "ShogunEmulator"),
			Cache:       j(`C:\Users\u\AppData\Local`, "ShogunEmulator", "cache"),
			Logs:        j(`C:\Users\u\AppData\Local`, "ShogunEmulator", "logs"),
			Screenshots: j(`C:\Users\u`, "Pictures", "ShogunEmulator"),
			Videos:      j(`C:\Users\u`, "Videos", "ShogunEmulator"),
		}},
		{"linux", nil, Paths{
			Config:      j("/home/u/.config", "shogun-emulator"),
			Data:        j("/home/u", ".local", "share", "shogun-emulator"),
			Cache:       j("/home/u/.cache", "shogun-emulator"),
			Logs:        j("/home/u", ".local", "state", "shogun-emulator", "logs"),
			Screenshots: j("/home/u", "Pictures", "ShogunEmulator"),
			Videos:      j("/home/u", "Videos", "ShogunEmulator"),
		}},
		{"linux", map[string]string{"XDG_DATA_HOME": "/xdg/data", "XDG_STATE_HOME": "/xdg/state"}, Paths{
			Config:      j("/home/u/.config", "shogun-emulator"),
			Data:        j("/xdg/data", "shogun-emulator"),
			Cache:       j("/home/u/.cache", "shogun-emulator"),
			Logs:        j("/xdg/state", "shogun-emulator", "logs"),
			Screenshots: j("/home/u", "Pictures", "ShogunEmulator"),
			Videos:      j("/home/u", "Videos", "ShogunEmulator"),
		}},
	}
	for _, c := range cases {
		got, err := resolvePaths(fakePlatform(c.goos, c.env), false, Overrides{}, false)
		if err != nil {
			t.Fatalf("%s: %v", c.goos, err)
		}
		if got != c.want {
			t.Errorf("%s %v:\n実際 %+v\n期待 %+v", c.goos, c.env, got, c.want)
		}
	}
}

// TestPortableAndOverrides はポータブルモードと上書きを確かめる（設計書 11 編 §11.2.1）。
func TestPortableAndOverrides(t *testing.T) {
	pf := fakePlatform("linux", nil)
	got, err := resolvePaths(pf, true, Overrides{Logs: "/tmp/logs"}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{Config: "/opt/shogun", Data: "/opt/shogun", Cache: filepath.Join("/opt/shogun", "cache"),
		Logs: "/tmp/logs", Screenshots: filepath.Join("/opt/shogun", "screenshots"),
		Videos: filepath.Join("/opt/shogun", "videos")}
	if got != want {
		t.Errorf("実際 %+v\n期待 %+v", got, want)
	}
	if d := got.SaveDir(""); d != filepath.Join("/opt/shogun", "saves") {
		t.Errorf("セーブの保存先 = %s", d)
	}
	if d := got.SaveDir("/x"); d != "/x" {
		t.Errorf("上書きしたセーブの保存先 = %s", d)
	}
	if d := got.VideoDir(""); d != filepath.Join("/opt/shogun", "videos") {
		t.Errorf("動画の保存先 = %s", d)
	}
	if d := got.VideoDir("/v"); d != "/v" {
		t.Errorf("上書きした動画の保存先 = %s", d)
	}
}

// TestFallbackWhenConfigDirUnknown は OS の設定ディレクトリを求められないとき
// ホームディレクトリ直下へ退避することを確かめる。
func TestFallbackWhenConfigDirUnknown(t *testing.T) {
	pf := fakePlatform("darwin", nil)
	pf.userConfigDir = func() (string, error) { return "", errors.New("無い") }
	got, err := resolvePaths(pf, false, Overrides{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/Users/u", ".shogun-emulator", "config"); got.Config != want {
		t.Errorf("設定の保存先 = %s, 期待 %s", got.Config, want)
	}
	pf.home = func() (string, error) { return "", errors.New("無い") }
	if _, err := resolvePaths(pf, false, Overrides{}, false); err == nil {
		t.Error("ホームも分からないのにエラーにならない")
	}
}

// TestResolvePathsCreatesDirs は設定・データ・ログのディレクトリを作ることを確かめる。
func TestResolvePathsCreatesDirs(t *testing.T) {
	dir := t.TempDir()
	pf := hostPlatform()
	pf.executableDir = func() (string, error) { return dir, nil }
	p, err := resolvePaths(pf, true, Overrides{}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{p.Config, p.Data, p.Logs} {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			t.Errorf("%s が作られていない", d)
		}
	}
}
