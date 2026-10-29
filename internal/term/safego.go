package term

// SafeGo runs fn in a new goroutine. If fn panics, the panic is recovered
// and passed to onPanic instead of crashing the program (design §8.6).
func SafeGo(fn func(), onPanic func(recovered any)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				onPanic(r)
			}
		}()
		fn()
	}()
}
