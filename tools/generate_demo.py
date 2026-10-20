"""Generate synthetic RTPS packets for UI and regression tests, not vendor captures."""
from pathlib import Path
import struct
import ipaddress

ROOT = Path(__file__).resolve().parents[1]
LE = lambda fmt, *v: struct.pack('<' + fmt, *v)
BE = lambda fmt, *v: struct.pack('>' + fmt, *v)


def param(pid, value):
    value += b'\0' * (-len(value) % 4)
    return LE('HH', pid, len(value)) + value


def string(value):
    raw = value.encode() + b'\0'
    return LE('I', len(raw)) + raw


def parameters(*values):
    return b'\x00\x03\x00\x00' + b''.join(values) + LE('HH', 1, 0)


def rtps(prefix, writer, seq, payload):
    body = LE('HH', 0, 16) + bytes(4) + writer + LE('iI', 0, seq) + payload
    return b'RTPS\x02\x03\x01\x0f' + prefix + bytes([0x15, 5]) + LE('H', len(body)) + body


def checksum(data):
    if len(data) % 2:
        data += b'\0'
    n = sum(struct.unpack('>' + 'H' * (len(data)//2), data))
    n = (n & 0xffff) + (n >> 16)
    n += n >> 16
    return ~n & 0xffff


def ethernet(payload, source='192.0.2.10'):
    udp = BE('HHHH', 7411, 7412, len(payload)+8, 0) + payload
    ip = BE('BBHHHBBH', 0x45, 0, len(udp)+20, 1, 0, 64, 17, 0)
    ip += ipaddress.ip_address(source).packed + ipaddress.ip_address('192.0.2.20').packed
    ip = ip[:10] + BE('H', checksum(ip)) + ip[12:]
    return bytes.fromhex('0200000000020200000000010800') + ip + udp


def generate():
    pub = bytes.fromhex('010f0102030405060708090a')
    sub = bytes.fromhex('010f1112131415161718191a')
    packets = []
    for prefix, name in [(pub, 'synthetic-publisher'), (sub, 'synthetic-subscriber')]:
        payload = parameters(param(0x50, prefix + bytes.fromhex('000001c1')),
                             param(0x62, string(name)), param(0x0f, LE('I', 0)))
        packets.append((0, ethernet(rtps(prefix, bytes.fromhex('000100c2'), 1, payload))))
    topics = [('rt/telemetry/temperature', 'demo::Temperature', 0x10),
              ('rt/telemetry/imu', 'demo::Imu', 0x20),
              ('device/heartbeat', 'demo::Heartbeat', 0x30)]
    for topic, typename, entity in topics:
        for prefix, kind, writer in [(pub, 3, '000003c2'), (sub, 4, '000004c2')]:
            guid = prefix + bytes([0, 0, entity, kind])
            payload = parameters(param(0x5a, guid), param(5, string(topic)),
                                 param(7, string(typename)), param(0x1a, LE('III', 2, 0, 0)),
                                 param(0x1d, LE('I', 0)))
            packets.append((0, ethernet(rtps(prefix, bytes.fromhex(writer), entity, payload))))
    for seq in range(1, 41):
        for idx, (_, _, entity) in enumerate(topics):
            payload = b'\x00\x01\x00\x00' + LE('If', seq, 24.0 + idx + seq/100)
            packets.append((seq * 100000, ethernet(rtps(pub, bytes([0, 0, entity, 3]), seq, payload))))
    out = bytearray(LE('IHHIIII', 0xa1b2c3d4, 2, 4, 0, 0, 65536, 1))
    for micros, packet in packets:
        out += LE('IIII', 1792418400 + micros//1000000, micros % 1000000, len(packet), len(packet)) + packet
    dest = ROOT/'frontend/public/demo.pcap'
    dest.write_bytes(out)
    print(f'{dest}: {len(packets)} synthetic packets, 120 user samples')


if __name__ == '__main__':
    generate()
