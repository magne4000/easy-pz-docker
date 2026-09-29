package rcon

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	typeResponseValue = 0
	typeExecCommand   = 2
	typeAuthResponse  = 2
	typeAuth          = 3

	maxPacket      = 1 << 20
	fullBodyCutoff = 4000
	defaultTimeout = 10 * time.Second
	idleAfterFull  = 250 * time.Millisecond
)

var ErrAuth = errors.New("rcon: authentication failed")

type packet struct {
	id   int32
	typ  int32
	body string
}

type Client struct {
	mu     sync.Mutex
	conn   net.Conn
	r      *bufio.Reader
	nextID int32
}

func Dial(ctx context.Context, addr, password string) (*Client, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("rcon: dial %s: %w", addr, err)
	}
	c := &Client{conn: conn, r: bufio.NewReader(conn)}
	if err := c.auth(ctx, password); err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) deadline(ctx context.Context) time.Time {
	if dl, ok := ctx.Deadline(); ok {
		return dl
	}
	return time.Now().Add(defaultTimeout)
}

func (c *Client) auth(ctx context.Context, password string) error {
	c.conn.SetDeadline(c.deadline(ctx))
	defer c.conn.SetDeadline(time.Time{})
	id := c.id()
	if err := c.write(packet{id: id, typ: typeAuth, body: password}); err != nil {
		return err
	}
	for {
		p, err := c.read()
		if err != nil {
			return fmt.Errorf("rcon: auth: %w", err)
		}
		if p.typ != typeAuthResponse {
			continue // some servers send an empty RESPONSE_VALUE first
		}
		if p.id == -1 {
			return ErrAuth
		}
		return nil
	}
}

func (c *Client) id() int32 {
	c.nextID++
	if c.nextID <= 0 {
		c.nextID = 1
	}
	return c.nextID
}

// Exec runs one command and returns the reassembled response.
func (c *Client) Exec(ctx context.Context, cmd string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	dl := c.deadline(ctx)
	c.conn.SetDeadline(dl)
	defer c.conn.SetDeadline(time.Time{})
	id := c.id()
	if err := c.write(packet{id: id, typ: typeExecCommand, body: cmd}); err != nil {
		return "", err
	}
	var parts []string
	for {
		p, err := c.read()
		if err != nil {
			var ne net.Error
			if len(parts) > 0 && errors.As(err, &ne) && ne.Timeout() {
				break // idle after a full-size packet: the response ended exactly on a boundary
			}
			return "", fmt.Errorf("rcon: exec: %w", err)
		}
		if p.id != id || p.typ != typeResponseValue {
			continue
		}
		parts = append(parts, p.body)
		if len(p.body) < fullBodyCutoff {
			break
		}
		idle := time.Now().Add(idleAfterFull)
		if idle.Before(dl) {
			c.conn.SetReadDeadline(idle)
		}
	}
	return strings.TrimRight(strings.Join(parts, ""), "\x00 \r\n\t"), nil
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) write(p packet) error {
	size := int32(4 + 4 + len(p.body) + 2)
	buf := make([]byte, 4+size)
	binary.LittleEndian.PutUint32(buf[0:], uint32(size))
	binary.LittleEndian.PutUint32(buf[4:], uint32(p.id))
	binary.LittleEndian.PutUint32(buf[8:], uint32(p.typ))
	copy(buf[12:], p.body)
	_, err := c.conn.Write(buf)
	return err
}

func (c *Client) read() (packet, error) {
	var hdr [12]byte
	if _, err := io.ReadFull(c.r, hdr[:]); err != nil {
		return packet{}, err
	}
	size := int32(binary.LittleEndian.Uint32(hdr[0:]))
	if size < 10 || size > maxPacket {
		return packet{}, fmt.Errorf("rcon: bad packet size %d", size)
	}
	body := make([]byte, size-8)
	if _, err := io.ReadFull(c.r, body); err != nil {
		return packet{}, err
	}
	return packet{
		id:   int32(binary.LittleEndian.Uint32(hdr[4:])),
		typ:  int32(binary.LittleEndian.Uint32(hdr[8:])),
		body: strings.TrimRight(string(body), "\x00"),
	}, nil
}
