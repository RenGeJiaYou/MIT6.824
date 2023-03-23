package mr

import (
	"log"
	"sync"
	"time"
)
import "net"
import "os"
import "net/rpc"
import "net/http"

const maxTaskTime = 10 // seconds

// MapTaskState 的结构说明了：哪个 worker(workerId) 在什么时候(beginSecond)处理哪个文件(fileId)
type MapTaskState struct {
	beginTime int64
	workerID  int
	fileID    int
}

type ReduceTaskState struct {
	beginTime int64
	workID    int
	fileID    int
}

type Coordinator struct {
	// Your definitions here.
	filename []string // 数据文件的文件名集合
	nReduce  int      // Reduce 进程的数量

	curWorkerId int // 当前 worker id.由于 Master 将只有一个实例，curWorkerId 实际是作为一个静态变量，为每个 worker 实例标记一个工号

	// Map 任务有且只有三种状态：未执行(unIssuedMapTasks 维护)、正在执行(issuedMapTasks 维护)，执行完毕(前两者同时为空)
	// 实际用 0 ~ nReduce-1 (int 类型)表示一个任务
	unIssuedMapTask *BlockQueue // 一个队列，维护所有未运行的 map 任务，运行但超时的 map 任务也将重新放回这里
	issuedMapTask   *MapSet     // 一个 map，维护所有正在运行的 map 任务，运行完后移出。
	issuedMapMutex  sync.Mutex  // 多线程竞态需加锁

	unIssuedReduceTask *BlockQueue
	issuedReduceTask   *MapSet
	issuedReduceMutex  sync.Mutex

	mapTasks    []MapTaskState
	reduceTasks []ReduceTaskState

	// state
	mapDone bool
	allDone bool
}

// Your code here -- RPC handlers for the worker to call.

type MapTaskArgs struct {
	// -1 if does not have one
	workerID int
}

type MapTaskReply struct {
	// worker 将调用 os.Open(fileName) 打开文件
	fileName string

	// worker 将用于临时文件命名、并后续 RPC 给 master 来标明维护任务队列的具体元素
	fileID int

	// worker 将用于临时文件命名
	nReduce int

	// 实际来自 MapTaskArgs
	workerID int

	// map 全部完成
	allDone bool
}

// mapDoneProcess 改变一些字段，好让检测这些字段的其他函数意识到 Map 任务已全部完成
func mapDoneProcess(reply *MapTaskReply) {
	log.Println("all map tasks complete, telling workers to switch to reduce mode")
	reply.fileID = -1
	reply.allDone = true
}

// GiveMapTask 主要是将 unIssuedTask 全部处理完
func (c *Coordinator) GiveMapTask(args *MapTaskArgs, reply *MapTaskReply) error {
	// 为第一次请求任务的 Worker 分配一个 ID
	if args.workerID == -1 {
		args.workerID = c.curWorkerId
		c.curWorkerId++
	} else {
		reply.workerID = args.workerID
	}
	log.Printf("worker %v asks for a map task", reply.workerID)

	// 互斥地访问 unIssued 和 issued 任务队列
	c.issuedMapMutex.Lock()

	// 若 map 任务显示全部完成
	if c.mapDone {
		c.issuedMapMutex.Unlock()
		mapDoneProcess(reply)
		return nil
	}

	// 若两个 Task 队列为空，说明既没有未提交任务，也没有正在执行的任务。
	if c.unIssuedMapTask.Size() == 0 && c.issuedMapTask.Size() == 0 {
		c.issuedMapMutex.Unlock()
		c.mapDone = true
		c.prepareAllReduceTasks()
		mapDoneProcess(reply)
		return nil
	}
	log.Printf("%v unIssued map tasks,%v issued map tasks at hand\n", c.unIssuedReduceTask.Size(), c.issuedMapTask.Size())
	c.issuedMapMutex.Unlock()

	curTime := getNowTimeSecond()
	ret, err := c.unIssuedMapTask.PopBack() // ※ 从 unIssuedMapTasks 队列中取出一个任务
	var fileID int
	if err != nil {
		log.Printf("已无 map 任务，Worker 等待")
		fileID = -1
	} else {
		// todo:重构点1：如果全部完成后运行无误，考虑删去 else{}
		fileID = ret.(int)
		c.issuedMapMutex.Lock()
		reply.fileName = c.filename[fileID]
		c.mapTasks[fileID].beginTime = curTime // todo: 重构点2，直接在此调用函数
		c.mapTasks[fileID].workerID = reply.workerID
		c.issuedMapTask.Insert(fileID) // ※ 将取出的任务放到 issuedMapTasks 中
		c.issuedMapMutex.Unlock()
		log.Printf("giving map task %v on file %v at second %v\n", fileID, reply.fileName, curTime)
	}

	reply.fileID = fileID // reply 中的 fileID 要么用 -1 表示已无 map 任务，要么用 0~len(文件集合)表示当前的一个 map 任务
	reply.allDone = false
	reply.nReduce = c.nReduce

	return nil
}

func (c *Coordinator) prepareAllReduceTasks() {
	for i := 0; i < c.nReduce; i++ {
		log.Printf("putting %vth reduce task into channel\n", i)
		c.unIssuedReduceTask.PutBack(i)
	}
}

type MapTaskJoinArgs struct {
	fileID   int
	workerID int
}

type MapTaskJoinReply struct {
	accept bool
}

func getNowTimeSecond() int64 {
	return time.Now().Unix()
}

// JoinMapTask 主要是将 issuedTask 全部处理完。并将信息(「哪个 worker」在「什么时间」申请处理「哪个任务」)记录到 mapTasks
func (c *Coordinator) JoinMapTask(args *MapTaskJoinArgs, reply *MapTaskJoinReply) error {
	log.Printf("got join request form worker %v on file %v %v\n", args.workerID, args.fileID, c.mapTasks[args.fileID])

	c.issuedMapMutex.Lock()

	// 意外情况一：当前任务不存在于 issuedMapTask 队列里
	if !c.issuedMapTask.Has(args.fileID) {
		log.Println("task abandoned or does not exists, ignoring...")
		reply.accept = false
		c.issuedMapMutex.Unlock()
		return nil
	}

	// 意外情况二：当前 worker 请求的是其它 worker 的任务
	if c.mapTasks[args.fileID].workerID != args.workerID {
		log.Printf("map task belongs to worker %v not this %v, ignoring...", c.mapTasks[args.fileID].workerID, args.workerID)
		reply.accept = false
		c.issuedMapMutex.Unlock()
		return nil
	}

	// 意外情况三：worker 执行任务超时
	curTime := getNowTimeSecond()
	taskTime := c.mapTasks[args.fileID].beginTime
	if curTime-taskTime > maxTaskTime {
		log.Println("task exceeds max wait time, abadoning... ")
		reply.accept = false
		c.unIssuedMapTask.PutFront(args.fileID) // 任务超时，将该 map 任务重新放回 unIssuedMapTasks 队列
	} else {
		log.Println("task within max wait time, accepting...")
		reply.accept = true
		c.issuedMapTask.Remove(args.fileID) // ※ 核心语句：任务已完成，从 issuedMapTasks 取出该项
	}

	c.issuedMapMutex.Unlock()
	return nil
}

// an example RPC handler.
//
// the RPC argument and reply types are defined in rpc.go.
func (c *Coordinator) Example(args *ExampleArgs, reply *ExampleReply) error {
	reply.Y = args.X + 1
	return nil
}

// start a thread that listens for RPCs from worker.go
func (c *Coordinator) server() {
	rpc.Register(c)
	rpc.HandleHTTP()
	//l, e := net.Listen("tcp", ":1234")
	sockname := coordinatorSock()
	os.Remove(sockname)
	l, e := net.Listen("unix", sockname)
	if e != nil {
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

// Done
// main/mrcoordinator.go calls Done() periodically to find out
// if the entire job has finished.
func (c *Coordinator) Done() bool {
	ret := false

	// Your code here.

	return ret
}

// MakeCoordinator
// create a Coordinator.
// main/mrcoordinator.go calls this function.
// nReduce is the number of reduce tasks to use.
func MakeCoordinator(files []string, nReduce int) *Coordinator {
	c := Coordinator{}

	// Your code here.
	c.filename = files
	c.nReduce = nReduce
	c.curWorkerId = 0 // worker ID 编号从0~len(files)-1 ,和任务数量一一对应

	c.mapTasks = make([]MapTaskState, len(files))
	c.reduceTasks = make([]ReduceTaskState, nReduce)

	c.unIssuedMapTask = NewBlockQueue()
	c.issuedMapTask = NewMapSet()

	c.allDone = false
	c.mapDone = false

	log.SetPrefix("coordinator: ")
	log.Println("coordinator was initialized")


	c.server()
	log.Printf("rpc listening start")

	// unIssuedMapTask init
	// send to channel after everything else initializes
	log.Printf("file count %d \n", len(files))
	for i := 0; i < len(files); i++ {
		log.Printf("sending %vth file map task to channel", i)
		c.unIssuedMapTask.PutBack(i)
	}

	return &c
}
