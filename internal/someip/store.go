package someip

import (
	"fmt"
	"github.com/bkcarlos/ProtoWatch/internal/packet"
	"sort"
	"time"
)

const MaxSamples = 256
const MaxPayload = 16 * 1024
const MaxServices = 1024
const MaxMembers = 4096
const MaxDiscovery = 256

type Service struct {
	Key            string     `json:"key"`
	ServiceID      uint16     `json:"service_id"`
	InstanceID     *uint16    `json:"instance_id"`
	Name           string     `json:"name"`
	Major          byte       `json:"major"`
	Minor          *uint32    `json:"minor"`
	Provider       string     `json:"provider"`
	Endpoints      []Endpoint `json:"endpoints"`
	Status         string     `json:"status"`
	TTL            uint32     `json:"ttl"`
	Expires        time.Time  `json:"expires"`
	LastSeen       time.Time  `json:"last_seen"`
	Messages       uint64     `json:"messages"`
	Bytes          uint64     `json:"bytes"`
	Hz             float64    `json:"hz"`
	BytesPerSecond float64    `json:"bytes_per_second"`
	offeredAt      time.Time
	buckets        [5]rateBucket
}
type Member struct {
	Transport     string    `json:"transport"`
	Key           string    `json:"key"`
	ServiceKey    string    `json:"service_key"`
	ServiceID     uint16    `json:"service_id"`
	InstanceID    *uint16   `json:"instance_id"`
	MethodID      uint16    `json:"method_id"`
	Name          string    `json:"name"`
	Kind          string    `json:"kind"`
	Messages      uint64    `json:"messages"`
	Requests      uint64    `json:"requests"`
	Responses     uint64    `json:"responses"`
	Notifications uint64    `json:"notifications"`
	Errors        uint64    `json:"errors"`
	Bytes         uint64    `json:"bytes"`
	LastSeen      time.Time `json:"last_seen"`
}
type Sample struct {
	Transport   string    `json:"transport"`
	ID          uint64    `json:"id"`
	ServiceKey  string    `json:"service_key"`
	ServiceID   uint16    `json:"service_id"`
	InstanceID  *uint16   `json:"instance_id"`
	ServiceName string    `json:"service_name"`
	MethodID    uint16    `json:"method_id"`
	MethodName  string    `json:"method_name"`
	ClientID    uint16    `json:"client_id"`
	SessionID   uint16    `json:"session_id"`
	Interface   byte      `json:"interface_version"`
	MessageType string    `json:"message_type"`
	ReturnCode  byte      `json:"return_code"`
	Source      string    `json:"source"`
	Destination string    `json:"destination"`
	Detection   string    `json:"detection"`
	Time        time.Time `json:"time"`
	Packet      uint64    `json:"packet"`
	Size        int       `json:"size"`
	Truncated   bool      `json:"truncated"`
	Payload     []byte    `json:"-"`
}
type DiscoveryEvent struct {
	Entry
	ID             uint64    `json:"id"`
	ServiceName    string    `json:"service_name"`
	EventgroupName string    `json:"eventgroup_name"`
	Source         string    `json:"source"`
	Destination    string    `json:"destination"`
	Time           time.Time `json:"time"`
	Packet         uint64    `json:"packet"`
	SessionID      uint16    `json:"session_id"`
	Reboot         bool      `json:"reboot"`
	Unicast        bool      `json:"unicast"`
}
type State struct {
	TCPMessages      uint64            `json:"tcp_messages"`
	UDPMessages      uint64            `json:"udp_messages"`
	TCPConnections   int               `json:"tcp_connections"`
	TCPBuffered      int               `json:"tcp_buffered"`
	Packets          uint64            `json:"packets"`
	MessageCount     uint64            `json:"message_count"`
	DiscoveryCount   uint64            `json:"discovery_count"`
	Requests         uint64            `json:"requests"`
	Responses        uint64            `json:"responses"`
	Notifications    uint64            `json:"notifications"`
	Errors           uint64            `json:"errors"`
	Evicted          uint64            `json:"evicted"`
	DiscoveryEvicted uint64            `json:"discovery_evicted"`
	Warnings         map[string]uint64 `json:"warnings"`
	Services         []Service         `json:"services"`
	Members          []Member          `json:"members"`
	Samples          []Sample          `json:"samples"`
	Discovery        []DiscoveryEvent  `json:"discovery"`
}
type rateBucket struct {
	Sec          int64
	Count, Bytes uint64
}

// Store is owned by watch.Store; callers serialize access with the parent mutex.
type Store struct {
	state               State
	services            map[string]*Service
	members             map[string]*Member
	samples             []Sample
	discovery           []DiscoveryEvent
	samplePos, eventPos int
	config              Config
	tcp                 map[string]*tcpConnection
	tcpBytes            int
	tcpClock, tcpSweep  time.Time
}

func NewStore() *Store { s := &Store{config: EmptyConfig()}; s.Reset(); return s }
func (s *Store) Reset() {
	s.tcp = map[string]*tcpConnection{}
	s.tcpBytes = 0
	s.tcpClock, s.tcpSweep = time.Time{}, time.Time{}
	s.state = State{Warnings: map[string]uint64{}}
	s.services = map[string]*Service{}
	s.members = map[string]*Member{}
	s.samples = nil
	s.discovery = nil
	s.samplePos = 0
	s.eventPos = 0
}
func (s *Store) SetConfig(c Config) { s.config = c }
func (s *Store) Config() Config {
	c := EmptyConfig()
	c.Ports = append(c.Ports, s.config.Ports...)
	for k, v := range s.config.Services {
		n := ServiceNames{Name: v.Name, Methods: map[string]string{}, Eventgroups: map[string]string{}}
		for a, b := range v.Methods {
			n.Methods[a] = b
		}
		for a, b := range v.Eventgroups {
			n.Eventgroups[a] = b
		}
		c.Services[k] = n
	}
	return c
}
func (s *Store) warn(key string) { s.state.Warnings[key]++ }
func (s *Store) known(d packet.Datagram) bool {
	for _, p := range s.config.Ports {
		if d.SourcePort == p || d.DestinationPort == p {
			return true
		}
	}
	for _, v := range s.services {
		if !activeAt(v, d.Time) {
			continue
		}
		for _, ep := range v.Endpoints {
			if ep.Transport == d.Transport && ((ep.Address == d.SourceIP && ep.Port == d.SourcePort) || (ep.Address == d.DestinationIP && ep.Port == d.DestinationPort)) {
				return true
			}
		}
	}
	return false
}
func activeAt(v *Service, t time.Time) bool {
	return v.Status == "offered" && !t.Before(v.offeredAt) && t.Before(v.Expires)
}
func (s *Store) Ingest(d packet.Datagram) {
	s.ExpireTCP(d.Time)
	known := s.known(d)
	if d.Transport == "TCP" {
		s.ingestTCP(d, known)
		return
	}
	if d.Transport != "UDP" {
		return
	}
	sdPort := d.SourcePort == 30490 || d.DestinationPort == 30490
	if !known && !sdPort && !Candidate(d.Payload) {
		return
	}
	messages, err := Parse(d.Payload)
	if err != nil {
		if known || sdPort {
			s.warn("parse_failed")
		}
		return
	}
	s.state.Packets++
	detection := "header_heuristic"
	if known {
		detection = "configured_or_sd_endpoint"
	}
	for _, m := range messages {
		if m.SD != nil {
			s.ingestSD(m, d)
			continue
		}
		if m.TP {
			s.warn("tp_reassembly_unsupported")
			continue
		}
		s.ingestMessage(m, d, detection)
	}
}
func (s *Store) ingestSD(m Message, d packet.Datagram) {
	for _, w := range m.SD.Warnings {
		s.warn(w)
	}
	for _, entry := range m.SD.Entries {
		s.state.DiscoveryCount++
		e := DiscoveryEvent{Entry: entry, ID: s.state.DiscoveryCount, Source: d.Source, Destination: d.Destination, Time: d.Time, Packet: d.Packet, SessionID: m.Session, Reboot: m.SD.Flags&0x80 != 0, Unicast: m.SD.Flags&0x40 != 0}
		if len(s.discovery) < MaxDiscovery {
			s.discovery = append(s.discovery, e)
		} else {
			s.discovery[s.eventPos] = e
			s.eventPos = (s.eventPos + 1) % MaxDiscovery
			s.state.DiscoveryEvicted++
		}
		if entry.Kind != "OfferService" && entry.Kind != "StopOfferService" {
			continue
		}
		key := fmt.Sprintf("%04x/%04x/%d/%s", entry.Service, entry.Instance, entry.Major, d.SourceIP)
		v := s.services[key]
		if v == nil {
			if len(s.services) >= MaxServices {
				s.warn("service_limit")
				continue
			}
			id := entry.Instance
			v = &Service{Key: key, ServiceID: entry.Service, InstanceID: &id, Major: entry.Major, Provider: d.SourceIP, Endpoints: []Endpoint{}}
			s.services[key] = v
		}
		if d.Time.Before(v.offeredAt) {
			s.warn("out_of_order_sd")
			continue
		}
		v.offeredAt = d.Time
		minor := entry.Minor
		v.Minor = &minor
		v.TTL = entry.TTL
		v.Expires = d.Time.Add(time.Duration(entry.TTL) * time.Second)
		if d.Time.After(v.LastSeen) {
			v.LastSeen = d.Time
		}
		v.Status = "offered"
		if entry.TTL == 0 {
			v.Status = "stopped"
		} else {
			v.Endpoints = append([]Endpoint{}, entry.Endpoints...)
		}
	}
}
func (s *Store) resolve(m Message, d packet.Datagram) *Service {
	serverIP, serverPort := d.SourceIP, d.SourcePort
	if m.Type == 0 || m.Type == 1 || m.Type == 0x42 || m.Type == 0xc0 || m.Type == 0xc1 {
		serverIP, serverPort = d.DestinationIP, d.DestinationPort
	}
	var result *Service
	ambiguous := false
	for _, v := range s.services {
		if v.ServiceID != m.Service || v.Major != m.Interface || !activeAt(v, d.Time) {
			continue
		}
		for _, ep := range v.Endpoints {
			if ep.Address == serverIP && ep.Port == serverPort && ep.Transport == d.Transport && !ep.Multicast {
				if result != nil && result.Key != v.Key {
					ambiguous = true
				}
				result = v
				break
			}
		}
	}
	if result != nil && !ambiguous {
		return result
	}
	if ambiguous {
		s.warn("ambiguous_instance")
	}
	key := fmt.Sprintf("%04x/unknown/%d/%s:%d/%s", m.Service, m.Interface, serverIP, serverPort, d.Transport)
	if v := s.services[key]; v != nil {
		return v
	}
	if len(s.services) >= MaxServices {
		s.warn("service_limit")
		return nil
	}
	v := &Service{Key: key, ServiceID: m.Service, Major: m.Interface, Provider: serverIP, Status: "observed", Endpoints: []Endpoint{{Address: serverIP, Port: serverPort, Transport: d.Transport}}}
	s.services[key] = v
	return v
}
func (s *Store) ingestMessage(m Message, d packet.Datagram, detection string) {
	s.state.MessageCount++
	if d.Transport == "TCP" {
		s.state.TCPMessages++
	} else {
		s.state.UDPMessages++
	}
	switch m.Type {
	case 0, 1:
		s.state.Requests++
	case 0x80, 0x81:
		s.state.Responses++
	case 2:
		s.state.Notifications++
	}
	if m.ReturnCode != 0 || m.Type == 0x81 {
		s.state.Errors++
	}
	v := s.resolve(m, d)
	sample := Sample{Transport: d.Transport, ID: s.state.MessageCount, ServiceID: m.Service, MethodID: m.Method, ClientID: m.Client, SessionID: m.Session, Interface: m.Interface, MessageType: TypeName(m.Type), ReturnCode: m.ReturnCode, Source: d.Source, Destination: d.Destination, Detection: detection, Time: d.Time, Packet: d.Packet, Size: len(m.Payload), Truncated: len(m.Payload) > MaxPayload, Payload: append([]byte{}, m.Payload[:min(len(m.Payload), MaxPayload)]...)}
	if sample.Truncated {
		s.warn("payload_preview_truncated")
	}
	if v != nil {
		sample.ServiceKey = v.Key
		sample.InstanceID = v.InstanceID
		v.Messages++
		v.Bytes += uint64(len(m.Payload))
		if d.Time.After(v.LastSeen) {
			v.LastSeen = d.Time
		}
		sec := d.Time.Unix()
		b := &v.buckets[(sec%5+5)%5]
		if b.Sec <= sec {
			if b.Sec != sec {
				*b = rateBucket{Sec: sec}
			}
			b.Count++
			b.Bytes += uint64(len(m.Payload))
		}
		key := fmt.Sprintf("%s/%04x/%s", v.Key, m.Method, d.Transport)
		member := s.members[key]
		if member == nil {
			if len(s.members) >= MaxMembers {
				s.warn("member_limit")
			} else {
				kind := "method"
				if m.Method&0x8000 != 0 {
					kind = "event"
				}
				member = &Member{Transport: d.Transport, Key: key, ServiceKey: v.Key, ServiceID: m.Service, InstanceID: v.InstanceID, MethodID: m.Method, Kind: kind}
				s.members[key] = member
			}
		}
		if member != nil {
			member.Messages++
			member.Bytes += uint64(len(m.Payload))
			if d.Time.After(member.LastSeen) {
				member.LastSeen = d.Time
			}
			switch m.Type {
			case 0, 1:
				member.Requests++
			case 0x80, 0x81:
				member.Responses++
			case 2:
				member.Notifications++
			}
			if m.ReturnCode != 0 || m.Type == 0x81 {
				member.Errors++
			}
		}
	}
	if len(s.samples) < MaxSamples {
		s.samples = append(s.samples, sample)
	} else {
		s.samples[s.samplePos] = sample
		s.samplePos = (s.samplePos + 1) % MaxSamples
		s.state.Evicted++
	}
}
func (s *Store) enrich(v Sample) Sample {
	names := s.config.Services[Hex(v.ServiceID)]
	v.ServiceName = names.Name
	v.MethodName = names.Methods[Hex(v.MethodID)]
	return v
}
func (s *Store) Sample(id uint64) (Sample, bool) {
	for _, v := range s.samples {
		if v.ID == id {
			v = s.enrich(v)
			v.Payload = append([]byte{}, v.Payload...)
			return v, true
		}
	}
	return Sample{}, false
}
func (s *Store) Snapshot(now time.Time) State {
	s.ExpireTCP(now)
	out := s.state
	out.TCPConnections, out.TCPBuffered = len(s.tcp), s.tcpBytes
	out.Warnings = map[string]uint64{}
	for k, v := range s.state.Warnings {
		out.Warnings[k] = v
	}
	out.Services = []Service{}
	out.Members = []Member{}
	out.Samples = []Sample{}
	out.Discovery = []DiscoveryEvent{}
	for _, v := range s.services {
		row := *v
		row.Endpoints = append([]Endpoint{}, v.Endpoints...)
		row.Name = s.config.Services[Hex(v.ServiceID)].Name
		if row.Status == "offered" && !now.Before(row.Expires) {
			row.Status = "expired"
		}
		for _, b := range v.buckets {
			if b.Sec <= now.Unix() && b.Sec > now.Unix()-5 {
				row.Hz += float64(b.Count) / 5
				row.BytesPerSecond += float64(b.Bytes) / 5
			}
		}
		out.Services = append(out.Services, row)
	}
	for _, v := range s.members {
		row := *v
		row.Name = s.config.Services[Hex(v.ServiceID)].Methods[Hex(v.MethodID)]
		out.Members = append(out.Members, row)
	}
	for _, v := range s.samples {
		row := s.enrich(v)
		row.Payload = append([]byte{}, v.Payload...)
		out.Samples = append(out.Samples, row)
	}
	for _, v := range s.discovery {
		row := v
		row.Endpoints = append([]Endpoint{}, v.Endpoints...)
		names := s.config.Services[Hex(v.Service)]
		row.ServiceName = names.Name
		if v.Eventgroup != nil {
			row.EventgroupName = names.Eventgroups[Hex(*v.Eventgroup)]
		}
		out.Discovery = append(out.Discovery, row)
	}
	sort.Slice(out.Services, func(i, j int) bool { return out.Services[i].Key < out.Services[j].Key })
	sort.Slice(out.Members, func(i, j int) bool { return out.Members[i].Key < out.Members[j].Key })
	sort.Slice(out.Samples, func(i, j int) bool { return out.Samples[i].ID > out.Samples[j].ID })
	sort.Slice(out.Discovery, func(i, j int) bool { return out.Discovery[i].ID > out.Discovery[j].ID })
	return out
}
