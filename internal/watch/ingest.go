package watch

import (
	"github.com/bkcarlos/ProtoWatch/internal/packet"
	"github.com/bkcarlos/ProtoWatch/internal/someip"
	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
)

// Ingest owns session counters and dispatch; protocol stores own their entity models.
func (s *Store) Ingest(frame []byte, ci gopacket.CaptureInfo, link layers.LinkType) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Packets++
	s.state.WireBytes += uint64(max(0, ci.Length))
	if ci.Timestamp.After(s.state.Observed) {
		s.state.Observed = ci.Timestamp
	}
	d, warning := packet.Decode(frame, ci, link)
	if warning != "" {
		s.state.Warnings[warning]++
		return
	}
	d.Packet = s.state.Packets
	if d.Transport == "UDP" && len(d.Payload) >= 4 && string(d.Payload[:4]) == "RTPS" {
		s.ingestDDS(d)
		return
	}
	s.someip.Ingest(d)
}
func (s *Store) SomeIPSample(id uint64) (someip.Sample, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.someip.Sample(id)
}
func (s *Store) SomeIPConfig() someip.Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.someip.Config()
}
func (s *Store) SetSomeIPConfig(data []byte) error {
	c, err := someip.ParseConfig(data)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.someip.SetConfig(c)
	return nil
}
