package mr

import (
	"errors"
	"sync"
)

// 定义 unIssued[Map/Reduce]Task 的数据结构，日后将用 Go 原生的 channel 代替
// 实际存入的 data 是 int 类型的 0 ~ len(文件)-1 ，表示每一个任务

//（Go 的 sync.Cond 明确提出大多数场景下 channels 比 Cond 更好
// Broadcast 等同于关闭一个 channel,Signal 等同于发信息给一个 channel）

// listNode 定义底层链表结点
type listNode struct {
	data interface{}
	next *listNode
	prev *listNode
}

func (node *listNode) addBefore(data interface{}) {
	newNode := listNode{}
	prev := node.prev
	newNode.data = data

	newNode.next = node
	node.prev = &newNode

	prev.next = &newNode
	newNode.prev = prev
}

func (node *listNode) addAfter(data interface{}) {
	newNode := listNode{}
	next := node.next
	newNode.data = data

	node.next = &newNode
	newNode.prev = node

	newNode.next = next
	next.prev = &newNode
}

func (node *listNode) removeBefore() {
	prev := node.prev.prev

	node.prev = prev
	prev.next = node

	//按 C++ 写，此处应有 delete prev; 但 Go 有 GC 机制，所以免了
}

func (node *listNode) removeAfter() {
	next := node.next.next
	node.next = next
	next.prev = node
}

// linkedList 定义底层的链表
type linkedList struct {
	head  listNode
	count int
}

func (list *linkedList) pushFront(data interface{}) {
	list.head.addAfter(data)
	list.count++
}

func (list *linkedList) pushBack(data interface{}) {
	list.head.addBefore(data)
	list.count++
}

func (list *linkedList) getFront() (interface{}, error) {
	if list.count == 0 {
		return nil, errors.New("list was empty")
	}
	return list.head.next.data, nil
}

func (list *linkedList) getBack() (interface{}, error) {
	if list.count == 0 {
		return nil, errors.New("list was empty")
	}
	return list.head.prev.data, nil
}

func (list *linkedList) popFront() (interface{}, error) {
	if list.count == 0 {
		return nil, errors.New("list was empty")
	}
	data := list.head.next.data
	list.head.removeAfter()
	list.count--
	return data, nil
}

func (list *linkedList) popBack() (interface{}, error) {
	if list.count == 0 {
		return nil, errors.New("list was empty")
	}
	data := list.head.prev.data
	list.head.removeBefore()
	list.count--
	return data, nil
}

func (list *linkedList) size() int {
	return list.count
}

// linkedList 链表初始化
func newLinkedList() *linkedList {
	list := linkedList{}

	list.count = 0
	list.head.next = &list.head
	list.head.prev = &list.head

	return &list
}

// BlockQueue 定义队列容器，由一个 LinkedList 和一个 sync.Cond 条件变量组成
type BlockQueue struct {
	list *linkedList
	cond *sync.Cond
}

// NewBlockQueue 队列容器初始化
func NewBlockQueue() *BlockQueue {
	queue := BlockQueue{}

	queue.list = newLinkedList()
	queue.cond = sync.NewCond(new(sync.Mutex))

	return &queue
}

// BlockQueue 的几个操作函数实际调用了 LinkedList 的对应函数，只不过在调用代码前后加了 Cond 锁

func (queue *BlockQueue) PutFront(data interface{}) {
	queue.cond.L.Lock()
	queue.list.pushFront(data)
	queue.cond.L.Unlock()
	queue.cond.Broadcast()
}

func (queue *BlockQueue) PutBack(data interface{}) {
	queue.cond.L.Lock()
	queue.list.pushBack(data)
	queue.cond.L.Unlock()
	queue.cond.Broadcast()
}

func (queue *BlockQueue) GetFront() (interface{}, error) {
	queue.cond.L.Lock()
	for queue.list.count == 0 {
		queue.cond.Wait() // Wait()的推荐用法，目的是让本线程等待，直到队列有新增元素时（如上两个Put()）再被 Broadcast() 唤醒
	}
	data, err := queue.list.getFront()
	queue.cond.L.Unlock()

	return data, err
}

func (queue *BlockQueue) GetBack() (interface{}, error) {
	queue.cond.L.Lock()
	for queue.list.count == 0 {
		queue.cond.Wait()
	}
	data, err := queue.list.getBack()
	queue.cond.L.Unlock()

	return data, err
}

func (queue *BlockQueue) PopFront() (interface{}, error) {
	queue.cond.L.Lock()
	data, err := queue.list.popFront()
	queue.cond.L.Unlock()

	return data, err
}

func (queue *BlockQueue) PopBack() (interface{}, error) {
	queue.cond.L.Lock()
	data, err := queue.list.popBack()
	queue.cond.L.Unlock()

	return data, err
}

func (queue *BlockQueue) Size() int {
	queue.cond.L.Lock()
	ret := queue.list.count
	queue.cond.L.Unlock()
	return ret
}
