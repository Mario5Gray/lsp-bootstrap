// Acceptance test fixture — Go.
//
// Known facts for test assertions:
//   - NewWorker() return type : *Worker
//   - Worker.Run() is defined at the line marked DEFINITION
//   - Run() is called at the line marked REFERENCE
package sample

import "fmt"

// Worker processes jobs identified by an integer ID.
type Worker struct {
	ID int
}

// Run executes the worker and returns a status string. // DEFINITION
func (w *Worker) Run() string {
	return fmt.Sprintf("worker-%d", w.ID)
}

// NewWorker constructs a Worker with the given ID.
func NewWorker(id int) *Worker {
	return &Worker{ID: id}
}

// Dispatch creates a worker and runs it, returning the status.
func Dispatch(id int) string {
	w := NewWorker(id)
	return w.Run() // REFERENCE
}
