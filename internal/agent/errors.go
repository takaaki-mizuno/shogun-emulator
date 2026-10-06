package agent

import (
	"errors"
	"fmt"
)

// Error は Agent Command のエラー。JSON-RPC のエラーにそのまま写す
// （設計書 14 編 §14.5.1）。
type Error struct {
	// Code は JSON-RPC のエラーコード。
	Code int
	// Kind は error.data.kind に入れる名前。
	Kind string
	// Message は説明。
	Message string
	// Position は式の構文エラーの位置。無いとき -1。
	Position int
}

// Error は説明を返す。
func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Kind, e.Message) }

// エラーの種類（設計書 14 編 §14.5.1 の表と JSON-RPC の標準のエラー）。
const (
	KindParseError          = "parse_error"
	KindInvalidRequest      = "invalid_request"
	KindMethodNotFound      = "method_not_found"
	KindInvalidParams       = "invalid_params"
	KindInternalError       = "internal_error"
	KindUnauthorized        = "unauthorized"
	KindControlRequired     = "control_required"
	KindControlHeld         = "control_held"
	KindInstanceNotFound    = "instance_not_found"
	KindInstanceRequired    = "instance_required"
	KindNotLoaded           = "not_loaded"
	KindInvalidLocation     = "invalid_location"
	KindInvalidExpression   = "invalid_expression"
	KindUnsupportedInGUI    = "unsupported_in_gui"
	KindLimitExceeded       = "limit_exceeded"
	KindCancelled           = "cancelled"
	KindMovieConflict       = "movie_conflict"
	KindIOError             = "io_error"
	KindUnsupportedHeadless = "unsupported_in_headless"
)

// errorCodes は種類とコードの対応。表の順に並べる。
var errorCodes = []struct {
	kind string
	code int
}{
	{KindParseError, -32700},
	{KindInvalidRequest, -32600},
	{KindMethodNotFound, -32601},
	{KindInvalidParams, -32602},
	{KindInternalError, -32603},
	{KindUnauthorized, -32001},
	{KindControlRequired, -32002},
	{KindControlHeld, -32003},
	{KindInstanceNotFound, -32004},
	{KindInstanceRequired, -32005},
	{KindNotLoaded, -32006},
	{KindInvalidLocation, -32007},
	{KindInvalidExpression, -32008},
	{KindUnsupportedInGUI, -32009},
	{KindLimitExceeded, -32010},
	{KindCancelled, -32011},
	{KindMovieConflict, -32012},
	{KindIOError, -32013},
	{KindUnsupportedHeadless, -32014},
}

// CodeOf は種類に対応するコードを返す。知らない種類は内部エラーのコード。
func CodeOf(kind string) int {
	for _, c := range errorCodes {
		if c.kind == kind {
			return c.code
		}
	}
	return -32603
}

// Errorf は種類と説明からエラーを作る。
func Errorf(kind, format string, args ...any) *Error {
	return &Error{Code: CodeOf(kind), Kind: kind, Message: fmt.Sprintf(format, args...), Position: -1}
}

// AsError は err を Error にする。Error でないものは内部エラーとする。
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return Errorf(KindInternalError, "%v", err)
}
