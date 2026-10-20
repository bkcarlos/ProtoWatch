package packet

import (
	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"testing"
	"time"
)

func TestVLANAndUDPEnvelope(t *testing.T) {
	eth := &layers.Ethernet{SrcMAC: []byte{0, 1, 2, 3, 4, 5}, DstMAC: []byte{0, 1, 2, 3, 4, 6}, EthernetType: layers.EthernetTypeDot1Q}
	vlan := &layers.Dot1Q{VLANIdentifier: 10, Type: layers.EthernetTypeIPv4}
	ip := &layers.IPv4{Version: 4, TTL: 64, Protocol: layers.IPProtocolUDP, SrcIP: []byte{192, 0, 2, 1}, DstIP: []byte{192, 0, 2, 2}}
	udp := &layers.UDP{SrcPort: 30501, DstPort: 40000}
	udp.SetNetworkLayerForChecksum(ip)
	b := gopacket.NewSerializeBuffer()
	err := gopacket.SerializeLayers(b, gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}, eth, vlan, ip, udp, gopacket.Payload("payload"))
	if err != nil {
		t.Fatal(err)
	}
	frame := b.Bytes()
	ci := gopacket.CaptureInfo{Timestamp: time.Unix(100, 0), Length: len(frame), CaptureLength: len(frame)}
	d, w := Decode(frame, ci, layers.LinkTypeEthernet)
	if w != "" || d.Source != "192.0.2.1:30501" || d.Destination != "192.0.2.2:40000" || string(d.Payload) != "payload" {
		t.Fatal(d, w)
	}
	ci.Length++
	if _, w = Decode(frame, ci, layers.LinkTypeEthernet); w != "capture_truncated" {
		t.Fatal(w)
	}
}

func TestTCPSequenceAndFlags(t *testing.T) {
	eth := &layers.Ethernet{SrcMAC: []byte{0, 1, 2, 3, 4, 5}, DstMAC: []byte{0, 1, 2, 3, 4, 6}, EthernetType: layers.EthernetTypeIPv4}
	ip := &layers.IPv4{Version: 4, TTL: 64, Protocol: layers.IPProtocolTCP, SrcIP: []byte{192, 0, 2, 1}, DstIP: []byte{192, 0, 2, 2}}
	tcp := &layers.TCP{SrcPort: 30501, DstPort: 40000, Seq: 0xfffffffe, SYN: true, ACK: true, FIN: true}
	tcp.SetNetworkLayerForChecksum(ip)
	b := gopacket.NewSerializeBuffer()
	if err := gopacket.SerializeLayers(b, gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}, eth, ip, tcp, gopacket.Payload("payload")); err != nil {
		t.Fatal(err)
	}
	frame := b.Bytes()
	ci := gopacket.CaptureInfo{Length: len(frame), CaptureLength: len(frame)}
	d, w := Decode(frame, ci, layers.LinkTypeEthernet)
	if w != "" || d.Transport != "TCP" || d.TCPSeq != 0xfffffffe || !d.TCPSYN || !d.TCPACK || !d.TCPFIN || string(d.Payload) != "payload" {
		t.Fatal(d, w)
	}
}
