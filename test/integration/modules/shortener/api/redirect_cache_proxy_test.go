//go:build integration

package api_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// redirectCacheProxy delays one selected Redis SET while other connections proceed.
type redirectCacheProxy struct {
	listener           net.Listener
	target             string
	mu                 sync.Mutex
	code               string
	held               chan struct{}
	release            chan struct{}
	delivered          chan struct{}
	rejectedDeleteCode string
	rejectedDelete     chan struct{}
}

func newRedirectCacheProxy(t *testing.T, target string) *redirectCacheProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxy := &redirectCacheProxy{listener: listener, target: target}
	t.Cleanup(func() {
		_ = listener.Close()
	})
	go func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			go proxy.serve(client)
		}
	}()
	return proxy
}

func (p *redirectCacheProxy) hold(code string) (<-chan struct{}, chan<- struct{}, <-chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.code = code
	p.held = make(chan struct{})
	p.release = make(chan struct{})
	p.delivered = make(chan struct{})
	return p.held, p.release, p.delivered
}

func (p *redirectCacheProxy) rejectDelete(code string) <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rejectedDeleteCode = code
	p.rejectedDelete = make(chan struct{})
	return p.rejectedDelete
}

func (p *redirectCacheProxy) serve(client net.Conn) {
	defer client.Close()
	server, err := net.Dial("tcp", p.target)
	if err != nil {
		return
	}
	defer server.Close()
	go func() {
		_, _ = io.Copy(client, server)
	}()
	reader := bufio.NewReader(client)
	for {
		command, args, err := p.readRedisCommand(reader)
		if err != nil {
			return
		}
		p.mu.Lock()
		var held, release, delivered, rejected chan struct{}
		if len(args) >= 2 && strings.EqualFold(args[0], "del") && p.rejectedDeleteCode != "" && strings.HasSuffix(args[1], ":"+p.rejectedDeleteCode) {
			rejected = p.rejectedDelete
			p.rejectedDeleteCode = ""
		}
		if len(args) >= 2 && strings.EqualFold(args[0], "set") && strings.HasSuffix(args[1], ":"+p.code) && p.code != "" {
			held, release, delivered = p.held, p.release, p.delivered
			p.code = ""
		}
		p.mu.Unlock()
		if rejected != nil {
			if _, err := client.Write([]byte("-ERR test invalidation failure\r\n")); err != nil {
				return
			}
			close(rejected)
			continue
		}
		if held != nil {
			close(held)
			<-release
		}
		if _, err := server.Write(command); err != nil {
			return
		}
		if delivered != nil {
			close(delivered)
		}
	}
}

func (p *redirectCacheProxy) readRedisCommand(reader *bufio.Reader) ([]byte, []string, error) {
	var raw bytes.Buffer
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, nil, err
	}
	raw.WriteString(line)
	if !strings.HasPrefix(line, "*") {
		return nil, nil, fmt.Errorf("unexpected Redis command %q", line)
	}
	count, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil || count < 0 {
		return nil, nil, fmt.Errorf("invalid Redis array %q", line)
	}
	args := make([]string, 0, count)
	for range count {
		line, err = reader.ReadString('\n')
		if err != nil {
			return nil, nil, err
		}
		raw.WriteString(line)
		if !strings.HasPrefix(line, "$") {
			return nil, nil, fmt.Errorf("unexpected Redis argument %q", line)
		}
		size, err := strconv.Atoi(strings.TrimSpace(line[1:]))
		if err != nil || size < 0 || size > 1<<20 {
			return nil, nil, fmt.Errorf("invalid Redis argument size %q", line)
		}
		data := make([]byte, size+2)
		if _, err := io.ReadFull(reader, data); err != nil {
			return nil, nil, err
		}
		raw.Write(data)
		args = append(args, string(data[:size]))
	}
	return raw.Bytes(), args, nil
}
