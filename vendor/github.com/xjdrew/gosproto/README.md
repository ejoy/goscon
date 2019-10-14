# gosproto

[sproto](https://github.com/cloudwu/sproto)'s encoder and decoder in golang.

## type map

sproto type      | golang type
---------------- | -------------------------------------------------
string           | \*string, []byte, string
integer          | \*int8, \*uint8, \*int16, \*uint16, \*int32, \*uint32, \*int64, \*uint64, \*int, \*uint, int8, uint8, int16, uint16, int32, uint32, int64, uint64, int, uint
boolean          | \*bool, bool
object           | \*struct
array of string  | []string
array of integer | []int8, []uint8, []int16, []uint16, []int32, []uint32, []int64, []uint64, []int, []uint
array of boolean | []bool
array of object  | []\*struct

## schema

You can define go struct corresponding to sproto schema directly as examples in all test cases.
Or use [sprotodump](https://github.com/lvzixun/sprotodump) to change sproto schema to go file.

## test

```
go test github.com/xjdrew/gosproto
```

## benchmark

```txt
BenchmarkEncode-8                 500000              2058 ns/op
BenchmarkDecode-8                 500000              3222 ns/op
BenchmarkEncodePacked-8           500000              2523 ns/op
BenchmarkDecodePacked-8           500000              3660 ns/op
PASS
ok      github.com/szyhf/go-sproto      5.864s
```

## 改进说明（improvement）

原来的sproto只支持指针类型的变量，要通过sproto.Int(ptr *int)类似的方法赋值访问，非常麻烦。

> 注：原来这是proto2的标准实现方法，但proto3已经不再使用指针的方案——20170228

这个库增强了Encode和Decode的能力，现在可以直接处理int string bool的值类型。

同时，使用相同位置及配置的值类型及对应指针，在tag一致的情况下，且指针均不为nil的情况下，编码结果是相等的，例如：

``` golang
type WithPtr struct{
	ID *int `sproto:"integer,0,name=ID"`
}

type WithVal struct{
	ID int `sproto:"integer,0,name=ID"`
}

```

如果WithPtr.ID=nil，则使用WithVal对编码进行Decode之后，WithVal.ID会等于0。

> 个人倾向尽可能不要使用nil作为值，作为一个用于跨平台的编码，nil容易在不同平台上产生不同的解析结果，极易产生歧义。

> 同样，个人不建议使用值类型的Struct，所以不支持了。

更多的实现效果请参考encode_test.go中的例子。
