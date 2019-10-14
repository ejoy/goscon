package sproto

import (
	"errors"
)

var (
	ErrNonPtr    = errors.New("sproto: called with Non-Ptr type")
	ErrNonStruct = errors.New("sproto: Encode called with Non-Ptr")
	ErrNil       = errors.New("sproto: Encode called with nil")
	ErrDecode    = errors.New("sproto: Decode msg failed")
	ErrUnpack    = errors.New("sproto: Unpack data failed")
)

func Append(dst, src []byte) []byte {
	l := len(dst)
	if l+len(src) > cap(dst) {
		// allocate double what's needed, for future growth
		buf := make([]byte, (l+len(src))*2)
		copy(buf, dst)
		dst = buf
	}
	dst = dst[0 : l+len(src)]
	copy(dst[l:], src)
	return dst
}

func String(v string) *string {
	return &v
}

func Bool(v bool) *bool {
	return &v
}

func Int8(v int8) *int8 {
	return &v
}

func Uint8(v uint8) *uint8 {
	return &v
}

func Int16(v int16) *int16 {
	return &v
}

func Uint16(v uint16) *uint16 {
	return &v
}

func Int32(v int32) *int32 {
	return &v
}

func Uint32(v uint32) *uint32 {
	return &v
}

func Int64(v int64) *int64 {
	return &v
}

func Uint64(v uint64) *uint64 {
	return &v
}

func Int(v int) *int {
	return &v
}

func Uint(v uint) *uint {
	return &v
}

// encode && pack
func EncodePacked(sp interface{}) ([]byte, error) {
	unpacked, err := Encode(sp)
	if err != nil {
		return nil, err
	}
	return Pack(unpacked), nil
}

// unpack && decode
func DecodePacked(data []byte, sp interface{}) error {
	unpacked, err := Unpack(data)
	if err != nil {
		return err
	}
	_, err = Decode(unpacked, sp)
	return err
}
