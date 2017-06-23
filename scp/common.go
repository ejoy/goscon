package scp

import (
	"fmt"
)

var SentCacheSize = 64 * 1024 // 64k

var ErrIllegalMsg = fmt.Errorf("Illegal Message")
var ErrUnauthorized = fmt.Errorf("401 Unauthorized")
var ErrIndexExpired = fmt.Errorf("403 Index Expired")
var ErrIDNotFound = fmt.Errorf("404 ID Not Found")
var ErrNotAcceptable = fmt.Errorf("406 Not Acceptable")

func newError(code int) error {
	switch code {
	case 200:
		return nil
	case 401:
		return ErrUnauthorized
	case 403:
		return ErrIndexExpired
	case 404:
		return ErrIDNotFound
	case 406:
		return ErrNotAcceptable
	default:
		return fmt.Errorf("%d Unknown", code)
	}
}
