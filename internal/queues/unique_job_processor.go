package queues

import (
	"slices"
)

type queueItem struct {
	id  string
	job func() error
}

type JobFinishedEvent struct {
	Id     string
	Result error
}

type UniqueJobProcessor struct {
	JobFinishedChannel          chan JobFinishedEvent
	processorPoolSize           int
	queue                       []queueItem
	numberOfJobsBeingProcessed  int
	newJobChannel               chan queueItem
	jobFinishedInternalChannel  chan JobFinishedEvent
	setProcessorPoolSizeChannel chan int
}

// You need to consume messages from [UniqueJobProcessor.JobFinishedChannel] or it get's stuck
func NewUniqueJobProcessor(processorPoolSize int) *UniqueJobProcessor {
	JobFinishedChannel := make(chan JobFinishedEvent)
	newJobChannel := make(chan queueItem)
	jobFinishedInternalChannel := make(chan JobFinishedEvent)
	setProcessorPoolSizeChannel := make(chan int)

	return &UniqueJobProcessor{
		JobFinishedChannel:          JobFinishedChannel,
		processorPoolSize:           processorPoolSize,
		queue:                       nil,
		numberOfJobsBeingProcessed:  0,
		newJobChannel:               newJobChannel,
		jobFinishedInternalChannel:  jobFinishedInternalChannel,
		setProcessorPoolSizeChannel: setProcessorPoolSizeChannel,
	}
}

func (u *UniqueJobProcessor) Start() {
	for {
		select {
		case job := <-u.newJobChannel:
			isAlreadyQueued := slices.ContainsFunc(u.queue, func(item queueItem) bool {
				return item.id == job.id
			})
			if isAlreadyQueued {
				continue
			}

			u.queue = append(u.queue, job)
			u.fillProcessors()

		case _ = <-u.jobFinishedInternalChannel:
			u.numberOfJobsBeingProcessed--
			u.fillProcessors()

		case processorPoolSize := <-u.setProcessorPoolSizeChannel:
			u.processorPoolSize = processorPoolSize
		}
	}
}

func (u *UniqueJobProcessor) SetProcessorPoolSize(processorPoolSize int) {
	u.setProcessorPoolSizeChannel <- processorPoolSize
}

func (u *UniqueJobProcessor) Process(id string, job func() error) {
	u.newJobChannel <- queueItem{
		id:  id,
		job: job,
	}
}

func (u *UniqueJobProcessor) fillProcessors() {
	for u.numberOfJobsBeingProcessed < u.processorPoolSize && len(u.queue) > 0 {
		job := u.queue[0]
		u.queue = u.queue[1:]
		u.numberOfJobsBeingProcessed++
		u.processItem(job)
	}
}

func (u *UniqueJobProcessor) processItem(job queueItem) {
	go func() {
		err := job.job()

		event := JobFinishedEvent{
			Id:     job.id,
			Result: err,
		}
		u.jobFinishedInternalChannel <- event
		u.JobFinishedChannel <- event
	}()
}
