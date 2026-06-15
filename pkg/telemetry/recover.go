package telemetry

import (
	"fmt"
	"runtime/debug"

	"github.com/chainreactors/logs"
)

// SDKRecover recovers from a panic in an SDK call and converts it to an error.
// Use with named return values:
//
//	func foo() (err error) {
//	    defer telemetry.SDKRecover("gogo", &err)
//	    ...
//	}
func SDKRecover(engine string, errp *error) {
	r := recover()
	if r == nil {
		return
	}
	stack := debug.Stack()
	logs.Log.Errorf("[sdk.%s] panic recovered: %v\n%s", engine, r, stack)
	*errp = fmt.Errorf("sdk.%s panic: %v", engine, r)
}

// SDKGoRecover recovers from a panic inside a goroutine that processes SDK
// results. It logs the panic; the deferred close(out) in the caller signals
// the consumer that the stream ended.
func SDKGoRecover(engine string) {
	r := recover()
	if r == nil {
		return
	}
	stack := debug.Stack()
	logs.Log.Errorf("[sdk.%s] goroutine panic recovered: %v\n%s", engine, r, stack)
}

// SDKCapRecover recovers from a panic in a scan-pipeline capability callback.
// Use as the first defer in a capability Run function:
//
//	defer telemetry.SDKCapRecover("gogo", emit)
//
// If emit is non-nil, it is called with the error message so the pipeline can
// surface it. The emit type is func(string) to avoid coupling with the scan
// event type.
func SDKCapRecover(engine string, emit func(string)) {
	r := recover()
	if r == nil {
		return
	}
	stack := debug.Stack()
	msg := fmt.Sprintf("sdk.%s panic: %v", engine, r)
	logs.Log.Errorf("[sdk.%s] panic recovered: %v\n%s", engine, r, stack)
	if emit != nil {
		emit(msg)
	}
}
