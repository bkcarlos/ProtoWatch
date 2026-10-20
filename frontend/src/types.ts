export interface Participant {
  guid: string
  name: string
  vendor: string
  version: string
  source: string
  domain?: number
  discovery_observed: boolean
  last_seen: string
}
export interface Endpoint {
  guid: string
  kind: string
  topic: string
  type: string
  reliability: string
  durability: string
  source: string
  samples: number
  bytes: number
  last_seen: string
}
export interface Topic {
  key: string
  domain: string
  name: string
  type: string
  ros_name?: string
  writers: number
  readers: number
  samples: number
  bytes: number
  hz: number
  bytes_per_second: number
  last_seen: string
}
export interface Sample {
  id: number
  writer: string
  sequence: string
  topic: string
  topic_key: string
  type: string
  time: string
  packet: number
  source: string
  size: number
  truncated: boolean
  encoding: string
}
export interface Observation {
  import_bytes: number
  import_total: number
  someip: SomeIPState
  source: string
  mode: 'idle' | 'file' | 'live'
  status: 'idle' | 'running' | 'importing' | 'complete' | 'stopped' | 'error'
  error?: string
  started: string
  observed: string
  packets: number
  rtps_packets: number
  wire_bytes: number
  sample_count: number
  duplicate_count: number
  discovery_count: number
  control_count: number
  kernel_drops: number | null
  evicted: number
  warnings: Record<string, number>
  participants: Participant[]
  endpoints: Endpoint[]
  topics: Topic[]
  samples: Sample[]
}
export interface NetworkInterface { name: string; addresses: string[]; up: boolean; loopback: boolean }
export interface InterfacesResponse { interfaces: NetworkInterface[]; live_supported: boolean; version: string; max_import_bytes: number }
export interface SampleDetail { sample: Sample; hex: string; payload_base64: string; decode_status: string }
export type Tab = 'topics' | 'participants' | 'endpoints' | 'diagnostics'

export type Protocol = 'dds' | 'someip'
export type SomeIPTab = 'services' | 'members' | 'discovery' | 'diagnostics'
export interface SomeIPEndpoint { address: string; port: number; transport: string; multicast: boolean }
export interface SomeIPService {
  key: string; service_id: number; instance_id: number | null; name: string; major: number; minor: number | null;
  provider: string; endpoints: SomeIPEndpoint[]; status: string; ttl: number; expires: string; last_seen: string;
  messages: number; bytes: number; hz: number; bytes_per_second: number;
}
export interface SomeIPMember {
  transport: string; key: string; service_key: string; service_id: number; instance_id: number | null; method_id: number;
  name: string; kind: string; messages: number; requests: number; responses: number; notifications: number; errors: number; bytes: number; last_seen: string;
}
export interface SomeIPSample {
  transport: string; id: number; service_key: string; service_id: number; instance_id: number | null; service_name: string;
  method_id: number; method_name: string; client_id: number; session_id: number; interface_version: number;
  message_type: string; return_code: number; source: string; destination: string; detection: string;
  time: string; packet: number; size: number; truncated: boolean;
}
export interface SomeIPDiscovery {
  id: number; kind: string; service_id: number; instance_id: number; major: number; minor: number; ttl: number;
  eventgroup_id?: number; counter: number; endpoints: SomeIPEndpoint[]; service_name: string; eventgroup_name: string;
  source: string; destination: string; time: string; packet: number; session_id: number; reboot: boolean; unicast: boolean;
}
export interface SomeIPState {
  tcp_messages: number; udp_messages: number; tcp_connections: number; tcp_buffered: number;
  packets: number; message_count: number; discovery_count: number; requests: number; responses: number;
  notifications: number; errors: number; evicted: number; discovery_evicted: number; warnings: Record<string, number>;
  services: SomeIPService[]; members: SomeIPMember[]; samples: SomeIPSample[]; discovery: SomeIPDiscovery[];
}
export interface SomeIPDetail { sample: SomeIPSample; hex: string; payload_base64: string; decode_status: string }

export interface ServerPCAPFile { name: string; size: number; modified: string }
export interface ServerPCAPFiles { directory: string; files: ServerPCAPFile[]; truncated: boolean }
