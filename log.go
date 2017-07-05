//
//   date  : 2014-06-07
//   author: xjdrew
//

package main

import (
	"io"
	"log"
	"os"
	"runtime"
)

var logger *log.Logger
var logLevel int

func init() {
	//logger = log.New(io.Writer(os.Stderr), "", log.Ldate | log.Lmicroseconds | log.Lshortfile)
	logger = log.New(io.Writer(os.Stderr), "", log.Ldate|log.Lmicroseconds)
}

func _print(format string, a ...interface{}) {
	logger.Printf(format, a...)
}

func Debug(format string, a ...interface{}) {
	if logLevel > 2 {
		_print(format, a...)
	}
}

func Info(format string, a ...interface{}) {
	if logLevel > 1 {
		_print(format, a...)
	}
}

func Error(format string, a ...interface{}) {
	if logLevel > 0 {
		_print(format, a...)
	}
}

func Panic(format string, a ...interface{}) {
	_print(format, a...)
	panic("!!")
}

func Log(format string, a ...interface{}) {
	_print(format, a...)
}

func LogCurStack(format string, a ...interface{}) {
	_print(format, a...)
	buf := make([]byte, 8192)
	runtime.Stack(buf, false)
	_print("!!!!!stack!!!!!: %s", buf)
}

func Recover() {
	if err := recover(); err != nil {
		LogCurStack("goroutine failed:%v", err)
	}
}
