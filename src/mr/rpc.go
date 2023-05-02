package mr

//
// RPC definitions.
//
// remember to capitalize all names.
//

import "os"
import "strconv"

//
// example to show how to declare the arguments
// and reply for an RPC.
//

type ExampleArgs struct {
	X int
}

type ExampleReply struct {
	Y int
}

// Add your RPC definitions here.

// 创建 UDS(UNIX-domain socket) 连接，和"127.0.0.1"一样，也是本机网络 IO 通信方式，但性能好很多。因为不是基于TCP/IP，而是 unix 的文件系统
// 相关资料1：https://juejin.cn/post/7075509542687080456
// 相关资料2：https://zhuanlan.zhihu.com/p/423856852
// Cook up a unique-ish UNIX-domain socket name
// in /var/tmp, for the coordinator.
// Can't use the current directory since
// Athena AFS doesn't support UNIX-domain sockets.
func coordinatorSock() string {
	s := "/var/tmp/824-mr-" // 这是 Linux 的临时文件夹
	s += strconv.Itoa(os.Getuid())
	return s
}
