package queue

type Queue[T any] struct {
	elements []*T
}

// NewQueue creates a new queue
func NewQueue[T any]() *Queue[T] {
	return &Queue[T]{
		elements: make([]*T, 0),
	}
}

// Enqueue adds an element to the queue
func (q *Queue[T]) Enqueue(item *T) {
	q.elements = append(q.elements, item)
}

// Peek returns the first element of the queue
func (q *Queue[T]) Peek() *T {
	return q.PeekN(0)
}

func (q *Queue[T]) PeekN(index int) *T {
	if len(q.elements) < index {
		return nil
	}

	return q.elements[index]
}

// Dequeue removes the first element of the queue
func (q *Queue[T]) Dequeue() *T {
	if len(q.elements) == 0 {
		return nil
	}

	item := q.Peek()
	q.elements = q.elements[1:]

	return item
}

// Len returns the length of the queue
func (q *Queue[T]) Len() int {
	return len(q.elements)
}
