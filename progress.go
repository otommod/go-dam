package godam

import (
	"io"
	"log"
	"time"
)

var (
	activityCh = make(chan int)
)

type ActivityReadCloser struct {
	io.ReadCloser
}

func (p ActivityReadCloser) Read(buf []byte) (int, error) {
	read, err := p.ReadCloser.Read(buf)
	activityCh <- read
	return read, err
}

type ActivityWriter struct {
	io.Writer
}

func (p ActivityWriter) Write(buf []byte) (int, error) {
	written, err := p.Writer.Write(buf)
	activityCh <- written
	return written, err
}

func init() {
	go func() {
		second := time.Tick(time.Second)
		activity := 0

		for {
			select {
			case <-second:
				log.Printf("DL @ %d kB/s", activity/1024)
				activity = 0

			case x := <-activityCh:
				activity += x
			}
		}
	}()
}
