package queue

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type Number struct {
	Data int
}

func TestQueueEnqueue(t *testing.T) {
	q := NewQueue[Number]()
	q.Enqueue(&Number{Data: 1})
	q.Enqueue(&Number{Data: 2})
	q.Enqueue(&Number{Data: 3})

	assert.Equal(t, q.Len(), 3)
}

func TestQueuePeek(t *testing.T) {
	number := &Number{Data: 1}
	q := NewQueue[Number]()
	q.Enqueue(number)
	q.Enqueue(&Number{Data: 2})
	q.Enqueue(&Number{Data: 3})

	assert.Equal(t, q.Peek(), number)
	assert.Equal(t, q.Peek(), number)
}

func TestQueueDequeue(t *testing.T) {
	number := &Number{Data: 1}
	q := NewQueue[Number]()
	q.Enqueue(number)
	q.Enqueue(&Number{Data: 2})
	q.Enqueue(&Number{Data: 3})

	assert.Equal(t, q.Dequeue(), number)
	assert.Equal(t, q.Len(), 2)
}

func TestQueueLen(t *testing.T) {
	q := NewQueue[Number]()
	q.Enqueue(&Number{Data: 1})
	q.Enqueue(&Number{Data: 2})
	q.Enqueue(&Number{Data: 3})

	assert.Equal(t, q.Len(), 3)
	q.Dequeue()
	assert.Equal(t, q.Len(), 2)
	q.Dequeue()
	assert.Equal(t, q.Len(), 1)
	q.Dequeue()
	assert.Equal(t, q.Len(), 0)
}
