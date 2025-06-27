package rpc

import "sync"

// ResponseProcFn is a function type for processing client responses
type ResponseProcFn func(resp clientResponse) error

// ResponseProcessor manages response callbacks by ID
type ResponseProcessor struct {
	cbs map[uint64]ResponseProcFn
	mu  *sync.RWMutex
}

// NewResponseProcessor creates a new response processor
func NewResponseProcessor() *ResponseProcessor {
	return &ResponseProcessor{
		cbs: make(map[uint64]ResponseProcFn),
		mu:  &sync.RWMutex{},
	}
}

// Add registers a callback function for a given response ID
func (r *ResponseProcessor) Add(id uint64, fn ResponseProcFn) {
	r.mu.Lock()
	r.cbs[id] = fn
	r.mu.Unlock()
}

// Remove removes a callback for a given ID
func (r *ResponseProcessor) Remove(id uint64) {
	r.mu.Lock()
	delete(r.cbs, id)
	r.mu.Unlock()
}

// Process processes a received response by calling its registered callback
// This is typically called by the receive routine
func (r *ResponseProcessor) Process(resp clientResponse) error {
	id := *resp.ID
	r.mu.RLock()
	fn, ok := r.cbs[id]
	r.mu.RUnlock()
	if ok && fn != nil {
		defer r.Remove(id)
		return fn(resp)
	}
	return nil
}
