package priorityqueue

type PQItem[T any] interface {
	GetPriority() int64
	GetIndex() int
	SetIndex(int)
	GetValue() T
}

// A PriorityQueue implements heap.Interface
type PriorityQueue[T any] []PQItem[T]

func (pq PriorityQueue[T]) Len() int { return len(pq) }

func (pq PriorityQueue[T]) Less(i, j int) bool {
	return pq[i].GetPriority() < pq[j].GetPriority()
}

func (pq PriorityQueue[T]) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].SetIndex(i)
	pq[j].SetIndex(j)
}

func (pq *PriorityQueue[T]) Push(x any) {
	item, ok := x.(PQItem[T])
	if !ok {
		panic("priorityqueue: item does not implement PQItem")
	}
	pq.PushType(item)
}

func (pq *PriorityQueue[T]) PushType(x PQItem[T]) {
	n := len(*pq)
	item := x
	item.SetIndex(n)
	*pq = append(*pq, item)
}

func (pq *PriorityQueue[T]) Pop() any {
	return pq.PopType()
}

func (pq *PriorityQueue[T]) PopType() PQItem[T] {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil    // don't stop the GC from reclaiming the item eventually
	item.SetIndex(-1) // for safety
	*pq = old[0 : n-1]
	return item
}

func (pq *PriorityQueue[T]) Peek() PQItem[T] {
	old := *pq
	n := len(old)
	return old[n-1]
}

// Extra function to allow removal of items based on matching criteria
func (pq *PriorityQueue[T]) DeleteMatching(match func(PQItem[T]) bool) {
	keep := []PQItem[T]{}
	for pq.Len() > 0 {
		item := pq.PopType()
		if !match(item) {
			keep = append(keep, item)
		}
	}
	for _, v := range keep {
		pq.Push(v)
	}
}
