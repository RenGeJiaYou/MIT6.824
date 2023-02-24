package mr

import (
	"log"
	"sync"
)
import "net"
import "os"
import "net/rpc"
import "net/http"

type MapTaskState struct {
	beginTime int64
	workID    int
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

	curWorkerId int // 当前 worker id

	// Map Part
	// Map 任务有且只有三种状态：未执行(unIssuedMapTasks 维护)、正在执行(issuedMapTasks 维护)，执行完毕(前两者同时为空)
	unIssuedMapTask *BlockQueue // 一个队列，维护所有未运行的 map 任务，运行但超时的 map 任务也将重新放回这里
	issuedMapTask   *MapSet     // 一个 map，维护所有正在运行的 map 任务，运行完后移出。
	issuedMutex     sync.Mutex  // 多线程竞态需加锁

	mapTasks    []MapTaskState
	reduceTasks []ReduceTaskState

	// state
	mapDone bool
	allDone bool
}

// Your code here -- RPC handlers for the worker to call.

//
// an example RPC handler.
//
// the RPC argument and reply types are defined in rpc.go.
//
func (c *Coordinator) Example(args *ExampleArgs, reply *ExampleReply) error {
	reply.Y = args.X + 1
	return nil
}

//
// start a thread that listens for RPCs from worker.go
//
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
//
func (c *Coordinator) Done() bool {
	ret := false

	// Your code here.

	return ret
}

// MakeCoordinator
// create a Coordinator.
// main/mrcoordinator.go calls this function.
// nReduce is the number of reduce tasks to use.
//
func MakeCoordinator(files []string, nReduce int) *Coordinator {
	c := Coordinator{}

	// Your code here.

	c.server()
	return &c
}
