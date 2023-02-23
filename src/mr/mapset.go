package mr

// 定义 issuedTask 的数据结构
// 由于主要的操作是：查询指定任务是否在 issuedTask (代表正在执行)。使用 Map (而不是 Queue) 将使查询时间复杂度从O(n) -> O(1)

type MapSet struct {
	bmap  map[interface{}]bool
	count int
}

func NewMapSet() *MapSet {
	m := MapSet{}
	m.bmap = make(map[interface{}]bool)
	m.count = 0
	return &m
}

func (m *MapSet) Insert(data interface{}) {
	m.bmap[data] = true
	m.count++
}

func (m *MapSet) Remove(data interface{}) {
	m.bmap[data] = false
	m.count--
}
func (m *MapSet) Has(data interface{}) bool {
	return m.bmap[data]
}

func (m *MapSet) Size() int {
	return m.count
}
