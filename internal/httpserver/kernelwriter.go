package httpserver

import (
	"context"
	"io"
	"net/http"
)

// kernelWriter takes the request's kernel charge before the first byte of its
// response reaches the connection (Ticket.waitKernel, Q-ARCH-500). A request
// whose context ends while it waits writes nothing: the client is gone. It
// forwards Flush and ReadFrom and exposes the wrapped writer through Unwrap, so
// http.ResponseController and the Flusher assertions keep working.
type kernelWriter struct {
	http.ResponseWriter
	ctx     context.Context
	t       *Ticket
	charged bool
	err     error
}

// start charges once; err is the context's error when the request ended first.
func (w *kernelWriter) start() error {
	if !w.charged {
		w.charged = true
		w.err = w.t.waitKernel(w.ctx)
	}
	return w.err
}

func (w *kernelWriter) WriteHeader(code int) {
	// 1xx answers (100 Continue, 103) are not the response.
	if code < 200 && code != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	if w.start() != nil {
		return
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *kernelWriter) Write(p []byte) (int, error) {
	if err := w.start(); err != nil {
		return 0, err
	}
	return w.ResponseWriter.Write(p)
}

// ReadFrom keeps the wrapped writer's io.ReaderFrom (sendfile) where it has one.
func (w *kernelWriter) ReadFrom(r io.Reader) (int64, error) {
	if err := w.start(); err != nil {
		return 0, err
	}
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(w.ResponseWriter, r)
}

// Flush implements http.Flusher.
func (w *kernelWriter) Flush() {
	if w.start() != nil {
		return
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Unwrap returns the wrapped writer for http.ResponseController.
func (w *kernelWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
