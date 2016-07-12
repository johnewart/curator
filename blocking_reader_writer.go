// Taken from https://gist.github.com/elazarl/998933

package main

import (
	"bytes"
	"log"
)

type BlockReadWriter struct {
	buf   *bytes.Buffer
	read  chan []byte
	write chan []byte
	nrc   chan int
	errc  chan error
}

func NewBlockReadWriter() *BlockReadWriter {
	ret := &BlockReadWriter{bytes.NewBuffer(nil),
		make(chan []byte),
		make(chan []byte),
		make(chan int),
		make(chan error)}
	go func(bc *BlockReadWriter) {
		for {
			var readTo, write []byte
			var hasData = true
			doWrite := func() {
				_, err := bc.buf.Write(write)
				if err != nil {
					log.Fatal("BlockReadWriter:", err)
				}
			}
			select {
			case readTo = <-bc.read:
				if bc.buf.Len() == 0 {
					for {
						if !hasData {
							break
						}
						write, hasData = <-bc.write
						doWrite()
						if !hasData {
							break
						}
						if bc.buf.Len() > 0 {
							break
						}
					}
				}
				nr, err := bc.buf.Read(readTo)
				bc.nrc <- nr
				bc.errc <- err
			case write = <-bc.write:
				if !hasData {
					log.Fatal("write after eof")
				}
				doWrite()
			}
		}
	}(ret)
	return ret
}

func (bc *BlockReadWriter) Write(data []byte) (nr int, err error) {
	bc.write <- data
	nr = len(data)
	return
}

func (bc *BlockReadWriter) Read(b []byte) (nr int, err error) {
	bc.read <- b
	nr = <-bc.nrc
	err = <-bc.errc
	return
}
