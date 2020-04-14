package scp

import (
	"fmt"
)

var NetBufferSize = 32 * 1024   // 32k
var RueseBufferSize = 64 * 1024 // 64k

const (
	SCPStatusOK            = 200 // succeed
	SCPStatusBadRequest    = 400 // malformed request
	SCPStatusUnauthorized  = 401 // verify checksum failed
	SCPStatusExpired       = 403 // verify handshake number failed
	SCPStatusIDNotFound    = 404 // match old connection failed
	SCPStatusNotAcceptable = 406 // reuse buffer overflow
	SCPStatusNetworkError  = 501 //
)

// Error .
type Error struct {
	Code int
	Desc string
}

// Error .
func (se *Error) Error() string {
	return fmt.Sprintf("%d %s", se.Code, se.Desc)
}

var ErrIllegalMsg = &Error{400, "illegal message"}
var ErrUnauthorized = &Error{401, "Unauthorized"}
var ErrIndexExpired = &Error{403, "Index Expired"}
var ErrIDNotFound = &Error{404, "ID Not Found"}
var ErrNotAcceptable = &Error{406, "Not Acceptable"}

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
