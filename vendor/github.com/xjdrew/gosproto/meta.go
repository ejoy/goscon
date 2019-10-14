package sproto

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	WireVarintName  = "integer" // int, uint, int8, uint8, int16, uint16, int32, uint32, int64, uint64
	WireBooleanName = "boolean" // bool
	WireBytesName   = "string"  // string, []byte
	WireStructName  = "struct"  // struct
)

var (
	mutex sync.Mutex
	stMap = make(map[reflect.Type]*SprotoType)
)

type headerEncoder func(st *SprotoField, v reflect.Value) (uint16, bool)
type encoder func(st *SprotoField, v reflect.Value) []byte
type decoder func(val *uint16, data []byte, st *SprotoField, v reflect.Value) error

type SprotoField struct {
	Name     string
	OrigName string
	Wire     string
	Tag      int
	Array    bool

	st *SprotoType // for struct types only

	index     []int // index sequence for Value.FieldByIndex
	headerEnc headerEncoder
	enc       encoder
	dec       decoder
}

// parse filed meta information
func (sf *SprotoField) parse(s string) error {
	// children,object,3,array
	fields := strings.Split(s, ",")
	if len(fields) < 2 {
		return fmt.Errorf("sproto: parse(%s) tag must have 2 or more fields", s)
	}
	sf.Wire = fields[0]
	switch sf.Wire {
	case WireVarintName, WireBooleanName, WireBytesName, WireStructName:
	default:
		return fmt.Errorf("sproto: parse(%s) unknown wire type: %s", s, sf.Wire)
	}

	var err error
	sf.Tag, err = strconv.Atoi(fields[1])
	if err != nil {
		return fmt.Errorf("sproto: parse(%s) parse tag failed: %s", s, err)
	}

	for i := 2; i < len(fields); i++ {
		f := fields[i]
		switch {
		case f == "array":
			sf.Array = true
		case strings.HasPrefix(f, "name="):
			sf.OrigName = f[len("name="):]
		}
	}
	return nil
}

func (sf *SprotoField) assertWire(expectedWire string, expectedArray bool) error {
	if sf.Wire != expectedWire {
		return fmt.Errorf("sproto: field(%s) expect %s but get %s", sf.Name, expectedWire, sf.Wire)
	}
	if sf.Array != expectedArray {
		n := "not"
		if expectedArray {
			n = ""
		}
		return fmt.Errorf("sproto: field(%s) should %s be array", sf.Name, n)
	}
	return nil
}

func (sf *SprotoField) setEncAndDec(f *reflect.StructField) error {
	var stype reflect.Type
	var err error
	t1 := f.Type
	if t1.Kind() == reflect.Ptr {
		t1 = t1.Elem()
	}

	switch t1.Kind() {
	case reflect.Bool:
		sf.headerEnc = headerEncodeBool
		sf.dec = decodeBool
		err = sf.assertWire(WireBooleanName, false)
	case reflect.Int8, reflect.Uint8, reflect.Int16, reflect.Uint16,
		reflect.Int32, reflect.Uint32, reflect.Int64, reflect.Uint64,
		reflect.Int, reflect.Uint:
		sf.headerEnc = headerEncodeInt
		sf.enc = encodeInt
		sf.dec = decodeInt
		err = sf.assertWire(WireVarintName, false)
	case reflect.String:
		sf.headerEnc = headerEncodeDefault
		sf.enc = encodeString
		sf.dec = decodeString
		sf.assertWire(WireBytesName, false)
	case reflect.Struct:
		stype = t1
		sf.headerEnc = headerEncodeDefault
		sf.enc = encodeStruct
		sf.dec = decodeStruct
		err = sf.assertWire(WireStructName, false)
	case reflect.Slice:
		switch t2 := t1.Elem(); t2.Kind() {
		case reflect.Bool:
			sf.headerEnc = headerEncodeDefault
			sf.enc = encodeBoolSlice
			sf.dec = decodeBoolSlice
			err = sf.assertWire(WireBooleanName, true)
		case reflect.Uint8:
			sf.headerEnc = headerEncodeDefault
			if sf.Wire == WireBytesName {
				sf.enc = encodeBytes
				sf.dec = decodeBytes
				err = sf.assertWire(WireBytesName, false)
			} else {
				sf.enc = encodeIntSlice
				sf.dec = decodeIntSlice
				err = sf.assertWire(WireVarintName, true)
			}
		case reflect.Int8, reflect.Int16, reflect.Uint16,
			reflect.Int32, reflect.Uint32, reflect.Int64, reflect.Uint64,
			reflect.Int, reflect.Uint:
			sf.headerEnc = headerEncodeDefault
			sf.enc = encodeIntSlice
			sf.dec = decodeIntSlice
			err = sf.assertWire(WireVarintName, true)
		case reflect.String:
			sf.headerEnc = headerEncodeDefault
			sf.enc = encodeStringSlice
			sf.dec = decodeStringSlice
			sf.assertWire(WireBytesName, true)
		case reflect.Ptr:
			switch t3 := t2.Elem(); t3.Kind() {
			case reflect.Struct:
				stype = t2.Elem()
				sf.headerEnc = headerEncodeDefault
				sf.enc = encodeStructSlice
				sf.dec = decodeStructSlice
				err = sf.assertWire(WireStructName, true)
			default:
				err = fmt.Errorf("sproto: field(%s) no coders for %s -> %s -> %s", sf.Name, t1.Kind().String(), t2.Kind().String(), t3.Kind().String())
			}
		default:
			err = fmt.Errorf("sproto: field(%s) no coders for %s -> %s", sf.Name, t1.Kind().String(), t2.Kind().String())
		}
	default:
		err = fmt.Errorf("sproto: field(%s) no coders for %s", sf.Name, t1.Kind().String())
	}

	if err != nil {
		return err
	}

	if stype != nil {
		if sf.st, err = getSprotoTypeLocked(stype); err != nil {
			return err
		}
	}
	return nil
}

func (sf *SprotoField) init(f *reflect.StructField) error {
	sf.Name = f.Name
	sf.OrigName = f.Name

	tagString := f.Tag.Get("sproto")
	if tagString == "" {
		sf.Tag = -1
		return nil
	}

	sf.index = f.Index
	if err := sf.parse(tagString); err != nil {
		return err
	}
	if err := sf.setEncAndDec(f); err != nil {
		return err
	}
	return nil
}

type SprotoType struct {
	Name   string // struct name
	Fields []*SprotoField
	tagMap map[int]int // tag -> fileds index
	order  []int       // list of struct field numbers in tag order
}

func (st *SprotoType) Len() int { return len(st.order) }
func (st *SprotoType) Less(i, j int) bool {
	return st.Fields[st.order[i]].Tag < st.Fields[st.order[j]].Tag
}
func (st *SprotoType) Swap(i, j int) {
	st.order[i], st.order[j] = st.order[j], st.order[i]
}

func (st *SprotoType) FieldByTag(tag int) *SprotoField {
	if index, ok := st.tagMap[tag]; ok {
		return st.Fields[index]
	}
	return nil
}

func GetSprotoType(t reflect.Type) (*SprotoType, error) {
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("sproto: type must have kind struct")
	}
	mutex.Lock()
	sp, err := getSprotoTypeLocked(t)
	mutex.Unlock()
	return sp, err
}

func getSprotoTypeLocked(t reflect.Type) (*SprotoType, error) {
	if st, ok := stMap[t]; ok {
		return st, nil
	}

	st := new(SprotoType)
	stMap[t] = st

	st.Name = t.Name()
	numField := t.NumField()
	st.Fields = make([]*SprotoField, numField)
	st.order = make([]int, numField)
	st.tagMap = make(map[int]int)

	for i := 0; i < numField; i++ {
		sf := new(SprotoField)
		f := t.Field(i)
		if err := sf.init(&f); err != nil {
			delete(stMap, t)
			return nil, err
		}

		st.Fields[i] = sf
		st.order[i] = i
		if sf.Tag >= 0 {
			// check repeated tag
			if _, ok := st.tagMap[sf.Tag]; ok {
				return nil, fmt.Errorf("sproto: field(%s.%s) tag repeated", st.Name, sf.Name)
			}
			st.tagMap[sf.Tag] = i
		}
	}

	// Re-order prop.order
	sort.Sort(st)
	return st, nil
}

// Get the type and value of a pointer to a struct from interface{}
func getbase(sp interface{}) (t reflect.Type, v reflect.Value, err error) {
	if sp == nil {
		err = ErrNil
		return
	}

	t = reflect.TypeOf(sp)
	if t.Kind() != reflect.Ptr {
		err = ErrNonPtr
		return
	}

	if t.Elem().Kind() != reflect.Struct {
		err = ErrNonStruct
		return
	}

	v = reflect.ValueOf(sp)
	if v.IsNil() {
		err = ErrNil
		return
	}

	return
}
