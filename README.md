# mit 6-824
The Source Code of MIT 6.824

本仓库包括了学习 MIT 6.824 分布式系统的源码及其它相关配置。准备每完成一个Lab ，都创建相关的 `readme.md`，记录项目结构和代码逻辑。


#### - 关于 RPC 调用  
当 `Client` 向 `Server` 发送一个 RPC 请求后，实际的函数仍在 **Server 进程** 中执行,执行结果已在入参 *reply 指向的内存地址中处理完.

#### - 关于 reduce 任务的具体数据结构
共 `NReduce` 个任务，第`[j]`个任务实际是规约所有的 `mr-*-j` 临时中间文件

#### - 关于中间文件  
从 worker 的源码 ```go
func ihash(key string) int``` 可知:  
不管出现多少次,**同名单词**经过 hash 一定会被放在 [0,NReduce) 的**同个 reduce 任务**中。  
换言之，`mr-*-j` 出现的每一个单词，都不会在`mr-*-not j` 出现
