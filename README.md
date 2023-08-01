
# MIT 6.824
The Source Code of MIT 6.824

本仓库包括了学习 MIT 6.824 分布式系统的源码及其它相关配置。准备每完成一个Lab ，都在 `readme.md` 创建相关的一章，记录项目结构和代码逻辑。

---
# Lab 1
### Introduction
Lab1 的任务是构建一个 MapReduce 系统。要实现一个 worker 进程来调用 Map and Reduce functions 并负责读取数据和写出结果,和一个 coordinator（论文中称为 master） 进程来分配任务给 workers 并应付 workers 处理失败时的情况。编写的程序应当和  [[MapReduce：Simplified Data Processing on Large Clusters]]  中的相同。
![任务详情](https://github.com/RenGeJiaYou/MIT6.824/assets/37719543/e90aefb8-6647-4af1-a867-8fec308b27eb)


### 结构设计
大致的函数调用关系如图：
![code tree](https://github.com/RenGeJiaYou/MIT6.824/assets/37719543/3f49681b-3ff4-47cf-a0f8-82547a426dc2)

要设计 Worker 和 Master 的结构，要先考虑好它们各有什么功能。

#### Worker
1. ~~一个标识字段。完成状态检测（工作、闲置、任务失败等）。
	1. ❌理解错误，任务的状态管理应该由 Master 完成
2. map 及 reduce 的具体执行函数。这两者均来自于命令行调用主程序时导入的`.so` 插件。
2. 一个PRC 函数。完成 RPC 请求。
3. 一个排序函数。执行 Reduce Work 时使用。 完成中间值的排序，并将同一 key 的一组键值对集供给 Reduce 函数进行计数操作。
4. 对于 Map Workers :
	1. 一个 `pg-*.txt` 文件就算一个分割，只用一个 Map 任务解决。
	2. Map Workers 数量和 Map 任务数量不是一个东西。Map Workers 数量表示启动了多少个线程，应该≤txt文件数；而第 `m`个`txt`文件，对应的第`m`个`map`任务，将产生`nReduce`个文件。
	3. 若共有 `m` 个 map 和 `n` 个 reduce ,则需要产生 `m × n`个临时中间文件。其中，第 i 个 map 传给第 j 个 reduce 的临时文件称为"mr-i-j"，第 j 个 reduce 任务读取所有名称为"mr- * -j"的文件。map 任务通过自定义的 j = Hash（kv.key） 函数决定应该将当前键值对传给 第 j 个键值对。



#### Master

1. 状态管理。
	1. 维护两个任务队列，`unIssuedTask` 表示未执行、`issuedTasks`表示正在执行、前两者同时为空表示执行完毕
2. 一个 RPC 接收函数。接收来自 Worker 的 RPC 请求，供给
	(1) input data 以完成 map 任务；
	(2) intermidiate data 以完成 reduce 任务。

3. 一个字段 srcFileName，保存源文件名的集合，当执行 Map 的 Workers 请求时，从集合中提供一个并标记为处理中（可用 enum 标识几种状态）。处理成功标识为已使用，处理失败标识为未使用。

4. 一个字段 immediateValue ,可设计成 N×M 的 bucket。M 个 Map Workers 可以按列存入数据。Map 阶段完成后整体排序，然后 N 个Reduce 任务开始执行。

### 一些关键问题
编写的程序分为 Master 和 Worker 两类，一次 MapRedece 任务中，Master 只有一个，而 Workers 有若干个。

划分的任务数（无论是 map 任务还是 reduce 任务）应当远多于 workers 的进程数，否则 *人多活少* 会造成严重资源浪费。一个 worker 在处理任务时位于「工作」状态，处理完任务时处于「闲置」状态。worker 是闲不住的，处于「闲置」状态时将会通过 RPC 向 Master 请求任务. 

Worker 可以选择执行 map 任务或者 reduce 任务。在 WordCount 这个例子中，map 的具体任务是：读取输入的文本，将每个单词生成一个键值对，值为"1"。
reduce 的具体任务是：将排序好的键值对数组，键相同的合并为一个键值对，计算长度。
这里的问题是：
1. 要分配多少个 Workers 执行 map 和 reduce ？
	输入数据会分成 M 个分段。M应当    ≥ 执行 Map 任务的 Worker  。
	使用一个 Hash 函数（key % R）将中间数据划分出 R 个分区。R 应当 > 执行 Reduce 任务的 Worker 。
	处理完一个任务的 Worker 将会不知疲倦地执行更多任务，因此 Worker 没有必要太多，那会造成冗员。

2. map 产生的中间值存储在内存还是硬盘上？
	内存，但定期保存到硬盘上。

3. 排序工作由谁来完成？
	Reduce 进程,完成排序后，对于遇到的每个惟一中间键，它将键和相应的中间值集传递给用户的 reduce 函数（即wc.so 中的 Reduce(key string, values []string)）。

4. Map 任务输出的中间数据如何传递给 Reduce ？
	中间数据会写到文件“mr-i-j”中，其地址会被传给 Master , 然后 master 通知 reduce 进程这些位置。

5. Map 任务有多个 Workers 并行；Reduce 任务也是多个 Workers 并行。Map 和Reduce 间能并行运行吗?
	只能**串行**：最后一个 Map Worker 完成全部任务后，第一个 Reduce Worker 才能开始。因为若中间结果不完整，Reduce Worker 的排序也不会正确，进而影响到 Reduce 后的值。

6. 中间值如何划分、保存？
	在 Word Count 任务中，将整体中间值分给 10 个 Reduce Workers，可以按英文字母分隔：每个 Reduce Worker 负责两个字母，其中 6 个再多分配一个字母。
	另外，给出的顺序实现中，中间值是直接缓存在内存中的；但是分布式实现要求中间值要输出到文件中，且这些文件所处的目录也是稍后 Reduce 输入的目录。

7. 超时检测如何设计？
	首先，「超时检测」这个任务的实质是将 `issued[Map|Reduce]Tasks` 中每一个超时的任务放回 `unIssued[Map|Reduce]Tasks` 中。
	本次实现中，`issued[Map|Reduce]Tasks`的数据类型为 `*MapSet` 指针对象。
	在初始化 Coordinator 实例时，为 `issued[Map|Reduce]Tasks` 赋予一个唯一的 MapSet 实例。这样做的目的是可以**直接**维护 issued Task 。
	```go
	type MapSet struct {  
		mapbool map[interface{}]bool  
		count int  
	}
	```

8. Map 的具体操作是怎样的
	「单词计数」任务中，如果源文本中“tree”出现了 10 次，则意味着某个中间文件中有 10 行 “tree,1”记录 。同一个单词的多个记录一定在**同一个中间文件**内。

9. Map 任务和 Reduce 任务的实质？
	有 m 个源文件，就有 m 个 Map 任务；自定义有  n 个 Reduce 任务。
	
	实际创建的 Worker goroutine 可以有任意个，既可以执行 map 也可以执行 reduce。但只有所有的 map 任务完成后，workers 才会转而执行reduce 任务（由 worker 的成员变量`mapOrReduce:bool`控制 ）。、
	
	第 i 个 map 任务 将第 i 个源文件 map 出若干行记录，存到 nRdeuce 个 “mr-i-\*”文件中。一条记录类似于 `{"Key":"tree","Value:"1"}`
	
	第 j 个 reduce 任务 规约 m 个 “mr-\*-j”文件为一个"mr-out-j" 文件

8. `Go Plugin` 在调试程序时的奇怪问题。
   最终的解决方案是在调试时直接调用`mapf`、`reducef`两个函数。在运行 `test-mr.sh` 前再重新采用插件模式。详见我的这篇博客：![Debugging 时无法获取Go plugin(.so文件）的内存地址](https://juejin.cn/post/7219084778347642938)
 



### 一些背景知识
#### - 关于 RPC 调用  
当 `Client` 向 `Server` 发送一个 RPC 请求后，实际的函数仍在 **Server 进程** 中执行,执行结果已在入参 *reply 指向的内存地址中处理完.

#### - 关于 reduce 任务的具体数据结构
共 `NReduce` 个任务，第`[j]`个任务实际是规约所有的 `mr-*-j` 临时中间文件

#### - 关于中间文件  
从 worker 的源码 ```go
func ihash(key string) int``` 可知:  
不管出现多少次,**同名单词**经过 hash 一定会被放在 [0,NReduce) 的**同个 reduce 任务**中。  
换言之，`mr-*-j` 出现的每一个单词，都不会在`mr-*-not j` 出现
