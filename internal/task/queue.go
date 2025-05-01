package task

const TaskQueueName = "task_queue"

type QueuePublisher interface {
	Publish(taskID string, input string) error
	DeclareQueue() error
	Close() error
}

type Acknowledger interface {
	Ack(multiple bool) error
	Nack(multiple bool, requeue bool) error
}
