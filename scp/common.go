package scp

import (
	"fmt"
)

// NetBufferSize .
var NetBufferSize = 32 * 1024 // 32k
// ReuseBufferSize .
var ReuseBufferSize = 64 * 1024 // 64k

// SCPStatus Code
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

// ErrIllegalMsg .
var ErrIllegalMsg = &Error{400, "illegal message"}

// ErrUnauthorized .
var ErrUnauthorized = &Error{401, "Unauthorized"}

// ErrIndexExpired .
var ErrIndexExpired = &Error{403, "Index Expired"}

// ErrIDNotFound .
var ErrIDNotFound = &Error{404, "ID Not Found"}

// ErrNotAcceptable .
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
