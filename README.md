# mit 6-824
The Source Code of MIT 6.824

本仓库包括了学习 MIT 6.824 分布式系统的源码及其它相关配置。准备每完成一个Lab ，都创建相关的 `readme.md`，记录项目结构和代码逻辑。


关于 RPC 调用  
当 `Client` 向 `Server` 发送一个 RPC 请求后，实际的函数仍在 **Server 进程** 中执行,执行结果已在入参 *reply 指向的内存地址中处理完.
