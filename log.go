//
//   date  : 2014-06-07
//   author: xjdrew
//

package main

import (
	"fmt"
	"log"
	"log/syslog"
	"runtime"
	"time"
)

var syslog_w *syslog.Writer
var logLevel int
var log_last_tsec int64
var log_last_tstr string

func _gen_logstr(format string, a ...interface{}) string {
	ts := time.Now().Unix()
	if(ts != log_last_tsec) {
		log_last_tsec = ts
		log_last_tstr = fmt.Sprintf(time.Unix(ts, 0).Format("[2006-01-02 15:04:05] "))
	}
	return log_last_tstr + fmt.Sprintf(format, a...)
}

func _print(s string) {
	fmt.Println(s)
}

func init() {
	_syslog, err := syslog.New(syslog.LOG_INFO, "goscon")
	if err != nil {
		log.Fatal(err)
	}
	syslog_w = _syslog
	log_last_tsec = 0
}

func Debug(format string, a ...interface{}) {
	if logLevel > 2 {
		s := _gen_logstr(format, a ...)
		_print(s)
		syslog_w.Debug(s)
	}
}

func Info(format string, a ...interface{}) {
	if logLevel > 1 {
		s := _gen_logstr(format, a ...)
		_print(s)
		syslog_w.Info(s)
	}
}

func Error(format string, a ...interface{}) {
	if logLevel > 0 {
		s := _gen_logstr(format, a ...)
		_print(s)
		syslog_w.Err(s)
	}
}

func Panic(format string, a ...interface{}) {
	s := _gen_logstr(format, a ...)
	_print(s)
	syslog_w.Crit(s)
	panic("!!")
}

func Log(format string, a ...interface{}) {
	s := _gen_logstr(format, a ...)
	_print(s)
	syslog_w.Info(s)
}

func LogCurStack(format string, a ...interface{}) {
	s := _gen_logstr(format, a ...)
	_print(s)
	syslog_w.Info(s)
	
	buf := make([]byte, 8192)
	runtime.Stack(buf, false)

	s = fmt.Sprintf("!!!!!stack!!!!!: %s", buf)
	_print(s)
	syslog_w.Info(s)
}

func Recover() {
	if err := recover(); err != nil {
		LogCurStack("goroutine failed:%v", err)
	}
}
