package helpers

import (
	"sync"

	"github.com/samber/oops"
)

// Executes a function for each element in the input data array
// The output of the function can be any other type of any quantity
// Regardless the ordinality of the output, all results will be
// collected into a single flat unordered list.
// Errors will be collected into a single joined error.
// Thus it is possible for partial failures when err!=nil
func ParallelEach[T any, R any](runFunc func(T) ([]R, error), data []T) ([]R, error) {
	if len(data) < 1 {
		return []R{}, nil
	}
	type outputType struct {
		Error   error
		Results []R
	}
	outputChan := make(chan outputType, len(data))

	var writerGroup sync.WaitGroup
	for _, d := range data {
		writerGroup.Go(func() {
			res, err := runFunc(d)
			outputChan <- outputType{Error: err, Results: res}
		})
	}

	errs := []error{}
	results := []R{}
	var readerGroup sync.WaitGroup
	readerGroup.Go(func() {
		for val := range outputChan {
			errs = append(errs, val.Error)
			results = append(results, val.Results...)
		}
	})
	writerGroup.Wait()
	close(outputChan)
	readerGroup.Wait()
	return results, oops.Join(errs...)
}
