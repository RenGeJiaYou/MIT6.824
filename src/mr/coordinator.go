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
	// todo fileID 字段似乎是不必要的
	fileID int
}

type ReduceTaskState struct {
	beginTime int64
	workerID  int
	fileID    int // todo  非必需的字段
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
	issuedMapMutex  sync.Mutex  // 多线程竞态需加锁.todo:可重命名，因为该锁不只针对issued 队列

	unIssuedReduceTask *BlockQueue
	issuedReduceTask   *MapSet
	issuedReduceMutex  sync.Mutex

	// record which worker performed which task at which time
	mapTasks    []MapTaskState
	reduceTasks []ReduceTaskState

	// state
	mapDone bool
	allDone bool
}

// Your code here -- RPC handlers for the worker to call.

// ======================================= ↓ Map Task Part ↓ =======================================

type MapTaskArgs struct {
	// -1 if does not have one
	WorkerID int
}

type MapTaskReply struct {
	// worker 将调用 os.Open(fileName) 打开文件
	FileName string

	// worker 将用于临时文件命名、并后续 RPC 给 master 来标明维护任务队列的具体元素
	FileID int

	// worker 将用于临时文件命名
	NReduce int

	// 实际来自 MapTaskArgs
	WorkerID int

	// map 全部完成
	AllDone bool
}

// mapDoneProcess 改变 reply 一些字段，好让 worker 的 RPC 接收函数 AskMapTask() 意识到 Map 任务已全部完成
func mapDoneProcess(reply *MapTaskReply) {
	log.Println("all map tasks complete, telling workers to switch to reduce mode")
	reply.FileID = -1
	reply.AllDone = true
}

// GiveMapTask 主要是将 unIssuedTask 全部处理完
func (c *Coordinator) GiveMapTask(args *MapTaskArgs, reply *MapTaskReply) error {
	// 为第一次请求任务的 Worker 分配一个 ID
	if args.WorkerID == -1 {
		reply.WorkerID = c.curWorkerId
		c.curWorkerId++
	} else {
		reply.WorkerID = args.WorkerID
	}
	log.Printf("worker %v asks for a map task", reply.WorkerID)

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
	log.Printf("%v unIssued map tasks,%v issued map tasks at hand\n", c.unIssuedMapTask.Size(), c.issuedMapTask.Size())
	c.issuedMapMutex.Unlock()

	curTime := getNowTimeSecond()
	var fileID int
	ret, err := c.unIssuedMapTask.PopBack() // ※ 从 unIssuedMapTasks 队列中取出一个任务
	if err != nil {
		log.Printf("已无 map 任务，Worker 等待")
		fileID = -1
	} else {
		// todo:重构点1：如果全部完成后运行无误，考虑删去 else{}
		fileID = ret.(int)
		log.Printf("任务 [%v , %s] 从unIssuedMapTask -> issuedMapMutex", fileID, c.filename[fileID])

		c.issuedMapMutex.Lock()
		reply.FileName = c.filename[fileID]
		c.mapTasks[fileID].beginTime = curTime // todo: 重构点2，直接在此调用函数
		c.mapTasks[fileID].workerID = reply.WorkerID
		c.issuedMapTask.Insert(fileID) // ※ 将任务l unIssuedMapTask -> issuedMapTasks 中
		c.issuedMapMutex.Unlock()

		log.Printf("giving map task %v on file %v at second %v\n", fileID, reply.FileName, curTime)
	}

	reply.FileID = fileID // reply 中的 FileID 要么用 -1 表示已无 map 任务，要么用 0~len(文件集合)表示当前的一个 map 任务
	reply.AllDone = false
	reply.NReduce = c.nReduce

	return nil
}

func (c *Coordinator) prepareAllReduceTasks() {
	for i := 0; i < c.nReduce; i++ {
		log.Printf("\033[1;32;40m prepareAllReduceTasks():putting %vth reduce task into channel \033[0m\n", i)
		// 共 NReduce 个任务，第[j]个任务实际是规约所有的 mr-*-j 临时文件
		c.unIssuedReduceTask.PutBack(i)
	}
}

// MapTaskJoinArgs 的属性名注意首字母大写，否则无法被 gob 解析
type MapTaskJoinArgs struct {
	FileID   int
	WorkerID int
}

type MapTaskJoinReply struct {
	Accept bool
}

func getNowTimeSecond() int64 {
	return time.Now().Unix()
}

// JoinMapTask 主要是将 issuedTask 全部处理完。并将信息(「哪个任务」在「什么时间」由「哪个 worker」申请处理)记录到 mapTasks
func (c *Coordinator) JoinMapTask(args *MapTaskJoinArgs, reply *MapTaskJoinReply) error {
	log.Printf("\033[1;36;40m got join request form worker %v on file %v %v \033[0m\n",
		args.WorkerID,
		args.FileID,
		c.mapTasks[args.FileID])

	c.issuedMapMutex.Lock()

	// 意外情况一：当前任务不存在于 issuedMapTask 队列里
	if !c.issuedMapTask.Has(args.FileID) {
		log.Println("意外情况：当前 map task 不存在于 issuedMapTask 队列里")
		reply.Accept = false
		c.issuedMapMutex.Unlock()
		return nil
	}

	// 意外情况二：当前 worker 请求的是其它 worker 的任务（其它 worker 正在执行且尚未超时）
	if c.mapTasks[args.FileID].workerID != args.WorkerID {
		log.Printf("当前 map task 属于 worker %v ,而不是 worker %v",
			c.mapTasks[args.FileID].workerID,
			args.WorkerID)
		reply.Accept = false
		c.issuedMapMutex.Unlock()
		return nil
	}

	// 意外情况三：worker 执行任务超时
	/*
		master 进程先把 fileID 从unIssuedMapTask -> issuedMapTask 后，一个 worker 进程才能获取 该 fileID 并处理它。
		worker 的处理有可能成功也有可能失败（输出的临时文件就是不完整或错误的），
		但即使失败了，这个 worker 进程崩溃了，它之前拿到的那个 fileID 仍然在issuedMapTask 里面。
		需要一种机制把超时的 issuedMapTask -> unIssuedMapTask。
	*/
	curTime := getNowTimeSecond()
	taskTime := c.mapTasks[args.FileID].beginTime
	if curTime-taskTime > maxTaskTime {
		log.Println("任务超时，该 map task 将重新放回 unIssuedMapTasks 队列")
		reply.Accept = false
		c.unIssuedMapTask.PutFront(args.FileID) // 任务超时，将该 map 任务重新放回 unIssuedMapTasks 队列
		// 无需相应地清空 c.mapTasks[fileID].放回到 unIssuedMapTask 后会在下一个 worker 进程请求时重新赋值
	} else {
		log.Println("map task 已在规定的最大限时内完成")
		reply.Accept = true
		c.issuedMapTask.Remove(args.FileID) // ※ 核心语句：任务已完成，从 issuedMapTasks 取出该项
	}

	c.issuedMapMutex.Unlock()
	return nil
}

// ======================================= ↑ Map Task Part ↑ =======================================

// ======================================= ↓ Reduce Task Part ↓ =======================================

type ReduceTaskArgs struct {
	WorkerID int
}

type ReduceTaskReply struct {
	RIndex    int
	NReduce   int
	FileCount int
	AllDone   bool
}

type ReduceJoinArgs struct {
	RIndex   int
	WorkerID int
}

type ReduceJoinReply struct {
	Accept bool
}

// GiveReduceTask 维护 unIssuedReduceTasks :处理 reduce 请求,
func (c *Coordinator) GiveReduceTask(args *ReduceTaskArgs, reply *ReduceTaskReply) error {
	log.Printf("\033[1;34;40m worker [%v] 请求 Reduce 任务  \0330m\n", args.WorkerID)

	c.issuedReduceMutex.Lock()

	// 终止条件：unIssued 队列和 issued 队列都清空，说明全部任务完成
	if c.unIssuedReduceTask.Size() == 0 && c.issuedReduceTask.Size() == 0 {
		log.Printf("\033[1;31;40m  Reduce tasks all done!   \033[0m\n")
		c.issuedReduceMutex.Unlock()
		c.allDone = true
		reply.RIndex = -1
		reply.AllDone = true
		return nil
	}

	log.Printf("unissued reduce tasks: %v\nissued reduce tasks: %v\n\n",
		c.unIssuedReduceTask.Size(),
		c.issuedReduceTask.Size())

	c.issuedReduceMutex.Unlock() // release lock to allow unissued update

	curTime := getNowTimeSecond()
	ret, err := c.unIssuedReduceTask.PopBack()
	var rindex int
	if err != nil {
		log.Printf("no more reduce tasks,let worker wait...")
		rindex = -1
	} else {
		rindex = ret.(int)

		c.issuedReduceMutex.Lock()
		c.reduceTasks[rindex].beginTime = curTime
		c.reduceTasks[rindex].workerID = args.WorkerID
		c.issuedReduceTask.Insert(rindex)
		c.issuedReduceMutex.Unlock()

		log.Printf("giving reduce task %v at second %v\n", rindex, curTime)
	}

	reply.RIndex = rindex
	reply.AllDone = false
	reply.NReduce = c.nReduce
	reply.FileCount = len(c.filename) // todo  非必需的字段

	return nil
}

// JoinReduceTask maintaining issuedReduceTask Queue after worker finish actual reduce work
func (c *Coordinator) JoinReduceTask(args *ReduceJoinArgs, reply *ReduceJoinReply) error {
	log.Printf("\033[1;36;40m got join request form worker %v on Reduce index %v %v \033[0m\n",
		args.WorkerID,
		args.RIndex,
		c.reduceTasks[args.RIndex])

	c.issuedReduceMutex.Lock()

	// exist determine
	if !c.issuedReduceTask.Has(args.RIndex) {
		log.Println("task abandoned or dose not exists in issuedReduceTask")
		c.issuedReduceMutex.Unlock()
		return nil
	}

	// worker-job mismatch determine
	if args.WorkerID != c.reduceTasks[args.RIndex].workerID {
		log.Printf("\033[1;36;40m reduce task belongs to worker %v , not this %v \033[0m\n",
			c.reduceTasks[args.RIndex].workerID,
			args.WorkerID,
		)
		c.issuedReduceMutex.Unlock()
		reply.Accept = false
		return nil
	}

	// overtime determine
	curTime := getNowTimeSecond()
	beginTime := c.reduceTasks[args.RIndex].beginTime
	if curTime-beginTime > maxTaskTime {
		log.Println("reduce task overtime")
		reply.Accept = false
		c.unIssuedReduceTask.PutFront(args.RIndex)
	} else {
		// here,the over-timed task should be taken out of the issuedTask Queue
		// will completed by a special timeout detection goroutine
		log.Println("reduce task accepting")
		reply.Accept = true
		c.issuedReduceTask.Remove(args.RIndex)
	}
	c.issuedReduceMutex.Unlock()
	return nil
}

// ======================================= ↑ Reduce Task Part ↑ =======================================

// ======================================= ↓ Timeout Detect Part ↓ ====================================
// 'm' mean issuedMapTask
func (m *MapSet) removeTimeoutMapTasks(mapTasks []MapTaskState, unIssuedMapTasks *BlockQueue) {
	// A Coordinator instance maintaining only one copy of issued(Map/Reduce)Task
	// complete timeout task via issuedTask (type:MapSet) directly
	for fileID, issued := range m.bmap {
		curTime := getNowTimeSecond()
		if issued {
			if curTime-mapTasks[fileID.(int)].beginTime > maxTaskTime {
				log.Printf("worker %v on file %v abandoned due to timeout\n",
					mapTasks[fileID.(int)].workerID,
					fileID)
				m.bmap[fileID.(int)] = false
				m.count--
				unIssuedMapTasks.PutFront(fileID.(int))
			}
		}
	}
}

// 'm' mean issuedReduceTask
func (m *MapSet) removeTimeoutReduceTasks(reduceTasks []ReduceTaskState, unIssuedReduceTasks *BlockQueue) {
	// A Coordinator instance maintaining only one copy of issued(Map/Reduce)Task
	// complete timeout task via issuedTask (type:MapSet) directly
	for fileID, issued := range m.bmap {
		curTime := getNowTimeSecond()
		if issued {
			if curTime-reduceTasks[fileID.(int)].beginTime > maxTaskTime {
				log.Printf("worker %v on file %v abandoned due to timeout\n",
					reduceTasks[fileID.(int)].workerID,
					fileID)
				m.bmap[fileID.(int)] = false
				m.count--
				unIssuedReduceTasks.PutFront(fileID.(int))
			}
		}
	}
}

func (c *Coordinator) removeTimeoutTasks() {
	log.Println("removing timeout maptasks...")
	c.issuedMapMutex.Lock()
	c.issuedMapTask.removeTimeoutMapTasks(c.mapTasks, c.unIssuedMapTask)
	c.issuedMapMutex.Unlock()

	c.issuedReduceMutex.Lock()
	c.issuedReduceTask.removeTimeoutReduceTasks(c.reduceTasks, c.unIssuedReduceTask)
	c.issuedReduceMutex.Unlock()
}

func (c *Coordinator) loopRemoveTimeoutTasks() {
	for {
		time.Sleep(2 * time.Second)
		c.removeTimeoutTasks()
	}
}

// ======================================= ↑ Timeout Detect Part ↑ ====================================

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

// Done 负责检查字段 c.allDone
// main/mrcoordinator.go calls Done() periodically to find out
// if the entire job has finished.
func (c *Coordinator) Done() bool {
	// Your code here.
	if c.allDone {
		log.Println("已经全部完成")
	} else {
		log.Println("尚未完成")

	}
	return c.allDone
}

// MakeCoordinator
// create a Coordinator.
// main/mrcoordinator.go calls this function.
// NReduce is the number of reduce tasks to use.
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

	c.unIssuedReduceTask = NewBlockQueue()
	c.issuedReduceTask = NewMapSet()

	c.allDone = false
	c.mapDone = false

	log.SetPrefix("coordinator: ")
	log.Println("coordinator was initialized")
	log.Println("files[0]:", files[0])

	c.server()

	log.Printf("Coordinator{} Object was registered, rpc listening start")

	go c.loopRemoveTimeoutTasks()
	// unIssuedMapTask init
	// send to channel after everything else initializes
	log.Printf("file count %d \n", len(files))
	for i := 0; i < len(files); i++ {
		log.Printf("sending %vth file map task to channel", i)
		c.unIssuedMapTask.PutBack(i)
	}

	return &c
}
