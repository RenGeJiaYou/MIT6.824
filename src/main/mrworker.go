package main

//
// start a worker process, which is implemented
// in ../mr/worker.go. typically there will be
// multiple worker processes, talking to one coordinator.
//
// go run mrworker.go wc.so
//
// Please do not change this file.
//

import (
	"bytes"
	"mit2/src/mr"
)
import "plugin"
import "os"
import "fmt"
import "log"

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: mrworker xxx.so\n")
		os.Exit(1)
	}

	//mapf := func(filename string, contents string) []mr.KeyValue {
	//	// function to detect word separators.
	//	ff := func(r rune) bool { return !unicode.IsLetter(r) }
	//
	//	// split contents into an array of words.
	//	words := strings.FieldsFunc(contents, ff)
	//
	//	kva := []mr.KeyValue{}
	//	for _, w := range words {
	//		kv := mr.KeyValue{w, "1"}
	//		kva = append(kva, kv)
	//	}
	//	return kva
	//}
	//
	//reducef := func(key string, values []string) string {
	//	// return the number of occurrences of this word.
	//	return strconv.Itoa(len(values))
	//}
	mapf, reducef := loadPlugin(os.Args[1]) // os.Args 保存了调用命令的参数，第[0] 是 "mrworker.go";第[1]个是"wc.so"

	// Temporarily turn off log printing to the terminal
	buf := new(bytes.Buffer)
	log.SetOutput(buf)

	mr.Worker(mapf, reducef)
}

//
// load the application Map and Reduce functions
// from a plugin file, e.g. ../mrapps/wc.so
//

func loadPlugin(filename string) (func(string, string) []mr.KeyValue, func(string, []string) string) {
	p, err := plugin.Open(filename)
	if err != nil {
		log.Printf("cannot load plugin %v,error message：\n%v", filename, err)
	}
	//log.Println("插件的地址为：", p)

	xmapf, err := p.Lookup("Map")
	if err != nil {
		log.Fatalf("cannot find Map in %v", filename)
	}
	mapf := xmapf.(func(string, string) []mr.KeyValue)
	xreducef, err := p.Lookup("Reduce")
	if err != nil {
		log.Fatalf("cannot find Reduce in %v", filename)
	}
	reducef := xreducef.(func(string, []string) string)

	return mapf, reducef
}
