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

func newError(code int, extra string) error {
	if extra == "" {
		extra = "no extra info"
	}
	switch code {
		case SCPStatusOK:
			return nil
		case SCPStatusUnauthorized:
			return fmt.Errorf("401 Unauthorized, %s", extra)
		case SCPStatusExpired:
			return fmt.Errorf("403 Index Expired, %s", extra)
		case SCPStatusIDNotFound:
			return fmt.Errorf("404 ID Not Found, %s", extra)
		case SCPStatusNotAcceptable:
			return fmt.Errorf("406 Not Acceptable, %s", extra)
		default:
			return fmt.Errorf("%d Unknown", code)
	}
}