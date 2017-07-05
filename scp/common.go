package scp

import (
	"fmt"
)

var NetBufferSize = 32 * 1024
var SentCacheSize = 64 * 1024 // 64k

const (
	SCPStatusOK             = 200
	SCPStatusUnauthorized   = 401
	SCPStatusExpired        = 403
	SCPStatusIDNotFound     = 404
	SCPStatusNotAcceptable  = 406
	SCPStatusServerInternal = 501
)

var ErrIllegalMsg = fmt.Errorf("Illegal Message")
var ErrUnauthorized = fmt.Errorf("401 Unauthorized")
var ErrIndexExpired = fmt.Errorf("403 Index Expired")
var ErrIDNotFound = fmt.Errorf("404 ID Not Found")
var ErrNotAcceptable = fmt.Errorf("406 Not Acceptable")

func newError(code int) error {
	switch code {
	case SCPStatusOK:
		return nil
	case SCPStatusUnauthorized:
		return ErrUnauthorized
	case SCPStatusExpired:
		return ErrIndexExpired
	case SCPStatusIDNotFound:
		return ErrIDNotFound
	case SCPStatusNotAcceptable:
		return ErrNotAcceptable
	default:
		return fmt.Errorf("%d Unknown", code)
	}
}
