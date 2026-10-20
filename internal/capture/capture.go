package capture

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"
)

const MaxImport int64 = 4 * 1024 * 1024 * 1024

// Sink must copy any packet data retained after Ingest returns.
type Sink interface {
	Ingest([]byte, gopacket.CaptureInfo, layers.LinkType)
}

type Live interface {
	ReadPacketData() ([]byte, gopacket.CaptureInfo, error)
	Close() error
	Dropped() (uint64, error)
}

// Reader is the in-memory convenience wrapper used by small fixtures.
func Reader(data []byte) (*pcapgo.Reader, error) { return StreamReader(bytes.NewReader(data)) }

// StreamReader validates only the global header; packets are read incrementally.
// Total input size is a caller policy. Individual packets remain bounded by snaplen.
func StreamReader(input io.Reader) (*pcapgo.Reader, error) {
	data := make([]byte, 24)
	if _, err := io.ReadFull(input, data); err != nil {
		return nil, fmt.Errorf("PCAP header is incomplete: %w", err)
	}
	magic := binary.BigEndian.Uint32(data[:4])
	if magic == 0x0a0d0d0a {
		return nil, fmt.Errorf("MVP0 supports classic PCAP only; export PCAPNG as PCAP first")
	}
	if magic != 0xa1b2c3d4 && magic != 0xd4c3b2a1 && magic != 0xa1b23c4d && magic != 0x4d3cb2a1 {
		return nil, fmt.Errorf("unsupported file; expected uncompressed PCAP")
	}
	r, err := pcapgo.NewReader(io.MultiReader(bytes.NewReader(data), input))
	if err != nil {
		return nil, err
	}
	if r.Snaplen() > 1024*1024 {
		return nil, fmt.Errorf("PCAP snaplen exceeds 1 MiB limit")
	}
	// The upstream LinkType is uint8: validate the original 32-bit header before
	// conversion so an unknown link type cannot alias a supported one.
	var bo binary.ByteOrder = binary.BigEndian
	if magic == 0xd4c3b2a1 || magic == 0x4d3cb2a1 {
		bo = binary.LittleEndian
	}
	link := bo.Uint32(data[20:24])
	if link != 1 && link != 113 {
		return nil, fmt.Errorf("unsupported PCAP link type %d (Ethernet=1, Linux SLL=113)", link)
	}
	return r, nil
}
func Import(ctx context.Context, r *pcapgo.Reader, s Sink) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		b, ci, err := r.ZeroCopyReadPacketData()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		s.Ingest(b, ci, r.LinkType())
	}
}
func Run(ctx context.Context, h Live, s Sink) error {
	for {
		b, ci, err := h.ReadPacketData()
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		s.Ingest(b, ci, layers.LinkTypeEthernet)
	}
}
