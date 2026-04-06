package steam

import (
	"log"

	proto "github.com/golang/protobuf/proto"
	"github.com/paralin/go-steam"
	"github.com/paralin/go-steam/protocol/gamecoordinator"
)

type GCMessageHandler func(appID uint32, msgType uint32, packet *gamecoordinator.GCPacket)

type GCRouter struct {
	client   *steam.Client
	handlers map[uint32]map[uint32]GCMessageHandler
}

func NewGCRouter(client *steam.Client) *GCRouter {
	return &GCRouter{
		client:   client,
		handlers: make(map[uint32]map[uint32]GCMessageHandler),
	}
}

func (r *GCRouter) RegisterHandler(appID uint32, msgType uint32, handler GCMessageHandler) {
	if r.handlers[appID] == nil {
		r.handlers[appID] = make(map[uint32]GCMessageHandler)
	}
	r.handlers[appID][msgType] = handler
}

func (r *GCRouter) HandleGCPacket(packet *gamecoordinator.GCPacket) {
	appHandlers, ok := r.handlers[packet.AppId]
	if !ok {
		return
	}

	handler, ok := appHandlers[packet.MsgType]
	if !ok {
		log.Printf("Unhandled GC message: AppID=%d MsgType=%d", packet.AppId, packet.MsgType)
		return
	}

	handler(packet.AppId, packet.MsgType, packet)
}

func (r *GCRouter) SendProtoMessage(appID uint32, msgType uint32, body proto.Message) {
	msg := gamecoordinator.NewGCMsgProtobuf(appID, msgType, body)
	r.client.GC.Write(msg)
}
