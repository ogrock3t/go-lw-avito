package positions

import (
	"container/heap"
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

type enqueueResult string

const (
	resultQueued    enqueueResult = "queued"
	resultDropped   enqueueResult = "dropped"
	resultDuplicate enqueueResult = "duplicate"
	resultCanceled  enqueueResult = "canceled"
)

type activeTask struct {
	id     uuid.UUID
	cancel context.CancelFunc
}

type Queue struct {
	mu          sync.Mutex
	tasks       taskHeap
	capacity    int
	available   chan struct{}
	activeTasks map[uuid.UUID]activeTask

	canceledDuringScan map[uuid.UUID]struct{}
}

func NewQueue(capacity int) (*Queue, error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("queue capacity must be positive, got %d", capacity)
	}

	return &Queue{
		tasks:       make(taskHeap, 0, capacity),
		capacity:    capacity,
		available:   make(chan struct{}, 1),
		activeTasks: make(map[uuid.UUID]activeTask),
	}, nil
}

func (q *Queue) beginScan() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.canceledDuringScan = make(map[uuid.UUID]struct{})
}

func (q *Queue) endScan() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.canceledDuringScan = nil
}

func (q *Queue) tryEnqueue(newTask task) (enqueueResult, *task) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if _, canceled := q.canceledDuringScan[newTask.tripID]; canceled {
		return resultCanceled, nil
	}

	if _, exists := q.activeTasks[newTask.tripID]; exists {
		return resultDuplicate, nil
	}

	var evicted *task

	if len(q.tasks) == q.capacity {
		newestIndex := 0
		for i := 1; i < len(q.tasks); i++ {
			if q.tasks[newestIndex].lastActivityAt.Before(q.tasks[i].lastActivityAt) {
				newestIndex = i
			}
		}

		newest := q.tasks[newestIndex]
		if !newTask.lastActivityAt.Before(newest.lastActivityAt) {
			return resultDropped, nil
		}

		removed := heap.Remove(&q.tasks, newestIndex).(task)
		delete(q.activeTasks, removed.tripID)
		evicted = &removed
	}

	newTask.id = uuid.New()

	heap.Push(&q.tasks, newTask)
	q.activeTasks[newTask.tripID] = activeTask{
		id: newTask.id,
	}
	q.notify()

	return resultQueued, evicted
}

func (q *Queue) tryDequeue() (task, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.tasks) == 0 {
		return task{}, false
	}

	job := heap.Pop(&q.tasks).(task)

	if len(q.tasks) > 0 {
		q.notify()
	}

	return job, true
}

func (q *Queue) dequeue(ctx context.Context) (task, bool) {
	for {
		if ctx.Err() != nil {
			return task{}, false
		}

		if job, ok := q.tryDequeue(); ok {
			return job, true
		}

		select {
		case <-ctx.Done():
			return task{}, false
		case <-q.available:
		}
	}
}

func (q *Queue) notify() {
	select {
	case q.available <- struct{}{}:
	default:
	}
}

func (q *Queue) complete(job task) {
	q.mu.Lock()
	defer q.mu.Unlock()

	current, exists := q.activeTasks[job.tripID]
	if exists && current.id == job.id {
		delete(q.activeTasks, job.tripID)
	}
}

func (q *Queue) start(job task, cancel context.CancelFunc) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	current, exists := q.activeTasks[job.tripID]
	if !exists || current.id != job.id {
		return false
	}

	current.cancel = cancel
	q.activeTasks[job.tripID] = current

	return true
}

func (q *Queue) CancelTrip(tripID uuid.UUID) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.canceledDuringScan != nil {
		q.canceledDuringScan[tripID] = struct{}{}
	}

	current, exists := q.activeTasks[tripID]
	if !exists {
		return
	}

	if current.cancel != nil {
		current.cancel()
		return
	}

	for i := range q.tasks {
		if q.tasks[i].id == current.id {
			heap.Remove(&q.tasks, i)
			break
		}
	}

	delete(q.activeTasks, tripID)
}
