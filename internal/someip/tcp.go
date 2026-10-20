package someip

import (
	"bytes"
	"github.com/bkcarlos/ProtoWatch/internal/packet"
	"time"
)

const MaxTCPConnections = 256
const MaxTCPMessage = 4 * 1024 * 1024 // includes the 16-byte SOME/IP header
const MaxTCPBuffered = 32 * 1024 * 1024
const MaxTCPReorderBytes = 256 * 1024
const MaxTCPSegments = 128
const TCPIdleTimeout = 60 * time.Second

type tcpSegment struct {
	seq  uint32
	data []byte
	fin  bool
}
type tcpHalf struct {
	initialized, syn, closed, failed, known, matched, midstream bool
	isn, next                                                   uint32
	buffer                                                      []byte
	pending                                                     []tcpSegment
	pendingBytes                                                int
	packets, charged                                            uint64
}
type tcpConnection struct {
	halves [2]tcpHalf
	last   time.Time
}

func tcpKey(d packet.Datagram) (string, int) {
	if d.Source < d.Destination {
		return d.Source + "/" + d.Destination, 0
	}
	return d.Destination + "/" + d.Source, 1
}
func (s *Store) clearTCPHalf(h *tcpHalf) {
	s.tcpBytes -= len(h.buffer) + h.pendingBytes
	h.buffer = nil
	h.pending = nil
	h.pendingBytes = 0
}
func (s *Store) closeTCP(c *tcpConnection) {
	for i := range c.halves {
		h := &c.halves[i]
		if (h.known || h.matched) && (len(h.buffer) > 0 || len(h.pending) > 0) {
			s.warn("tcp_incomplete")
		}
		s.clearTCPHalf(h)
		h.closed = true
	}
}
func (s *Store) FinishTCP() {
	for key, c := range s.tcp {
		s.closeTCP(c)
		delete(s.tcp, key)
	}
}
func (s *Store) ExpireTCP(now time.Time) {
	if now.After(s.tcpClock) {
		s.tcpClock = now
	}
	if s.tcpClock.Sub(s.tcpSweep) < time.Second {
		return
	}
	s.tcpSweep = s.tcpClock
	for key, c := range s.tcp {
		if s.tcpClock.Sub(c.last) > TCPIdleTimeout {
			s.closeTCP(c)
			delete(s.tcp, key)
		}
	}
}
func (s *Store) failTCP(h *tcpHalf, reason string) {
	if h.known || h.matched {
		s.warn(reason)
	}
	s.clearTCPHalf(h)
	h.failed = true
}
func (s *Store) reserveTCP(h *tcpHalf, n int) bool {
	if s.tcpBytes+n > MaxTCPBuffered {
		s.failTCP(h, "tcp_buffer_limit")
		return false
	}
	return true
}

func (s *Store) ingestTCP(d packet.Datagram, known bool) {
	key, dir := tcpKey(d)
	c := s.tcp[key]
	if c != nil && d.TCPSYN && !d.TCPACK {
		h := &c.halves[dir]
		// Repeated SYNs for the same ISN belong to the existing connection.
		if h.closed || !h.syn || h.isn != d.TCPSeq {
			s.closeTCP(c)
			delete(s.tcp, key)
			c = nil
		}
	}
	if c == nil {
		if len(d.Payload) == 0 && !d.TCPSYN {
			return
		}
		if len(s.tcp) >= MaxTCPConnections {
			var oldest string
			for k, v := range s.tcp {
				if oldest == "" || v.last.Before(s.tcp[oldest].last) {
					oldest = k
				}
			}
			s.closeTCP(s.tcp[oldest])
			delete(s.tcp, oldest)
			s.warn("tcp_connection_limit")
		}
		c = &tcpConnection{last: s.tcpClock}
		s.tcp[key] = c
	}
	if d.Time.After(c.last) {
		c.last = d.Time
	}
	if d.TCPRST {
		s.closeTCP(c)
		return
	}
	h := &c.halves[dir]
	h.known = h.known || known
	if h.closed || h.failed {
		return
	}
	if !h.initialized {
		if len(d.Payload) == 0 && !d.TCPSYN {
			return
		}
		h.initialized = true
		h.next = d.TCPSeq
		h.midstream = !d.TCPSYN
		if d.TCPSYN {
			h.syn = true
			h.isn = d.TCPSeq
			h.next++
		}
	}
	if len(d.Payload) > 0 {
		h.packets++
	}
	seq := d.TCPSeq
	if d.TCPSYN {
		seq++
	}
	s.acceptTCPSegment(h, seq, d.Payload, d.TCPFIN, d)
	// Includes retransmitted payload packets, but messages are counted only once.
	if h.known || h.matched {
		s.state.Packets += h.packets - h.charged
		h.charged = h.packets
	}
}

func (s *Store) acceptTCPSegment(h *tcpHalf, seq uint32, data []byte, fin bool, meta packet.Datagram) {
	if len(data) == 0 && !fin {
		return
	}
	offset := int64(int32(seq - h.next)) // TCP serial arithmetic, including sequence wrap.
	for _, p := range h.pending {
		start := max(offset, int64(int32(p.seq-h.next)))
		end := min(offset+int64(len(data)), int64(int32(p.seq-h.next))+int64(len(p.data)))
		if start < end && !bytes.Equal(data[start-offset:end-offset], p.data[start-int64(int32(p.seq-h.next)):end-int64(int32(p.seq-h.next))]) {
			s.failTCP(h, "tcp_overlap_conflict")
			return
		}
		if p.seq == seq && bytes.Equal(p.data, data) && p.fin == fin {
			return
		}
	}
	if offset > 0 {
		if offset+int64(len(data)) > MaxTCPReorderBytes {
			s.failTCP(h, "tcp_gap_limit")
			return
		}
		if len(h.pending) >= MaxTCPSegments || h.pendingBytes+len(data) > MaxTCPReorderBytes {
			s.failTCP(h, "tcp_reorder_limit")
			return
		}
		if !s.reserveTCP(h, len(data)) {
			return
		}
		h.pending = append(h.pending, tcpSegment{seq, append([]byte(nil), data...), fin})
		h.pendingBytes += len(data)
		s.tcpBytes += len(data)
		return
	}
	s.consumeTCP(h, seq, data, fin, meta)
	for !h.failed && !h.closed {
		index := -1
		for i, p := range h.pending {
			if int32(p.seq-h.next) <= 0 && (index < 0 || int32(p.seq-h.pending[index].seq) < 0) {
				index = i
			}
		}
		if index < 0 {
			break
		}
		p := h.pending[index]
		copy(h.pending[index:], h.pending[index+1:])
		h.pending[len(h.pending)-1] = tcpSegment{}
		h.pending = h.pending[:len(h.pending)-1]
		h.pendingBytes -= len(p.data)
		s.tcpBytes -= len(p.data)
		// Provenance is the packet that completed the contiguous byte sequence.
		s.consumeTCP(h, p.seq, p.data, p.fin, meta)
	}
}
func (s *Store) consumeTCP(h *tcpHalf, seq uint32, data []byte, fin bool, meta packet.Datagram) {
	skip := -int64(int32(seq - h.next))
	if skip > int64(len(data)) {
		return
	}
	data = data[skip:]
	if len(data) > 0 {
		if !s.reserveTCP(h, len(data)) {
			return
		}
		h.buffer = append(h.buffer, data...)
		s.tcpBytes += len(data)
		h.next += uint32(len(data))
		s.frameTCP(h, meta)
	}
	if fin && !h.failed {
		if len(h.buffer) > 0 || len(h.pending) > 0 {
			if h.known || h.matched {
				s.warn("tcp_incomplete")
			}
		}
		s.clearTCPHalf(h)
		h.next++
		h.closed = true
	}
}
func (s *Store) frameTCP(h *tcpHalf, d packet.Datagram) {
	// Release consumed prefixes without copying every incomplete packet append.
	originalLen := len(h.buffer)
	defer func() {
		if len(h.buffer) > 0 && len(h.buffer) < originalLen {
			h.buffer = bytes.Clone(h.buffer)
		}
	}()
	for len(h.buffer) >= 16 {
		b := h.buffer
		length := uint64(be.Uint32(b[4:8])) + 8
		if !Candidate(b) {
			s.failTCP(h, "tcp_invalid_header")
			return
		}
		if length > MaxTCPMessage {
			s.failTCP(h, "tcp_message_limit")
			return
		}
		if !h.matched {
			h.matched = true
			if h.midstream {
				s.warn("tcp_midstream")
			}
		}
		if uint64(len(b)) < length {
			return
		}
		// vsomeip magic cookies are control markers, never application messages.
		if isMagicCookie(b[:length]) {
			s.warn("tcp_magic_cookie")
		} else {
			messages, err := parseMessages(b[:length])
			if err != nil {
				s.failTCP(h, "tcp_invalid_header")
				return
			}
			m := messages[0]
			if m.SD != nil || m.TP {
				s.failTCP(h, "tcp_invalid_message")
				return
			}
			detection := "header_heuristic"
			if h.known {
				detection = "configured_or_sd_endpoint"
			}
			s.ingestMessage(m, d, detection)
		}
		h.buffer = h.buffer[int(length):]
		s.tcpBytes -= int(length)
		if len(h.buffer) == 0 {
			h.buffer = nil
		}
	}
}
func isMagicCookie(b []byte) bool {
	if len(b) != 16 {
		return false
	}
	client := []byte{0xff, 0xff, 0, 0, 0, 0, 0, 8, 0xde, 0xad, 0xbe, 0xef, 1, 1, 1, 0}
	server := []byte{0xff, 0xff, 0x80, 0, 0, 0, 0, 8, 0xde, 0xad, 0xbe, 0xef, 1, 1, 2, 0}
	return bytes.Equal(b, client) || bytes.Equal(b, server)
}
