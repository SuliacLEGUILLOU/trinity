package network

import ( 
  "github.com/tomdionysus/trinity/util"
  "github.com/tomdionysus/trinity/packets"
  "github.com/tomdionysus/consistenthash"
  "crypto/tls"
  "errors"
  // "bytes"
  "encoding/gob"
  "time"
)

const (
  PeerStateDisconnected = iota
  PeerStateConnecting = iota
  PeerStateHandshake = iota
  PeerStateConnected = iota
  PeerStateDefib = iota
)

var PeerStateString map[uint]string = map[uint]string{
  PeerStateDisconnected: "PeerStateDisconnected",
  PeerStateConnecting: "PeerStateConnecting",
  PeerStateHandshake: "PeerStateHandshake",
  PeerStateConnected: "PeerStateConnected",
  PeerStateDefib: "PeerStateDefib",
}

type Peer struct {
  Logger *util.Logger
  Server *TLSServer
  
  Address string
  State uint

  Connection *tls.Conn
  HeartbeatTicker *time.Ticker

  Writer *gob.Encoder
  Reader *gob.Decoder

  LastHeartbeat time.Time

  ServerNetworkNode *consistenthash.ServerNetworkNode

  Replies map[[16]byte]chan(*packets.Packet)
}

func NewPeer(logger *util.Logger, server *TLSServer, address string) *Peer {
  inst := &Peer{
    Logger: logger,
    Address: address,
    State: PeerStateDisconnected,
    Server: server,
    LastHeartbeat: time.Now(),
    ServerNetworkNode: nil,
    Replies: map[[16]byte]chan(*packets.Packet){},
  }
  return inst
}

func NewConnectingPeer(logger *util.Logger, server *TLSServer, connection *tls.Conn) *Peer {
  inst := NewPeer(logger, server, connection.RemoteAddr().String())
  inst.Connection = connection
  inst.State = PeerStateHandshake
  return inst
}

func (me *Peer) Connect() error {
  me.State = PeerStateConnecting
  conn, err := tls.Dial("tcp", me.Address, &tls.Config{
    RootCAs: me.Server.CAPool.Pool,
    Certificates: []tls.Certificate{*me.Server.Certificate},
  })
  if err != nil {
    me.Logger.Error("Peer", "Cannot connect to %s: %s", me.Address, err.Error())
    me.Disconnect()
    return err
  }
  me.Connection = conn
  state := conn.ConnectionState()
  if len(state.PeerCertificates)==0 {
    me.Logger.Error("Peer", "Cannot connect to %s: Peer has no certificates", me.Address)
    me.Disconnect()
    return errors.New("Peer has no certificates")
  }
  me.State = PeerStateHandshake
  return nil
}


func (me *Peer) Disconnect() {
  if me.State == PeerStateConnected {
    me.State = PeerStateDisconnected
    if me.Connection!=nil { me.Connection.Close() }
    if me.HeartbeatTicker!=nil { me.HeartbeatTicker.Stop() }
    me.Logger.Info("Peer", "Disconnected: %s", me.Connection.RemoteAddr())
    delete(me.Server.Connections, me.ServerNetworkNode.ID)
  }
}

func (me *Peer) Start() error {
  if me.State != PeerStateHandshake {
    me.Logger.Error("Peer", "Cannot Start Client, Handshake not ready")
    return errors.New("Handshake not ready")
  }
  err := me.Connection.Handshake()
  if err!=nil {
    me.Logger.Error("Peer", "Peer TLS Handshake failed, disconnecting: %s",err.Error())
    me.Disconnect()
    return errors.New("TLS Handshake Failed")
  }
  state := me.Connection.ConnectionState()
  if len(state.PeerCertificates)==0 {
    me.Logger.Error("Peer", "Peer has no certificates, disconnecting")
    me.Disconnect()
    return errors.New("Peer sent no certificates")
  }
  sub := state.PeerCertificates[0].Subject.CommonName
  me.Logger.Info("Peer", "Connected to %s (%s) [%s]", me.Connection.RemoteAddr(), sub, Ciphers[me.Connection.ConnectionState().CipherSuite])
  me.State = PeerStateConnected

  me.Reader = gob.NewDecoder(me.Connection)
  me.Writer = gob.NewEncoder(me.Connection)

  go me.heartbeat()

  me.SendDistribution()
  me.SendPeerlist()

  go me.process()

  return nil
}

// Ping the Peer every second.
func (me *Peer) heartbeat() {
  me.HeartbeatTicker = time.NewTicker(time.Second)

  for {
    <- me.HeartbeatTicker.C

    // Check For Defib
    if time.Now().After(me.LastHeartbeat.Add(5 * time.Second)) {
      me.Logger.Warn("Peer", "%s: Peer Defib (no response for 5 seconds)", me.Connection.RemoteAddr())
      me.State = PeerStateDefib
    }

    switch me.State {
      case PeerStateConnected:
        err := me.SendPacket(packets.NewPacket(packets.CMD_HEARTBEAT,nil))
        if err!=nil {
          me.Logger.Error("Peer","Error Sending Heartbeat, disconnecting", me.Connection.RemoteAddr())
          me.Disconnect()
          return
        }
      case PeerStateDefib:
        if time.Now().After(me.LastHeartbeat.Add(10 * time.Second)) {
          me.Logger.Warn("Peer", "%s: Peer DOA (Defib for 5 seconds, disconnecting)", me.Connection.RemoteAddr())
          me.Disconnect()
          return
        }
    }

  }

}

func (me *Peer) process() {

  var packet packets.Packet

  for {
    // Read Command
    err := me.Reader.Decode(&packet)
    if err!=nil {
      if err.Error()=="EOF" {
        me.Logger.Debug("Peer", "%s: Peer Closed Connection", me.Connection.RemoteAddr())
      } else {
        me.Logger.Error("Peer", "%s: Error Reading: %s", me.Connection.RemoteAddr(), err.Error())
      }
      goto end
    }
    switch (packet.Command) {
      case packets.CMD_HEARTBEAT:
        me.LastHeartbeat = time.Now()
      case packets.CMD_DISTRIBUTION:
        me.Logger.Debug("Peer", "%s: CMD_DISTRIBUTION", me.Connection.RemoteAddr())
        servernetworknode := packet.Payload.(consistenthash.ServerNetworkNode)
        me.ServerNetworkNode = &servernetworknode
        me.Server.Connections[me.ServerNetworkNode.ID] = me
        err := me.Server.ServerNode.RegisterNode(me.ServerNetworkNode)
        if err!=nil {
          me.Logger.Error("Peer","Adding Node ID %02x Failed: %s", me.ServerNetworkNode.ID, err.Error())
        }
        me.Server.NotifyNewPeer(me)
      case packets.CMD_KVSTORE:
        me.Logger.Debug("Peer", "%s: CMD_KVSTORE", me.Connection.RemoteAddr())
        me.handleKVStorePacket(&packet)
      case packets.CMD_KVSTORE_ACK:
        me.Logger.Debug("Peer", "%s: CMD_KVSTORE_ACK", me.Connection.RemoteAddr())
        me.handleReply(&packet)
      case packets.CMD_KVSTORE_NOT_FOUND:
        me.Logger.Debug("Peer", "%s: CMD_KVSTORE_NOT_FOUND", me.Connection.RemoteAddr())
        me.handleReply(&packet)
      case packets.CMD_PEERLIST:
        peers := packet.Payload.([]string)
        me.Logger.Debug("Peer", "%s: CMD_PEERLIST (%d Peers)", me.Connection.RemoteAddr(), len(peers))
        for _, k := range peers {
          if me.Server.Listener.Addr().String() == k {
            me.Logger.Error("Peer", "%s: - Peer %s us of ourselves.", me.Connection.RemoteAddr(), k)       
          } else {
            if !me.Server.IsConnectedTo(k)  {
              me.Logger.Debug("Peer", "%s: - Connecting New Peer %s", me.Connection.RemoteAddr(), k)
              me.Server.ConnectTo(k)
            } else {
              me.Logger.Debug("Peer", "%s: - Already Connected to Peer %s", me.Connection.RemoteAddr(), k)
            }
          }
        }
      default:
        me.Logger.Warn("Peer", "%s: Unknown Packet Command %d", me.Connection.RemoteAddr(), packet.Command)
    }
  }
  end:

  me.Disconnect()
}

func (me *Peer) handleReply(packet *packets.Packet) {
  chn, found := me.Replies[packet.RequestID]
  if found {
    delete(me.Replies, packet.RequestID)
    chn <- packet
  } else {
    me.Logger.Warn("Peer", "%s: Unsolicited Reply to unknown packet %02X", me.Connection.RemoteAddr(), packet.RequestID)
  }
}

func (me *Peer) SendPeerlist() error {
  peers := []string{}
  for _, peer := range me.Server.Connections { 
    if peer.ServerNetworkNode!=nil && peer.ServerNetworkNode.ID != me.Server.ServerNode.ID { peers = append(peers, peer.ServerNetworkNode.HostAddr) }
  }
  packet := packets.NewPacket(packets.CMD_PEERLIST, peers)
  me.SendPacket(packet)
  return nil
}

func (me *Peer) SendDistribution() error {
  packet := packets.NewPacket(packets.CMD_DISTRIBUTION, me.Server.ServerNode.ServerNetworkNode)
  me.SendPacket(packet)
  return nil
}

func (me *Peer) SendPacket(packet *packets.Packet) error {
  err := me.Writer.Encode(packet)
  if err!=nil {
    me.Logger.Error("Peer", "Error Writing: %s", err.Error())
  }
  return err
}

func (me *Peer) SendPacketWaitReply(packet *packets.Packet, timeout time.Duration) (*packets.Packet, error) {
  me.Replies[packet.ID] = make(chan(*packets.Packet))
  me.SendPacket(packet)
  reply := <- me.Replies[packet.ID]
  me.Logger.Debug("Peer", "%s: Got Reply %02X for packet ID %02X", me.Connection.RemoteAddr(), reply.ID, packet.ID)
  return reply, nil
}

func (me *Peer) handleKVStorePacket(packet *packets.Packet) {
  kvpacket := packet.Payload.(packets.KVStorePacket)
  switch kvpacket.Command {
  case packets.CMD_KVSTORE_SET:
    me.handleKVStoreSet(&kvpacket, packet)
  case packets.CMD_KVSTORE_GET:
    me.handleKVStoreGet(&kvpacket, packet)
  case packets.CMD_KVSTORE_DELETE:
    me.handleKVStoreDelete(&kvpacket, packet)
  default:
    me.Logger.Error("Peer", "KVStorePacket: Unknown Command %d", packet.Command)
  }
}

func (me *Peer) handleKVStoreSet(packet *packets.KVStorePacket, request *packets.Packet) {
  me.Logger.Debug("Peer", "%s: KVStoreSet: %s = %s", me.Connection.RemoteAddr(), packet.Key, packet.Data)
  me.Server.KVStore.Set(
    packet.Key,
    packet.Data,
    packet.Flags,
    packet.ExpiresAt)

  response := packets.NewResponsePacket(packets.CMD_KVSTORE_ACK, request.ID, packet.Key)
    me.Logger.Debug("Peer", "%s: KVStoreSet: %s Acknowledge, replying", me.Connection.RemoteAddr(), packet.Key)
  me.SendPacket(response)
}

func (me *Peer) handleKVStoreGet(packet *packets.KVStorePacket, request *packets.Packet) {
  me.Logger.Debug("Peer", "%s: KVStoreGet: %s", me.Connection.RemoteAddr(), packet.Key)
  value, flags, found := me.Server.KVStore.Get(packet.Key)

  var response *packets.Packet

  if found {
    payload := packets.KVStorePacket{
      Command: packets.CMD_KVSTORE_GET,
      Key: packet.Key,
      Data: value,
      Flags: flags,
    }
    response = packets.NewResponsePacket(packets.CMD_KVSTORE_ACK, request.ID, payload)
    me.Logger.Debug("Peer", "%s: KVStoreGet: %s = %s, replying", me.Connection.RemoteAddr(), packet.Key, value)
  } else {
    response = packets.NewResponsePacket(packets.CMD_KVSTORE_NOT_FOUND, request.ID, packet.Key)
    me.Logger.Debug("Peer", "%s: KVStoreGet: %s Not found, replying", me.Connection.RemoteAddr(), packet.Key)
  }

  me.SendPacket(response)
}

func (me *Peer) handleKVStoreDelete(packet *packets.KVStorePacket, request *packets.Packet) {
  me.Logger.Debug("Peer", "%s: KVStoreDelete: %s", me.Connection.RemoteAddr(), packet.Key)
  found := me.Server.KVStore.Delete(packet.Key)
  
  var response *packets.Packet

  if found {
    response = packets.NewResponsePacket(packets.CMD_KVSTORE_ACK, request.ID, packet.Key)
    me.Logger.Debug("Peer", "%s: KVStoreDelete: %s Deleted, replying", me.Connection.RemoteAddr(), packet.Key)
  } else {
    response = packets.NewResponsePacket(packets.CMD_KVSTORE_NOT_FOUND, request.ID, packet.Key)
    me.Logger.Debug("Peer", "%s: KVStoreDelete: %s Not found, replying", me.Connection.RemoteAddr(), packet.Key)
  }

  me.SendPacket(response)
}





