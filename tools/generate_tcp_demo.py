"""SOME/IP TCP + UDP fixture with SD, segmentation, reordering and retransmission."""
from pathlib import Path
import struct, ipaddress
from generate_someip_demo import message, endpoint, entry, sd, frame, BE, LE, checksum, ROOT

def tcp(payload=b'', source='192.0.2.30',dest='192.0.2.40',sport=30501,dport=40000,seq=100,flags=0x18):
    segment=BE('HHIIBBHHH',sport,dport,seq,0,5<<4,flags,65535,0,0)+payload
    pseudo=ipaddress.ip_address(source).packed+ipaddress.ip_address(dest).packed+BE('BBH',0,6,len(segment))
    segment=segment[:16]+BE('H',checksum(pseudo+segment))+segment[18:]
    ip=BE('BBHHHBBH',0x45,0,len(segment)+20,1,0,64,6,0)+ipaddress.ip_address(source).packed+ipaddress.ip_address(dest).packed
    ip=ip[:10]+BE('H',checksum(ip))+ip[12:]
    return bytes.fromhex('0200000000020200000000010800')+ip+segment

def generate():
    packets=[]
    def add(b):packets.append(b)
    add(frame(sd(entry(1,0x1234,1,options=2),endpoint('192.0.2.30',30501,6)+endpoint('192.0.2.30',30501,17)),dest='224.244.224.245',sport=30490,dport=30490))
    add(tcp(source='192.0.2.40',dest='192.0.2.30',sport=40000,dport=30501,seq=99,flags=2))
    add(tcp(seq=499,flags=0x12))
    request=message(0x1234,1,b'get temperature',kind=0)
    for offset,part in [(8,request[8:]),(0,request[:8]),(0,request)]:
        add(tcp(part,source='192.0.2.40',dest='192.0.2.30',sport=40000,dport=30501,seq=100+offset))
    response=message(0x1234,1,BE('f',25.5),kind=0x80)
    event=message(0x1234,0x8001,b'TCP event',kind=2,client=0)
    both=response+event
    add(tcp(both[:7],seq=500));add(tcp(both[7:],seq=507));add(tcp(both,seq=500))
    big=message(0x1234,0x8002,bytes(range(256))*400,kind=2,client=0)
    for offset in range(0,len(big),1400):add(tcp(big[offset:offset+1400],seq=500+len(both)+offset))
    add(tcp(seq=500+len(both)+len(big),flags=0x11))
    add(tcp(source='192.0.2.40',dest='192.0.2.30',sport=40000,dport=30501,seq=100+len(request),flags=0x11))
    add(frame(message(0x1234,0x8001,b'UDP event',kind=2,client=0)))
    out=bytearray(LE('IHHIIII',0xa1b2c3d4,2,4,0,0,65536,1))
    for i,p in enumerate(packets):out+=LE('IIII',1792418400,i*1000,len(p),len(p))+p
    (ROOT/'frontend/public/tcp-demo.pcap').write_bytes(out)
    print('tcp-demo.pcap',len(packets),'packets; 4 TCP + 1 UDP user messages')
if __name__=='__main__':generate()
