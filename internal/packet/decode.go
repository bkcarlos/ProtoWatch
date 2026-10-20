// Package packet decodes the shared network envelope without interpreting application protocols.
package packet

import (
	"fmt"
	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"time"
)

type Datagram struct {
	Source, Destination, SourceIP, DestinationIP string
	SourcePort, DestinationPort                  uint16
	Transport                                    string
	TCPSeq                                       uint32
	TCPSYN, TCPACK, TCPFIN, TCPRST               bool
	Payload                                      []byte
	Time                                         time.Time
	Packet                                       uint64
}

func Decode(frame []byte, ci gopacket.CaptureInfo, link layers.LinkType) (Datagram, string) {
	d := Datagram{Time: ci.Timestamp}
	if ci.CaptureLength < ci.Length {
		return d, "capture_truncated"
	}
	p := gopacket.NewPacket(frame, link, gopacket.DecodeOptions{Lazy: true, NoCopy: true, SkipDecodeRecovery: false})
	if p.Layer(layers.LayerTypeIPv6) != nil {
		return d, "ipv6_unsupported"
	}
	layer := p.Layer(layers.LayerTypeIPv4)
	if layer == nil {
		return d, ""
	}
	ip := layer.(*layers.IPv4)
	if int(ip.Length) > len(ip.Contents)+len(ip.Payload) {
		return d, "malformed_packet"
	}
	if ip.FragOffset != 0 || ip.Flags&layers.IPv4MoreFragments != 0 {
		return d, "ip_fragment_unsupported"
	}
	d.SourceIP, d.DestinationIP = ip.SrcIP.String(), ip.DstIP.String()
	if u := p.Layer(layers.LayerTypeUDP); u != nil {
		udp := u.(*layers.UDP)
		d.Transport = "UDP"
		d.SourcePort, d.DestinationPort = uint16(udp.SrcPort), uint16(udp.DstPort)
		d.Payload = udp.Payload
	} else if t := p.Layer(layers.LayerTypeTCP); t != nil {
		tcp := t.(*layers.TCP)
		d.Transport = "TCP"
		d.TCPSeq, d.TCPSYN, d.TCPACK, d.TCPFIN, d.TCPRST = tcp.Seq, tcp.SYN, tcp.ACK, tcp.FIN, tcp.RST
		if tcp.DataOffset < 5 || int(ip.Length) != int(ip.IHL)*4+len(tcp.Contents)+len(tcp.Payload) {
			return d, "malformed_packet"
		}
		d.SourcePort, d.DestinationPort = uint16(tcp.SrcPort), uint16(tcp.DstPort)
		d.Payload = tcp.Payload
	}
	// Stop before application dissectors: unrelated DNS etc. must not affect envelope validity.
	if d.Transport == "UDP" && (int(ip.Length) < int(ip.IHL)*4+8 || len(d.Payload) != int(p.Layer(layers.LayerTypeUDP).(*layers.UDP).Length)-8) {
		return d, "malformed_packet"
	}
	d.Source = fmt.Sprintf("%s:%d", d.SourceIP, d.SourcePort)
	d.Destination = fmt.Sprintf("%s:%d", d.DestinationIP, d.DestinationPort)
	return d, ""
}
