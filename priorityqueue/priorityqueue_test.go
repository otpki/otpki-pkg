package priorityqueue

import (
	"container/heap"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	item2, ok := heap.Pop(pq).(*Item)
	require.True(t, ok)
	assert.Equal(t, item2.GetValue(), "2")
	item4, ok := heap.Pop(pq).(*Item)
	require.True(t, ok)
	assert.Equal(t, item4.GetValue(), "4")
	item6, ok := heap.Pop(pq).(*Item)
	require.True(t, ok)
	assert.Equal(t, item6.GetValue(), "6")
}
