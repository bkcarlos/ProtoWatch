package watch

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bkcarlos/ProtoWatch/internal/someip"
)

const MaxSamples = 256
const MaxPayload = 16 * 1024
const MaxEndpoints = 4096
const MaxParticipants = 1024

type Participant struct {
	GUID              string    `json:"guid"`
	Name              string    `json:"name"`
	Vendor            string    `json:"vendor"`
	Version           string    `json:"version"`
	Source            string    `json:"source"`
	Domain            *uint32   `json:"domain,omitempty"`
	DiscoveryObserved bool      `json:"discovery_observed"`
	LastSeen          time.Time `json:"last_seen"`
}
type Endpoint struct {
	GUID        string    `json:"guid"`
	Kind        string    `json:"kind"`
	Topic       string    `json:"topic"`
	Type        string    `json:"type"`
	Reliability string    `json:"reliability"`
	Durability  string    `json:"durability"`
	Source      string    `json:"source"`
	Samples     uint64    `json:"samples"`
	Bytes       uint64    `json:"bytes"`
	LastSeen    time.Time `json:"last_seen"`
	buckets     [5]bucket
}
type bucket struct {
	Sec            int64
	Samples, Bytes uint64
}
type Sample struct {
	ID        uint64    `json:"id"`
	Writer    string    `json:"writer"`
	Sequence  string    `json:"sequence"`
	Topic     string    `json:"topic"`
	TopicKey  string    `json:"topic_key"`
	Type      string    `json:"type"`
	Time      time.Time `json:"time"`
	Packet    uint64    `json:"packet"`
	Source    string    `json:"source"`
	Size      int       `json:"size"`
	Truncated bool      `json:"truncated"`
	Encoding  string    `json:"encoding"`
	Payload   []byte    `json:"-"`
}
type Topic struct {
	Key            string    `json:"key"`
	Domain         string    `json:"domain"`
	Name           string    `json:"name"`
	Type           string    `json:"type"`
	ROSName        string    `json:"ros_name,omitempty"`
	Writers        int       `json:"writers"`
	Readers        int       `json:"readers"`
	Samples        uint64    `json:"samples"`
	Bytes          uint64    `json:"bytes"`
	Hz             float64   `json:"hz"`
	BytesPerSecond float64   `json:"bytes_per_second"`
	LastSeen       time.Time `json:"last_seen"`
}
type State struct {
	ImportBytes    int64             `json:"import_bytes"`
	ImportTotal    int64             `json:"import_total"`
	SomeIP         someip.State      `json:"someip"`
	Source         string            `json:"source"`
	Mode           string            `json:"mode"`
	Status         string            `json:"status"`
	Error          string            `json:"error,omitempty"`
	Started        time.Time         `json:"started"`
	Observed       time.Time         `json:"observed"`
	Packets        uint64            `json:"packets"`
	RTPSPackets    uint64            `json:"rtps_packets"`
	WireBytes      uint64            `json:"wire_bytes"`
	SampleCount    uint64            `json:"sample_count"`
	DuplicateCount uint64            `json:"duplicate_count"`
	DiscoveryCount uint64            `json:"discovery_count"`
	ControlCount   uint64            `json:"control_count"`
	KernelDrops    *uint64           `json:"kernel_drops"`
	Evicted        uint64            `json:"evicted"`
	Warnings       map[string]uint64 `json:"warnings"`
	Participants   []Participant     `json:"participants"`
	Endpoints      []Endpoint        `json:"endpoints"`
	Topics         []Topic           `json:"topics"`
	Samples        []Sample          `json:"samples"`
}
type Store struct {
	someip       *someip.Store
	mu           sync.Mutex
	state        State
	participants map[string]*Participant
	endpoints    map[string]*Endpoint
	samples      []Sample
	seen         map[string]struct{}
	seenOrder    []string
	seenPos      int
	samplePos    int
}

func NewStore() *Store { s := &Store{someip: someip.NewStore()}; s.Reset("", "idle"); return s }
func (s *Store) Reset(source, mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.someip.Reset()
	s.state = State{Source: source, Mode: mode, Status: "idle", Started: time.Now().UTC(), Warnings: map[string]uint64{}}
	s.participants = map[string]*Participant{}
	s.endpoints = map[string]*Endpoint{}
	s.samples = nil
	s.seen = map[string]struct{}{}
	s.seenOrder = nil
	s.seenPos = 0
	s.samplePos = 0
}
func (s *Store) Status(status string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status == "complete" || status == "stopped" || status == "error" {
		s.someip.FinishTCP()
	}
	s.state.Status = status
	if err != nil {
		s.state.Error = err.Error()
	}
}
func (s *Store) ImportProgress(read, total int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.ImportBytes, s.state.ImportTotal = read, total
}
func (s *Store) Drops(n uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.KernelDrops == nil {
		v := uint64(0)
		s.state.KernelDrops = &v
	}
	*s.state.KernelDrops += n
}

func (s *Store) participant(prefix string, t time.Time) *Participant {
	p := s.participants[prefix]
	if p == nil {
		if len(s.participants) >= MaxParticipants {
			s.state.Warnings["participant_limit"]++
			return nil
		}
		p = &Participant{GUID: prefix + "000001c1"}
		s.participants[prefix] = p
	}
	if t.After(p.LastSeen) {
		p.LastSeen = t
	}
	return p
}
func (s *Store) endpoint(guid string, t time.Time) *Endpoint {
	e := s.endpoints[guid]
	if e == nil {
		if len(s.endpoints) >= MaxEndpoints {
			s.state.Warnings["endpoint_limit"]++
			return nil
		}
		e = &Endpoint{GUID: guid}
		s.endpoints[guid] = e
	}
	if t.After(e.LastSeen) {
		e.LastSeen = t
	}
	return e
}
func (s *Store) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.state
	v.Warnings = map[string]uint64{}
	for k, n := range s.state.Warnings {
		v.Warnings[k] = n
	}
	if v.KernelDrops != nil {
		n := *v.KernelDrops
		v.KernelDrops = &n
	}
	v.Participants = []Participant{}
	v.Endpoints = []Endpoint{}
	v.Topics = []Topic{}
	v.Samples = []Sample{}
	for _, p := range s.participants {
		v.Participants = append(v.Participants, *p)
	}
	topics := map[string]*Topic{}
	now := v.Observed.Unix()
	if v.Mode == "live" && v.Status == "running" {
		now = time.Now().Unix()
	}
	for _, e := range s.endpoints {
		v.Endpoints = append(v.Endpoints, *e)
		name := e.Topic
		if name == "" {
			name = "未知 Topic · " + e.GUID
		}
		key, domain := s.topicKey(e)
		t := topics[key]
		if t == nil {
			t = &Topic{Key: key, Domain: domain, Name: name, Type: e.Type}
			if strings.HasPrefix(name, "rt/") {
				t.ROSName = "/" + strings.TrimPrefix(name, "rt/")
			}
			topics[key] = t
		}
		if e.Kind == "writer" {
			t.Writers++
		} else if e.Kind == "reader" {
			t.Readers++
		}
		t.Samples += e.Samples
		t.Bytes += e.Bytes
		if e.LastSeen.After(t.LastSeen) {
			t.LastSeen = e.LastSeen
		}
		for _, b := range e.buckets {
			if b.Sec <= now && b.Sec > now-5 {
				t.Hz += float64(b.Samples) / 5
				t.BytesPerSecond += float64(b.Bytes) / 5
			}
		}
	}
	for _, t := range topics {
		v.Topics = append(v.Topics, *t)
	}
	for _, sample := range s.samples {
		v.Samples = append(v.Samples, s.enrich(sample))
	}
	sort.Slice(v.Samples, func(i, j int) bool { return v.Samples[i].ID > v.Samples[j].ID })
	sort.Slice(v.Topics, func(i, j int) bool {
		if v.Topics[i].Samples != v.Topics[j].Samples {
			return v.Topics[i].Samples > v.Topics[j].Samples
		}
		return v.Topics[i].Name < v.Topics[j].Name
	})
	sort.Slice(v.Participants, func(i, j int) bool { return v.Participants[i].GUID < v.Participants[j].GUID })
	sort.Slice(v.Endpoints, func(i, j int) bool { return v.Endpoints[i].GUID < v.Endpoints[j].GUID })
	snapshotTime := v.Observed
	if v.Mode == "live" && v.Status == "running" {
		snapshotTime = time.Now()
	}
	v.SomeIP = s.someip.Snapshot(snapshotTime)
	return v
}
func (s *Store) enrich(v Sample) Sample {
	if e := s.endpoints[v.Writer]; e != nil {
		v.Topic = e.Topic
		v.Type = e.Type
		v.TopicKey, _ = s.topicKey(e)
	}
	return v
}

func (s *Store) topicKey(e *Endpoint) (string, string) {
	domain := "未知"
	scope := e.GUID[:24]
	if p := s.participants[scope]; p != nil && p.Domain != nil {
		domain = fmt.Sprint(*p.Domain)
		scope = "domain:" + domain
	}
	name := e.Topic
	if name == "" {
		name = e.GUID
	}
	return scope + "/" + name + "/" + e.Type, domain
}
func (s *Store) Sample(id uint64) (Sample, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.samples {
		if v.ID == id {
			return s.enrich(v), true
		}
	}
	return Sample{}, false
}
func vendor(id string) string {
	switch id {
	case "010f":
		return "Fast DDS (010f)"
	case "0110":
		return "Cyclone DDS (0110)"
	case "0101":
		return "RTI (0101)"
	}
	return id
}
func encoding(b []byte) string {
	if len(b) < 4 {
		return "missing_encapsulation"
	}
	switch hex.EncodeToString(b[:2]) {
	case "0000":
		return "CDR_BE"
	case "0001":
		return "CDR_LE"
	case "0002":
		return "PL_CDR_BE"
	case "0003":
		return "PL_CDR_LE"
	case "0006", "0007", "0008", "0009", "000a", "000b":
		return "XCDR2"
	}
	return "unknown"
}
