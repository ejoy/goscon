package sproto

import (
	"fmt"
	"os"
	"reflect"
)

type Tag struct {
	Tag uint16
	Val *uint16
}

func readChunk(chunk []byte) (int, []byte, error) {
	if len(chunk) < 4 {
		return 0, nil, ErrDecode
	}
	sz := int(readUint32(chunk))
	expected := 4 + sz
	if len(chunk) < expected {
		return 0, nil, ErrDecode
	}
	return expected, chunk[4:expected], nil
}

func readUint16(buf []byte) uint16 {
	var n uint16
	n = uint16(buf[1]) << 8
	n |= uint16(buf[0])
	return n
}

func readUint32(buf []byte) uint32 {
	var n uint32
	n = uint32(buf[3]) << 24
	n |= uint32(buf[2]) << 16
	n |= uint32(buf[1]) << 8
	n |= uint32(buf[0])
	return n
}

func readUint64(buf []byte) uint64 {
	var n uint64
	n = uint64(buf[7]) << 56
	n |= uint64(buf[6]) << 48
	n |= uint64(buf[5]) << 40
	n |= uint64(buf[4]) << 32
	n |= uint64(buf[3]) << 24
	n |= uint64(buf[2]) << 16
	n |= uint64(buf[1]) << 8
	n |= uint64(buf[0])
	return n
}

func decodeBool(val *uint16, data []byte, sf *SprotoField, v reflect.Value) error {
	b := true
	if *val == 0 {
		b = false
	}
	if v.Kind() == reflect.Ptr {
		v.Addr().Elem().Set(reflect.ValueOf(&b))
	} else {
		v.SetBool(b)
	}
	return nil
}

func decodeInt(val *uint16, data []byte, sf *SprotoField, v reflect.Value) error {
	var n uint64
	if val != nil {
		n = uint64(*val)
	} else {
		switch len(data) {
		case 0:
			n = 0
		case 4:
			n = uint64(readUint32(data))
		case 8:
			n = readUint64(data)
		default:
			return fmt.Errorf("sproto: malformed integer data for field %s", sf.Name)
		}
	}
	if v.Type().Kind() == reflect.Ptr {
		e := v.Type().Elem()
		v.Addr().Elem().Set(reflect.New(e))
		switch e.Kind() {
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
			switch len(data) {
			case 4:
				v.Elem().SetInt(int64(int32(n)))
			case 8:
				v.Elem().SetInt(int64(n))
			case 0:
				fallthrough
			default:
				v.Elem().SetInt(int64(int16(n)))
			}
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint:
			v.Elem().SetUint(n)
		}
	} else {
		// 初始化默认值
		v.SetInt(0)
		switch v.Kind() {
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
			switch len(data) {
			case 4:
				v.SetInt(int64(int32(n)))
			case 8:
				v.SetInt(int64(n))
			case 0:
				fallthrough
			default:
				v.SetInt(int64(int16(n)))
			}
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint:
			v.SetUint(n)
		}
	}
	return nil
}

func decodeString(val *uint16, data []byte, sf *SprotoField, v reflect.Value) error {
	str := string(data)
	if v.Kind() == reflect.Ptr {
		*v.Addr().Interface().(**string) = &str
	} else {
		v.SetString(str)
	}

	return nil
}

func decodeBytes(val *uint16, data []byte, sf *SprotoField, v reflect.Value) error {
	buf := make([]byte, len(data))
	copy(buf, data)
	v.Set(reflect.ValueOf(buf))
	return nil
}

func decodeBoolSlice(val *uint16, data []byte, sf *SprotoField, v reflect.Value) error {
	vals := make([]bool, len(data))
	for i, v := range data {
		if v == 0 {
			vals[i] = false
		} else {
			vals[i] = true
		}
	}
	v.Set(reflect.ValueOf(vals))
	return nil
}

func decodeIntSlice(val *uint16, data []byte, sf *SprotoField, v reflect.Value) error {
	dataLen := len(data)
	if dataLen < 1 {
		return ErrDecode
	}
	intLen := int(data[0])
	if (dataLen-1)%intLen != 0 {
		return fmt.Errorf("sproto: malformed integer data for field %s", sf.Name)
	}
	sz := (dataLen - 1) / intLen
	vals := reflect.MakeSlice(v.Type(), sz, sz)
	data = data[1:]
	var n uint64
	for i := 0; i < sz; i++ {
		if intLen == 4 {
			n = uint64(readUint32(data[i*intLen:]))
		} else {
			n = readUint64(data[i*intLen:])
		}

		val := vals.Index(i)
		switch e := v.Type().Elem(); e.Kind() {
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
			val.SetInt(int64(n))
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint:
			val.SetUint(n)
		}
	}
	v.Set(vals)
	return nil
}

func decodeStringSlice(val *uint16, data []byte, sf *SprotoField, v reflect.Value) error {
	vals := make([]string, 0, 16)
	for len(data) > 0 {
		expected, val, err := readChunk(data)
		if err != nil {
			return err
		}
		vals = append(vals, string(val))
		data = data[expected:]
	}
	v.Set(reflect.ValueOf(vals))
	return nil
}

func decodeStruct(val *uint16, data []byte, sf *SprotoField, v reflect.Value) error {
	// v1: pointer to struct
	v1 := reflect.New(v.Type().Elem())
	used, err := decodeMessage(data, sf.st, v1)
	if err != nil {
		return err
	}
	if used != len(data) {
		return fmt.Errorf("sproto: malformed struct data for field %s", sf.Name)
	}
	v.Addr().Elem().Set(v1)
	return nil
}

func decodeStructSlice(val *uint16, data []byte, sf *SprotoField, v reflect.Value) error {
	vals := reflect.MakeSlice(v.Type(), 0, 16)
	for len(data) > 0 {
		expected, buf, err := readChunk(data)
		if err != nil {
			return err
		}

		// v1: pointer to struct
		v1 := reflect.New(v.Type().Elem().Elem())
		used, err := decodeMessage(buf, sf.st, v1)
		if err != nil {
			return err
		}
		if used != len(buf) {
			return fmt.Errorf("sproto: malformed struct data for field %s", sf.Name)
		}
		vals = reflect.Append(vals, v1)
		data = data[expected:]
	}
	v.Set(vals)
	return nil
}

func decodeHeader(chunk []byte) (int, []Tag, error) {
	if len(chunk) < 2 {
		return 0, nil, ErrDecode
	}
	fn := int(readUint16(chunk))
	expected := 2 + fn*2
	if len(chunk) < expected {
		return 0, nil, ErrDecode
	}
	tags := make([]Tag, fn)
	n := 0
	var tag uint16 = 0
	for i := 0; i < fn; i++ {
		v := readUint16(chunk[(i+1)*2:])
		if v%2 != 0 { //skip tag
			tag += (v + 1) / 2
			continue
		}
		var val *uint16
		if v != 0 { // value
			v1 := v/2 - 1
			val = &v1
		}
		tags[n] = Tag{
			Tag: tag,
			Val: val,
		}
		tag++
		n++
	}
	return expected, tags[:n], nil
}

func decodeMessage(chunk []byte, st *SprotoType, v reflect.Value) (int, error) {
	var total int
	var tags []Tag
	var err error
	if total, tags, err = decodeHeader(chunk); err != nil {
		return 0, err
	}

	for _, tag := range tags {
		var used int
		var data []byte
		if tag.Val == nil {
			if used, data, err = readChunk(chunk[total:]); err != nil {
				return 0, err
			}
			total += used
		}
		sf := st.FieldByTag(int(tag.Tag))
		if sf == nil {
			fmt.Fprintf(os.Stderr, "sproto<%s>: unknown tag %d\n", st.Name, tag.Tag)
			continue
		}
		v1 := v.Elem().FieldByIndex(sf.index)
		if err = sf.dec(tag.Val, data, sf, v1); err != nil {
			return 0, err
		}
	}
	return total, nil
}

func Decode(data []byte, sp interface{}) (int, error) {
	t, v, err := getbase(sp)
	if err != nil {
		return 0, err
	}
	// clear sp
	v.Elem().Set(reflect.Zero(t.Elem()))
	if len(data) == 0 {
		return 0, nil
	}
	st, err := GetSprotoType(t.Elem())
	if err != nil {
		return 0, err
	}
	return decodeMessage(data, st, v)
}

func MustDecode(data []byte, sp interface{}) int {
	n, err := Decode(data, sp)
	if err != nil {
		panic(err)
	}
	return n
}
