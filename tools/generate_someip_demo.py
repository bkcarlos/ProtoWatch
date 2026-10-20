"""Synthetic SOME/IP + SD fixtures. No vendor compatibility claim."""
from pathlib import Path
import struct, ipaddress
from generate_demo import BE, LE, checksum, ROOT

def message(service, method, payload=b'', kind=0, client=0x42, session=1, interface=1, code=0):
    return BE('HHIHHBBBB',service,method,len(payload)+8,client,session,1,interface,kind,code)+payload

def endpoint(address,port,proto=17):
    return BE('HB',9,4)+bytes(1)+ipaddress.ip_address(address).packed+BE('BBH',0,proto,port)

def entry(kind,service,instance,ttl=30,options=0,group=0,counter=0):
    tail=BE('I',0) if kind in (0,1) else BE('BBH',0,counter,group)
    return BE('BBBBHHB',kind,0,0,options<<4,service,instance,1)+ttl.to_bytes(3,'big')+tail

def sd(entries,options=b'',session=1):
    payload=bytes([0xc0,0,0,0])+BE('I',len(entries))+entries+BE('I',len(options))+options
    return message(0xffff,0x8100,payload,2,0,session)

def frame(payload,source='192.0.2.30',dest='192.0.2.40',sport=30501,dport=40000):
    udp=BE('HHHH',sport,dport,len(payload)+8,0)+payload
    ip=BE('BBHHHBBH',0x45,0,len(udp)+20,1,0,64,17,0)+ipaddress.ip_address(source).packed+ipaddress.ip_address(dest).packed
    ip=ip[:10]+BE('H',checksum(ip))+ip[12:]
    return bytes.fromhex('0200000000020200000000010800')+ip+udp

def generate():
    packets=[]
    def discovery(t,e,options=b'',client=False,session=1):
        source='192.0.2.40' if client else '192.0.2.30'
        packets.append((t,frame(sd(e,options,session),source,'224.244.224.245' if not client else '192.0.2.30',30490,30490)))
    discovery(0,entry(0,0x1234,0xffff),client=True)
    discovery(10000,entry(1,0x1234,1,options=1),endpoint('192.0.2.30',30501))
    discovery(20000,entry(1,0x2000,2,options=1),endpoint('192.0.2.30',30502),session=2)
    discovery(30000,entry(1,0x3000,3,ttl=1,options=1),endpoint('192.0.2.30',30503),session=3)
    discovery(40000,entry(6,0x1234,1,options=1,group=1,counter=2),endpoint('192.0.2.40',40001),client=True,session=2)
    discovery(50000,entry(7,0x1234,1,group=1,counter=2),session=4)
    for seq in range(1,31):
        packets.append((seq*100000,frame(message(0x1234,1,BE('I',seq),session=seq), '192.0.2.40','192.0.2.30',40000,30501)))
        packets.append((seq*100000+1000,frame(message(0x1234,1,BE('f',24+seq/100),kind=0x81 if seq==15 else 0x80,session=seq,code=1 if seq==15 else 0))))
        packets.append((seq*100000+2000,frame(message(0x1234,0x8001,BE('If',seq,24+seq/100),kind=2,client=0,session=seq),dport=40001)))
    packets.append((3200000,frame(message(0x1234,0x8001,BE('If',31,24.31),kind=2,client=0,session=31)+message(0x1234,0x8001,BE('If',32,24.32),kind=2,client=0,session=32),dport=40001)))
    discovery(3300000,entry(7,0x1234,1,ttl=0,group=2),session=5)
    discovery(3400000,entry(6,0x1234,1,ttl=0,group=1,counter=2),client=True,session=3)
    discovery(4000000,entry(1,0x2000,2,ttl=0),session=6)
    packets.append((4300000,frame(message(0x1234,0x8001,BE('I',1)+b'segment',kind=0x22,client=0,session=33),dport=40001)))
    packets.append((4500000,frame(message(0x4000,1,b'unknown instance',session=34),'192.0.2.40','192.0.2.30',40000,30504)))
    def write(name,records):
        out=bytearray(LE('IHHIIII',0xa1b2c3d4,2,4,0,0,65536,1))
        for t,p in sorted(records,key=lambda x:x[0]):out+=LE('IIII',1792418400+t//1000000,t%1000000,len(p),len(p))+p
        (ROOT/'frontend/public'/name).write_bytes(out)
        print(name,len(records),'packets')
    write('someip-demo.pcap',packets)
    dds=(ROOT/'frontend/public/demo.pcap').read_bytes();pos=24;mixed=list(packets)
    while pos<len(dds):
        sec,usec,n,_=struct.unpack_from('<IIII',dds,pos);pos+=16;mixed.append(((sec-1792418400)*1000000+usec,dds[pos:pos+n]));pos+=n
    write('mixed-demo.pcap',mixed)
if __name__=='__main__':generate()
