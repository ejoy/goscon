package scp

import (
	"errors"
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

var ErrIllegalMsg = errors.New("Illegal Message")
var ErrChecksumNotMatch = errors.New("Checksum Not Match")
var ErrSignatureNotMatch = errors.New("Signature Not Match")
var ErrUnauthorized = errors.New("401 Unauthorized")
var ErrIndexExpired = errors.New("403 Index Expired")
var ErrIDNotFound = errors.New("404 ID Not Found")
var ErrNotAcceptable = errors.New("406 Not Acceptable")

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
