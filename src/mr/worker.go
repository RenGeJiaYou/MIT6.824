package mr

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)
import "log"
import "net/rpc"
import "hash/fnv"

// KeyValue
// Map functions return a slice of KeyValue.
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

	workerID int
}

// logPrintf 辅助函数，打印 log
func (w *Aworker) logPrintf(format string, vars ...interface{}) {
	log.Printf("worker %d: "+format, w.workerID, vars)
}

// ======================================= ↓ Map Task Part ↓ =======================================
// makeIntermediateFromFile 调用插件里的 Map 函数，完成底层的 Map 操作
func makeIntermediateFromFile(filename string, mapf func(string, string) []KeyValue) []KeyValue {
	// 获取文件名 filename
	file, err := os.Open(filename)
	if err != nil {
		log.Fatalf("cannot open %v", filename) // 在文件读取期就出错属于 Fatal 级别错误，不能用成员函数 logPrintf() ,自然也不用写接收器
	}
	defer file.Close()

	// 根据文件名获取文件内容，存在一个大 string 里 content
	content, err := io.ReadAll(file) // 回头研究 File 类型是否实现了 Reader 接口
	if err != nil {
		log.Fatalf("can't read %v", filename)
	}

	// 调用 mapf,结果返回出去
	kva := mapf(filename, string(content))
	return kva
}

// writeToFiles 负责将 makeIntermediateFromFile() 生成的键值对保存到Linux 内的 var/tmp
// 第 i 个 map 任务传给第 j 个 reduce 的临时文件称为"mr-i-j",第 j 个 Reduce 任务读取所有的 "mr-*-j" 文件
// 在执行第 i 个 map 任务时会生成一个 fileID ，并调用本函数传入实参
func (worker *Aworker) writeToFiles(fileID int, nReduce int, intermediate []KeyValue) {
	// 根据 Hash 函数分割 intermediate
	kvas := make([][]KeyValue, nReduce)
	for i := 0; i < nReduce; i++ {
		kvas[i] = make([]KeyValue, 0)
	}
	for _, kv := range intermediate {
		index := keyReduceIndex(kv.Key, nReduce)
		kvas[index] = append(kvas[index], kv)
	}

	// 将每个分割块(Go Struct)转换为 JSON 文本保存到临时文件
	for i := 0; i < nReduce; i++ {
		tempfile, err := os.CreateTemp(".", "mrtemp")
		if err != nil {
			log.Fatal(err)
		}

		enc := json.NewEncoder(tempfile)
		for _, kv := range kvas[i] {
			err := enc.Encode(&kv) // 注意取值操作
			if err != nil {
				log.Fatal(err)
			}
		}

		oname := fmt.Sprintf("mr-%v-%v", fileID, i)
		err = os.Rename(tempfile.Name(), oname)
		if err != nil {
			worker.logPrintf("rename tempfile failed for %v\n", oname)
		}
	}
}

// 执行 RPC 调用，向 master 请求任务
func (worker *Aworker) askMapTask() *MapTaskReply {
	args := MapTaskArgs{}
	args.workerID = worker.workerID
	reply := MapTaskReply{}

	worker.logPrintf("requesting for a map task...\n")

	// "Coordinator.GiveMapTask" 表示调用 Coordinator 对象实例下的 GiveMapTask()
	call("Coordinator.GiveMapTask", &args, &reply)

	worker.workerID = reply.workerID

	if reply.fileID == -1 {
		// 说明没有剩余任务了
		if reply.allDone == true {
			worker.logPrintf("no more map task,switch to reduce mode\n")
			return nil
		} else {
			return &reply
		}
	}
	worker.logPrintf("got map task on file %v %v\n", reply.fileID, reply.fileName)

	return &reply // 因为变量逃逸机制，允许返回局部变量的指针
}

func (worker *Aworker) joinMapTask(fileID int) {

}

// ======================================= ↑ Map Task Part ↑ =======================================

// Worker
// main/mrworker.go calls this function.
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

// send an RPC request to the coordinator, wait for the response.
// usually returns true.
// returns false if something goes wrong.
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
