//go:build !darwin

package main

import "errors"

// setCustomIconFlag は macOS 以外では使えない。.dmg は macOS で作る。
func setCustomIconFlag(string) error {
	return errors.New("Finder 情報は macOS でだけ書ける")
}
