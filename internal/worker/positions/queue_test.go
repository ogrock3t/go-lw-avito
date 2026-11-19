package positions

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestQueue_DequeuesOldestFirst(t *testing.T) {
	queue := newTestQueue(t, 3)
	oldest := newTestTask(30 * time.Minute)
	middle := newTestTask(20 * time.Minute)
	newest := newTestTask(10 * time.Minute)

	for _, job := range []task{middle, newest, oldest} {
		requireQueued(t, queue, job)
	}

	for _, want := range []task{oldest, middle, newest} {
		got, ok := queue.tryDequeue()
		if !ok || got.tripID != want.tripID {
			t.Fatalf("dequeued trip = %s, present = %v, want %s", got.tripID, ok, want.tripID)
		}
		queue.complete(got)
	}
	if _, ok := queue.tryDequeue(); ok {
		t.Error("queue still contains a task after draining")
	}
}

func TestQueue_DeduplicatesQueuedAndProcessingTrips(t *testing.T) {
	queue := newTestQueue(t, 2)
	job := newTestTask(time.Minute)
	requireQueued(t, queue, job)

	if result, _ := queue.tryEnqueue(job); result != resultDuplicate {
		t.Fatalf("queued trip result = %q, want duplicate", result)
	}

	current, ok := queue.tryDequeue()
	if !ok {
		t.Fatal("queued task is missing")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !queue.start(current, cancel) {
		t.Fatal("cannot start dequeued task")
	}
	if result, _ := queue.tryEnqueue(job); result != resultDuplicate {
		t.Fatalf("processing trip result = %q, want duplicate", result)
	}
	if ctx.Err() != nil {
		t.Fatalf("processing context unexpectedly canceled: %v", ctx.Err())
	}

	queue.complete(current)
	requireQueued(t, queue, job)
}

func TestQueue_FullQueueDropsNewerOrEqualTask(t *testing.T) {
	for _, age := range []time.Duration{5 * time.Minute, 10 * time.Minute} {
		t.Run(age.String(), func(t *testing.T) {
			queue := newTestQueue(t, 1)
			existing := newTestTask(10 * time.Minute)
			incoming := newTestTask(age)
			requireQueued(t, queue, existing)

			result, evicted := queue.tryEnqueue(incoming)
			if result != resultDropped || evicted != nil {
				t.Fatalf("enqueue result = %q, evicted = %+v, want dropped without eviction", result, evicted)
			}
			got, ok := queue.tryDequeue()
			if !ok || got.tripID != existing.tripID {
				t.Fatalf("retained task = %+v, present = %v, want existing trip %s", got, ok, existing.tripID)
			}
			queue.complete(got)
			requireQueued(t, queue, incoming)
		})
	}
}

func TestQueue_FullQueueEvictsNewestAndAllowsReenqueue(t *testing.T) {
	queue := newTestQueue(t, 2)
	oldest := newTestTask(30 * time.Minute)
	middle := newTestTask(20 * time.Minute)
	newest := newTestTask(10 * time.Minute)
	requireQueued(t, queue, newest)
	requireQueued(t, queue, middle)

	result, evicted := queue.tryEnqueue(oldest)
	if result != resultQueued || evicted == nil || evicted.tripID != newest.tripID {
		t.Fatalf("enqueue result = %q, evicted = %+v, want queued and trip %s evicted",
			result, evicted, newest.tripID)
	}
	for _, want := range []task{oldest, middle} {
		got, ok := queue.tryDequeue()
		if !ok || got.tripID != want.tripID {
			t.Fatalf("dequeued trip = %s, present = %v, want %s", got.tripID, ok, want.tripID)
		}
		queue.complete(got)
	}
	requireQueued(t, queue, newest)
}

func TestQueue_CancelQueuedTaskFreesCapacity(t *testing.T) {
	queue := newTestQueue(t, 1)
	job := newTestTask(time.Minute)
	requireQueued(t, queue, job)

	queue.CancelTrip(job.tripID)
	queue.CancelTrip(job.tripID)

	if _, ok := queue.tryDequeue(); ok {
		t.Fatal("canceled task remains in the queue")
	}
	requireQueued(t, queue, job)
}

func TestQueue_CancellationDuringScanRejectsLaterEnqueue(t *testing.T) {
	queue := newTestQueue(t, 1)
	job := newTestTask(time.Minute)

	queue.beginScan()
	queue.CancelTrip(job.tripID)
	result, evicted := queue.tryEnqueue(job)
	if result != resultCanceled || evicted != nil {
		t.Fatalf("enqueue result = %q, evicted = %+v, want canceled without eviction", result, evicted)
	}
	if _, ok := queue.tryDequeue(); ok {
		t.Fatal("task canceled during the scan entered the queue")
	}
	queue.endScan()

	queue.beginScan()
	defer queue.endScan()
	requireQueued(t, queue, job)
}

func TestQueue_OldTaskCannotStartOrCompleteNewTask(t *testing.T) {
	queue := newTestQueue(t, 1)
	job := newTestTask(time.Minute)
	requireQueued(t, queue, job)
	oldTask, ok := queue.tryDequeue()
	if !ok {
		t.Fatal("original task is missing")
	}

	queue.CancelTrip(job.tripID)
	requireQueued(t, queue, job)
	newTask, ok := queue.tryDequeue()
	if !ok || newTask.id == oldTask.id {
		t.Fatal("reenqueued task must have a new task ID")
	}

	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	if queue.start(oldTask, cancel) {
		t.Fatal("canceled task started after reenqueue")
	}
	queue.complete(oldTask)
	if !queue.start(newTask, cancel) {
		t.Fatal("old task completion removed the new task")
	}
	queue.complete(newTask)
}

func TestQueue_CancelRunningTaskPreservesDeduplicationUntilCompletion(t *testing.T) {
	queue := newTestQueue(t, 1)
	job := newTestTask(time.Minute)
	requireQueued(t, queue, job)
	current, ok := queue.tryDequeue()
	if !ok {
		t.Fatal("queued task is missing")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !queue.start(current, cancel) {
		t.Fatal("cannot start task")
	}

	queue.CancelTrip(job.tripID)
	if ctx.Err() != context.Canceled {
		t.Fatalf("running context error = %v, want context.Canceled", ctx.Err())
	}
	if result, _ := queue.tryEnqueue(job); result != resultDuplicate {
		t.Fatalf("enqueue during cancellation = %q, want duplicate", result)
	}

	queue.complete(current)
	requireQueued(t, queue, job)
}

func TestQueue_ConcurrentEnqueueDeduplicates(t *testing.T) {
	const producers = 32
	queue := newTestQueue(t, producers)
	job := newTestTask(time.Minute)
	start := make(chan struct{})
	results := make(chan enqueueResult, producers)
	var wg sync.WaitGroup

	for i := 0; i < producers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, _ := queue.tryEnqueue(job)
			results <- result
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	counts := make(map[enqueueResult]int)
	for result := range results {
		counts[result]++
	}
	if counts[resultQueued] != 1 || counts[resultDuplicate] != producers-1 {
		t.Fatalf("enqueue results = %v, want one queued and %d duplicates", counts, producers-1)
	}
}

func newTestQueue(t *testing.T, capacity int) *Queue {
	t.Helper()
	queue, err := NewQueue(capacity)
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}
	return queue
}

func newTestTask(age time.Duration) task {
	return task{
		tripID:         uuid.New(),
		driverID:       uuid.New(),
		lastActivityAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC).Add(-age),
	}
}

func requireQueued(t *testing.T, queue *Queue, job task) {
	t.Helper()
	result, evicted := queue.tryEnqueue(job)
	if result != resultQueued || evicted != nil {
		t.Fatalf("enqueue result = %q, evicted = %+v, want queued without eviction", result, evicted)
	}
}
