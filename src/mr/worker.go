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

// logPrintf 辅助函数，打印当前workerID 及 log
func (w *Aworker) logPrintf(format string, vars ...interface{}) {
	log.Printf("worker %d: "+format, w.workerID, vars)
}

// ======================================= ↓ Map Task Part ↓ =======================================

// askMapTask 执行 RPC 调用，向 master 请求任务
func (w *Aworker) askMapTask() *MapTaskReply {
	args := MapTaskArgs{}
	args.WorkerID = w.workerID
	reply := MapTaskReply{}

	w.logPrintf("requesting for a map task...\n")

	// ※ "Coordinator.GiveMapTask" 表示调用 Coordinator 对象实例下的 GiveMapTask()
	call("Coordinator.GiveMapTask", &args, &reply)

	w.workerID = reply.WorkerID

	if reply.FileID == -1 {
		// 没有剩余 map 任务 -> nil
		if reply.AllDone == true {
			w.logPrintf("no more map task,should switch to reduce mode\n")
			return nil
		} else {
			return &reply
		}
	}
	w.logPrintf("got map task on file %v %v\n", reply.FileID, reply.FileName)

	return &reply // 因为变量逃逸机制，允许返回局部变量的指针
}

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
// 在执行第 i 个 map 任务时会生成一个 FileID ，并调用本函数传入实参
func (w *Aworker) writeToFiles(fileID int, nReduce int, intermediate []KeyValue) {
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
			w.logPrintf("rename tempfile failed for %v\n", oname)
		}
	}
}

// joinMapTask RPC 请求 coordinator 中的 joinMapTask
func (w *Aworker) joinMapTask(fileID int) {
	args := MapTaskJoinArgs{}
	args.WorkerID = w.workerID
	args.FileID = fileID

	reply := MapTaskJoinReply{}

	call("Coordinator.JoinMapTask", &args, &reply)

	if reply.Accept {
		w.logPrintf("accepted\n")
	} else {
		w.logPrintf("not accepted\n")
	}
}

func (w *Aworker) executeMap(reply *MapTaskReply) {
	intermediate := makeIntermediateFromFile(reply.FileName, w.mapf)
	w.logPrintf("writing map results to file\n")
	w.writeToFiles(reply.FileID, reply.NReduce, intermediate)
	w.joinMapTask(reply.FileID)
}

// ======================================= ↑ Map Task Part ↑ =======================================

// ======================================= ↓ Reduce Task Part ↓ =======================================
func (w *Aworker) askReduceTask() *ReduceTaskReply {
	args := ReduceTaskArgs{}
	reply := ReduceTaskReply{}

	args.WorkerID = w.workerID

	w.logPrintf("requesting for a map task...")

	call("Coordinator.GiveReduceTask", &args, &reply)

	// 说明 unIssuedReduceTask[与 issuedReduceTask] 再无 reduce 任务可分配.需根据 reply.AllDone 分类讨论
	if reply.RIndex == -1 {
		if reply.AllDone { // ! AllDone 指的是当前全部 Reduce 完成，而不是总体任务完成
			w.logPrintf("no more reduce tasks, try to terminate worker\n")
			return nil
		} else {
			return &reply
		}
	}

	return &reply
}

// 完成具体 reduce 工作
func (w *Aworker) executeReduce(reply *ReduceTaskReply) {
	// 把所有 mr-*-j 中的 []KeyValue 连缀为一个大 []KeyValue
	intermediate := make([]KeyValue, 0)
	for i := 0; i < reply.FileCount; i++ {
		w.logPrintf("generating intermediates on cluster %v\n in reduce task[%v]", i, reply.RIndex)
		intermediate = append(intermediate, w.readIntermediates(i, reply.RIndex)...)
	}

	outname := fmt.Sprintf("mr-out-%v", reply.RIndex)
	temp, err := os.CreateTemp(".", "mr-temp-*")
	if err != nil {
		w.logPrintf("cannot create tempfile for %v\n", outname)
	}
	reduceAllSlices(intermediate, w.reducef, temp)
	temp.Close()
	err = os.Rename(temp.Name(), outname)
	if err != nil {
		w.logPrintf("rename tempfile failed for %v\n", outname)
	}

	w.joinReduceTask(reply.RIndex)
}

func (w *Aworker) readIntermediates(fileID int, reduceID int) []KeyValue {
	// 1/2: open file
	filename := fmt.Sprintf("mr-%v-%v", fileID, reduceID)
	file, err := os.Open(filename)
	if err != nil {
		w.logPrintf("cannot open temp file: mr-%v-%v", fileID, reduceID)
	}
	defer file.Close()

	// 2/2: decode every record to an Object & append into an array
	kva := make([]KeyValue, 0)
	dec := json.NewDecoder(file)
	for {
		var kv KeyValue
		if err := dec.Decode(&kv); err == io.EOF {
			break
		} else if err != nil {
			log.Fatal(err)
		}
		kva = append(kva, kv)
	}

	return kva
}

// 规约 intermediate 数组。
// todo 不过 intermediate []KeyValue 是以传值调用的实参，内存开销大。看看有没有办法改为传指针
func reduceAllSlices(intermediate []KeyValue, reducef func(key string, values []string) string, temp *os.File) {
	// 把intermediate reduce 为一个map [string] int
	// 得到了单词的 set 及对应的数量,放入 mr-temp-j

	result := make(map[string]int, 0)
	for _, v := range intermediate {
		result[v.Key] += 1
	}

}

func (w *Aworker) joinReduceTask(index int) {

}

// ======================================= ↑ Reduce Task Part ↑ =======================================

// process() 的任务是根据 mapOrReduce 的状态选择让 worker 执行 Map 或 Reduce.
// mapOrReduce 的初始值在 Worker() 给定为 false,当 RPC 响应无结果时将切换到另一种状态.
// 获知任务信息后（存于变量 reply）,将传递给对应的 execute() 具体完成执行任务。
func (w *Aworker) process() {
	if !w.mapOrReduce { //todo 重构点3 更改为 true 进入 map;false 进入 reduce.比较符合变量名的直觉
		// process map task
		// todo 重构点4 分支语句可简化
		reply := w.askMapTask()
		if reply == nil {
			w.mapOrReduce = true // 切换到 reduce 模式
		} else {
			if reply.FileID == -1 {
				// -1 表示目前没有 map 任务。
				// 但要考虑到正在执行 map 的其它 worker 可能因为超时而将任务重新放回 unIssuedMapTask 队列。
				// 因此不可切换 reduce 模式
			} else {
				w.executeMap(reply)
			}
		}
	}

	if w.mapOrReduce {
		// process reduce task
		reply := w.askReduceTask()
		if reply == nil {
			// worker will finish
			w.allDone = true
		} else {
			w.executeReduce(reply)
		}
	}
}

// Worker
// main/mrworker.go calls this function.
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {

	// 初始化一个 worker
	worker := Aworker{}
	worker.mapf = mapf
	worker.reducef = reducef
	worker.mapOrReduce = false
	worker.allDone = false
	worker.workerID = -1 // 表示一个 worker 的初始状态。随后在与 master 的通信中确定自己的唯一工号，即 0 ~ len(任务)-1 其一

	worker.logPrintf("initialized!\n")

	// main process
	for !worker.allDone {
		worker.process()
	}
	worker.logPrintf("no more tasks, all done, exiting...\n")

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
	sockname := coordinatorSock() // todo maybe existed race problem
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
