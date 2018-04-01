package gozstd

// #include "zstd.h"
// #include "common/zstd_errors.h"
//
// #include <stdlib.h>  // for malloc/free
import "C"

import (
	"fmt"
	"io"
	"reflect"
	"runtime"
	"unsafe"
)

var (
	cstreamInBufSize  = C.ZSTD_CStreamInSize()
	cstreamOutBufSize = C.ZSTD_CStreamOutSize()
)

// Writer implements zstd writer.
type Writer struct {
	w                io.Writer
	compressionLevel int
	cs               *C.ZSTD_CStream

	inBuf  *C.ZSTD_inBuffer
	outBuf *C.ZSTD_outBuffer

	inBufGo  []byte
	outBufGo []byte
}

// NewWriter returns new zstd writer writing compressed data to w.
//
// The returned writer must be closed with Close call in order
// to finalize the compressed stream.
func NewWriter(w io.Writer) *Writer {
	return NewWriterLevel(w, DefaultCompressionLevel)
}

// NewWriterLevel returns new zstd writer writing compressed data to w
// at the given compression level.
//
// The returned writer must be closed with Close call in order
// to finalize the compressed stream.
func NewWriterLevel(w io.Writer, compressionLevel int) *Writer {
	cs := C.ZSTD_createCStream()
	result := C.ZSTD_initCStream(cs, C.int(compressionLevel))
	ensureNoError(result)

	inBuf := (*C.ZSTD_inBuffer)(C.malloc(C.sizeof_ZSTD_inBuffer))
	inBuf.src = C.malloc(cstreamInBufSize)
	inBuf.size = 0
	inBuf.pos = 0

	outBuf := (*C.ZSTD_outBuffer)(C.malloc(C.sizeof_ZSTD_outBuffer))
	outBuf.dst = C.malloc(cstreamOutBufSize)
	outBuf.size = cstreamOutBufSize
	outBuf.pos = 0

	zw := &Writer{
		w:                w,
		compressionLevel: compressionLevel,
		cs:               cs,
		inBuf:            inBuf,
		outBuf:           outBuf,
	}

	zw.inBufGo = *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{
		Data: uintptr(inBuf.src),
		Len:  int(cstreamInBufSize),
		Cap:  int(cstreamInBufSize),
	}))
	zw.outBufGo = *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{
		Data: uintptr(outBuf.dst),
		Len:  int(cstreamOutBufSize),
		Cap:  int(cstreamOutBufSize),
	}))

	runtime.SetFinalizer(zw, freeCStream)
	return zw
}

// Reset resets zw to write to w.
func (zw *Writer) Reset(w io.Writer) {
	zw.inBuf.size = 0
	zw.inBuf.pos = 0
	zw.outBuf.size = cstreamOutBufSize
	zw.outBuf.pos = 0

	result := C.ZSTD_initCStream(zw.cs, C.int(zw.compressionLevel))
	ensureNoError(result)

	zw.w = w
}

func freeCStream(v interface{}) {
	zw := v.(*Writer)
	result := C.ZSTD_freeCStream(zw.cs)
	ensureNoError(result)

	C.free(zw.inBuf.src)
	C.free(unsafe.Pointer(zw.inBuf))

	C.free(zw.outBuf.dst)
	C.free(unsafe.Pointer(zw.outBuf))
}

// Write writes p to zw.
func (zw *Writer) Write(p []byte) (int, error) {
	pLen := len(p)
	if pLen == 0 {
		return 0, nil
	}

	for {
		n := copy(zw.inBufGo[zw.inBuf.size:], p)
		zw.inBuf.size += C.size_t(n)
		p = p[n:]
		if len(p) == 0 {
			// Fast path - just copy the data to input buffer.
			return pLen, nil
		}
		if err := zw.flushInBuf(); err != nil {
			return 0, err
		}
	}
}

func (zw *Writer) flushInBuf() error {
	result := C.ZSTD_compressStream(zw.cs, zw.outBuf, zw.inBuf)

	// Adjust inBuf.
	copy(zw.inBufGo, zw.inBufGo[zw.inBuf.pos:zw.inBuf.size])
	zw.inBuf.size -= zw.inBuf.pos
	zw.inBuf.pos = 0

	if C.ZSTD_getErrorCode(result) != 0 {
		panic(fmt.Errorf("BUG: cannot compress data: %s", errStr(result)))
	}

	// Flush outBuf.
	return zw.flushOutBuf()
}

func (zw *Writer) flushOutBuf() error {
	if zw.outBuf.pos == 0 {
		return nil
	}
	_, err := zw.w.Write(zw.outBufGo[:zw.outBuf.pos])
	zw.outBuf.pos = 0
	if err != nil {
		return fmt.Errorf("cannot flush internal buffer to the underlying writer: %s", err)
	}
	return nil
}

// Flush flushes the remaining data from zw to the underlying writer.
func (zw *Writer) Flush() error {
	// Flush inBuf.
	for zw.inBuf.size > 0 {
		if err := zw.flushInBuf(); err != nil {
			return err
		}
	}

	// Flush the internal buffer to outBuf.
	for {
		result := C.ZSTD_flushStream(zw.cs, zw.outBuf)
		if err := zw.flushOutBuf(); err != nil {
			return err
		}
		if result == 0 {
			// No more data left in the internal buffer.
			return nil
		}
		if C.ZSTD_getErrorCode(result) != 0 {
			panic(fmt.Errorf("BUG: cannot flush internal buffer to outBuf: %s", errStr(result)))
		}
	}
}

// Close finalizes the compressed stream.
//
// It doesn't close the underlying writer passed to New* functions.
func (zw *Writer) Close() error {
	if err := zw.Flush(); err != nil {
		return err
	}

	for {
		result := C.ZSTD_endStream(zw.cs, zw.outBuf)
		if err := zw.flushOutBuf(); err != nil {
			return err
		}
		if result == 0 {
			return nil
		}
		if C.ZSTD_getErrorCode(result) != 0 {
			panic(fmt.Errorf("BUG: cannot close writer stream: %s", errStr(result)))
		}
	}
}
