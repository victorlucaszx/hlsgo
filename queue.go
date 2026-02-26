package main

import (
	"log"
	"sync"
)

type JobQueue struct {
	jobs    chan *ConversionJob
	active  map[string]*ConversionJob
	mu      sync.RWMutex
	wg      sync.WaitGroup
}

func NewJobQueue() *JobQueue {
	q := &JobQueue{
		jobs:   make(chan *ConversionJob, 100),
		active: make(map[string]*ConversionJob),
	}
	q.wg.Add(1)
	go q.worker()
	return q
}

func (q *JobQueue) Enqueue(job *ConversionJob) {
	q.mu.Lock()
	q.active[job.ID] = job
	q.mu.Unlock()
	q.jobs <- job
	log.Printf("[QUEUE] Job %s enfileirado (media_file_id=%d, qualidades=%v)", job.ID, job.Request.MediaFileID, job.Request.Qualities)
}

func (q *JobQueue) Cancel(conversionID string) bool {
	q.mu.RLock()
	job, exists := q.active[conversionID]
	q.mu.RUnlock()
	if !exists {
		return false
	}
	job.Cancel()
	log.Printf("[QUEUE] Job %s cancelado", conversionID)
	return true
}

func (q *JobQueue) Remove(conversionID string) {
	q.mu.Lock()
	delete(q.active, conversionID)
	q.mu.Unlock()
}

func (q *JobQueue) worker() {
	defer q.wg.Done()
	for job := range q.jobs {
		log.Printf("[QUEUE] Processando job %s", job.ID)
		processJob(job)
		q.Remove(job.ID)
		log.Printf("[QUEUE] Job %s finalizado", job.ID)
	}
}

func (q *JobQueue) Shutdown() {
	close(q.jobs)
	q.wg.Wait()
}
