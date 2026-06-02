package oopsie

func resetContextExtractorsForTesting() {
	contextExtractors.mu.Lock()
	defer contextExtractors.mu.Unlock()
	contextExtractors.fns = nil
}
