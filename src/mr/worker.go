package mr

import (
	"fmt"
	"os"
)
import "log"
import "net/rpc"
import "hash/fnv"

// 我的编码

// KeyValue
// Map functions return a slice of KeyValue.
//
type KeyValue struct {
	Key   string
	Value string
}

// keyReduceIndex 调用 ihash(key) % NReduce
// 根据当前 Map 任务产生的键值对，计算出对应的目标 reduce 任务的序号
func ihash(key string) int {
	h := fnv.New32a()                  // fnv 算法让高度相似的字符串也能均匀分散在 map
	h.Write([]byte(key))               // New32a() 返回一个 uint32 的指针对象，Write()根据传入的 key 修改 h(所指内存地址) 的 Hash 结果值
	return int(h.Sum32() & 0x7fffffff) // Sum32() 强制将 h 转回 uint32
}
func keyReduceIndex(key string, nReduce int) int {
	return ihash(key) % nReduce
}

// Aworker 表示一个 worker 实例
type Aworker struct {
	mapf    func(filename string, contents string) []KeyValue
	reducef func(key string, values []string) string

	// false on map
	// true on reduce
	mapOrReduce bool

	// map 任务和 reduce 任务全部完成后，才会被改为 true
	allDone bool

	workerID uint8
}

// logPrintf 辅助函数，打印 log
func (w *Aworker) logPrintf(format string, vars ...interface{}) {
	log.Printf("worker %d: "+format, w.workerID, vars)
}

// ======================================= ↓ Map Task Part ↓ =======================================
// 调用插件里的 Map 函数，完成底层的 Map 操作
func makeIntermediateFromFile(filename string, mapf func(string, string) []KeyValue) []KeyValue {
	// 获取文件名 filename
	file, err := os.Open(filename)
	if err != nil {
		log.Printf(filename)
	}
	defer file.Close()

	// 根据文件名获取文件内容，存在一个大 string 里 content
	var content []byte
	_, err = file.Read(content)
	if err != nil {
		log.Println(filename + "content can't read")
	}
	// 调用 mapf,结果返回出去
	kva := mapf(filename, string(content))
	return kva

}

// ======================================= ↑ Map Task Part ↑ =======================================

// Worker
// main/mrworker.go calls this function.
//
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {

	// Your worker implementation here.

	// uncomment to send the Example RPC to the coordinator.
	// CallExample()

}

// CallExample
// example function to show how to make an RPC call to the coordinator.
//
// the RPC argument and reply types are defined in rpc.go.
//
func CallExample() {

	// declare an argument structure.
	args := ExampleArgs{}

	// fill in the argument(s).
	args.X = 99

	// declare a reply structure.
	reply := ExampleReply{}

	// send the RPC request, wait for the reply.
	// the "Coordinator.Example" tells the
	// receiving server that we'd like to call
	// the Example() method of struct Coordinator.
	ok := call("Coordinator.Example", &args, &reply)
	if ok {
		// reply.Y should be 100.
		fmt.Printf("reply.Y %v\n", reply.Y)
	} else {
		fmt.Printf("call failed!\n")
	}
}

//
// send an RPC request to the coordinator, wait for the response.
// usually returns true.
// returns false if something goes wrong.
//
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := coordinatorSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		log.Fatal("dialing:", err)
	}
	defer c.Close()

	err = c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}
