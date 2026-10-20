package watch

import (
	"github.com/bkcarlos/ProtoWatch/internal/packet"
	"github.com/bkcarlos/ProtoWatch/internal/rtps"
)

func (s *Store) ingestDDS(d packet.Datagram) {
	s.state.RTPSPackets++
	m, err := rtps.Parse(d.Payload)
	if err != nil {
		s.state.Warnings["rtps_parse_failed"]++
		return
	}
	source := d.Source
	ci := d
	prefix := m.Prefix
	participant := s.participant(prefix, ci.Time)
	if participant != nil {
		participant.Vendor = vendor(m.Vendor)
		participant.Version = m.Version
		participant.Source = source
	}
	for _, w := range m.Warnings {
		s.state.Warnings[w]++
	}
	s.state.ControlCount += m.Control
	for _, d := range m.Data {
		if d.Discovery != nil {
			s.state.DiscoveryCount++
			disc := d.Discovery
			if disc.Kind == "participant" {
				q := s.participant(disc.GUID[:24], ci.Time)
				if q != nil {
					q.Name = disc.Name
					q.Domain = disc.Domain
					q.DiscoveryObserved = true
				}
			} else {
				e := s.endpoint(disc.GUID, ci.Time)
				if e != nil {
					e.Kind = disc.Kind
					e.Topic = disc.Topic
					e.Type = disc.Type
					e.Reliability = disc.Reliability
					e.Durability = disc.Durability
					e.Source = source
				}
			}
			continue
		}
		if d.Builtin {
			continue
		}
		if d.KeyOnly {
			s.state.Warnings["key_only_data"]++
			continue
		}
		e := s.endpoint(d.Writer, ci.Time)
		if e == nil {
			continue
		}
		e.Kind = "writer"
		e.Source = source
		key := d.Writer + ":" + d.Sequence
		if _, ok := s.seen[key]; ok {
			s.state.DuplicateCount++
			continue
		}
		if len(s.seenOrder) < 65536 {
			s.seenOrder = append(s.seenOrder, key)
		} else {
			delete(s.seen, s.seenOrder[s.seenPos])
			s.seenOrder[s.seenPos] = key
			s.seenPos = (s.seenPos + 1) % 65536
		}
		s.seen[key] = struct{}{}
		s.state.SampleCount++
		e.Samples++
		e.Bytes += uint64(len(d.Payload))
		sec := ci.Time.Unix()
		idx := int((sec%5 + 5) % 5)
		b := &e.buckets[idx]
		if b.Sec <= sec {
			if b.Sec != sec {
				*b = bucket{Sec: sec}
			}
			b.Samples++
			b.Bytes += uint64(len(d.Payload))
		}
		sample := Sample{ID: s.state.SampleCount, Writer: d.Writer, Sequence: d.Sequence, Time: ci.Time, Packet: s.state.Packets, Source: source, Size: len(d.Payload), Encoding: encoding(d.Payload)}
		sample.Truncated = len(d.Payload) > MaxPayload
		sample.Payload = append([]byte(nil), d.Payload[:min(len(d.Payload), MaxPayload)]...)
		if sample.Truncated {
			s.state.Warnings["payload_preview_truncated"]++
		}
		if len(s.samples) < MaxSamples {
			s.samples = append(s.samples, sample)
		} else {
			s.samples[s.samplePos] = sample
			s.samplePos = (s.samplePos + 1) % MaxSamples
			s.state.Evicted++
		}
	}
}
