package service

import (
	"bytes"
)

// cappedBuffer is a write-capped bytes.Buffer. Writes after the cap
// are silently dropped except for a one-time truncation marker so
// the reader can tell logs were trimmed.
type cappedBuffer struct {
	buf       *bytes.Buffer
	cap       int
	truncated bool
}

func newCappedBuffer(cap int) *cappedBuffer {
	return &cappedBuffer{buf: &bytes.Buffer{}, cap: cap}
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.buf.Len() >= c.cap {
		if !c.truncated {
			c.buf.WriteString("\n... [log truncated at 1 MiB] ...\n")
			c.truncated = true
		}
		return len(p), nil
	}
	remaining := c.cap - c.buf.Len()
	if len(p) <= remaining {
		return c.buf.Write(p)
	}
	c.buf.Write(p[:remaining])
	c.buf.WriteString("\n... [log truncated at 1 MiB] ...\n")
	c.truncated = true
	return len(p), nil
}

func (c *cappedBuffer) String() string { return c.buf.String() }
