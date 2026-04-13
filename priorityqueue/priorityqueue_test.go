package priorityqueue

import (
	"container/heap"
	"testing"

	"github.com/stretchr/testify/assert"
)

type Item struct {
	score int64
	index int
	data  string
}

func NewItem(data string, score int64) *Item {
	return &Item{score: score, index: -1, data: data}
}
func (t *Item) GetIndex() int      { return t.index }
func (t *Item) SetIndex(i int)     { t.index = i }
func (t *Item) GetPriority() int64 { return t.score }
func (t *Item) GetValue() string   { return t.data }

func TestPQ(t *testing.T) {
	t.Parallel()
	pq := &PriorityQueue[string]{}
	heap.Init(pq)
	heap.Push(pq, NewItem("6", 6))
	heap.Push(pq, NewItem("2", 2))
	heap.Push(pq, NewItem("4", 4))

	assert.Equal(t, heap.Pop(pq).(*Item).GetValue(), "2")
	assert.Equal(t, heap.Pop(pq).(*Item).GetValue(), "4")
	assert.Equal(t, heap.Pop(pq).(*Item).GetValue(), "6")
}
