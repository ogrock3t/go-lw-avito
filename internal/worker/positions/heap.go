package positions

type taskHeap []task

func (h taskHeap) Len() int {
	return len(h)
}

func (h taskHeap) Less(i, j int) bool {
	return h[i].lastActivityAt.Before(h[j].lastActivityAt)
}

func (h taskHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *taskHeap) Push(value any) {
	*h = append(*h, value.(task))
}

func (h *taskHeap) Pop() any {
	items := *h
	last := len(items) - 1
	job := items[last]

	items[last] = task{}
	*h = items[:last]

	return job
}
