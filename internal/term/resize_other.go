//go:build !unix

package term

// resizeEvents has no SIGWINCH outside Unix; Windows support is Later.
func resizeEvents() (<-chan struct{}, func()) { return nil, func() {} }
