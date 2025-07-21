package dpdknet

import (
	"errors"
	"sync"
)

type Listener struct {
	pc      *UDPConn
	connCh  chan *UDPConn
	closed  bool
	mu      sync.Mutex
}

func Listen(port uint16) (*Listener, error) {
	pc, err := ListenUDP(port)
	if err != nil {
		return nil, err
	}
	l := &Listener{
		pc:     pc,
		connCh: make(chan *UDPConn, 1024),
	}
	go func() { l.connCh <- pc }()
	return l, nil
}

func (l *Listener) Accept() (*UDPConn, error) {
	if l.closed {
		return nil, errors.New("listener closed")
	}
	return <-l.connCh, nil
}

func (l *Listener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	return l.pc.Close()
}
