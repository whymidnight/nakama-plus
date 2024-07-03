// Copyright 2024 The Bombus Authors
//
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package server

import (
	"time"

	"github.com/doublemo/nakama-kit/pb"
	"github.com/gofrs/uuid/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *LocalPeer) onRequest(frame *pb.Frame) {
	w := func(response *pb.ResponseWriter) error {
		if len(frame.Inbox) < 1 {
			return nil
		}

		endpoint, ok := s.members.Load(frame.Node)
		if !ok {
			return status.Error(codes.Aborted, "the remote node does not exist")
		}

		frame := &pb.Frame{
			Id:        uuid.Must(uuid.NewV4()).String(),
			Inbox:     frame.Inbox,
			Node:      s.endpoint.Name(),
			Timestamp: timestamppb.New(time.Now().UTC()),
			Payload:   &pb.Frame_ResponseWriter{ResponseWriter: response},
		}

		b, err := proto.Marshal(frame)
		if err != nil {
			return status.Error(codes.Aborted, err.Error())
		}

		if err := s.memberlist.SendReliable(endpoint.MemberlistNode(), b); err != nil {
			return status.Error(codes.Aborted, err.Error())
		}
		//s.metrics.PeerSent(int64(len(b)))
		return nil
	}

	request := frame.GetRequest()
	switch request.Payload.(type) {
	case *pb.Request_Ping:
		w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_Pong{Pong: "PONG"}})
		return

	case *pb.Request_Out:
		s.handler(nil, request.GetOut())
		return

	case *pb.Request_SingleSocket:
		s.singleSocket(request.GetSingleSocket())
		return

	case *pb.Request_Disconnect:
		s.disconnect(request.GetDisconnect())
		return

	case *pb.Request_PartyJoinRequest:
		partyJoinRequest := request.GetPartyJoinRequest()
		userID := uuid.FromStringOrNil(partyJoinRequest.Presence.GetUserID())
		sessionID := uuid.FromStringOrNil(partyJoinRequest.Presence.GetSessionID())
		stream := partyJoinRequest.Presence.GetStream()
		meta := partyJoinRequest.Presence.GetMeta()
		presence := &Presence{
			ID:     PresenceID{Node: partyJoinRequest.Presence.GetNode(), SessionID: sessionID},
			UserID: userID,
		}

		if len(stream) > 0 {
			presence.Stream = pb2PresenceStream(stream[0])
		}

		if len(meta) > 0 {
			presence.Meta = pb2PresenceMeta(meta[0])
		}
		ok, err := s.partyRegistry.PartyJoinRequest(s.ctx, uuid.FromStringOrNil(partyJoinRequest.Id), s.endpoint.Name(), presence)
		if err != nil {
			w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_Envelope{Envelope: newEnvelopeError(err)}})
			return
		}
		w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_PartyJoinRequest{PartyJoinRequest: ok}})
		return
	case *pb.Request_PartyPromote:
		promote := request.GetPartyPromote()
		err := s.partyRegistry.PartyPromote(s.ctx, uuid.FromStringOrNil(promote.Id), s.endpoint.Name(), promote.GetSessionID(), promote.GetFromNode(), promote.GetUserPresence())
		if err != nil {
			w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_Envelope{Envelope: newEnvelopeError(err)}})
			return
		}
		w(&pb.ResponseWriter{})
		return
	case *pb.Request_PartyAccept:
		accept := request.GetPartyAccept()
		err := s.partyRegistry.PartyAccept(s.ctx, uuid.FromStringOrNil(accept.Id), s.endpoint.Name(), accept.GetSessionID(), accept.GetFromNode(), accept.GetUserPresence())
		if err != nil {
			w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_Envelope{Envelope: newEnvelopeError(err)}})
			return
		}
		w(&pb.ResponseWriter{})
		return
	case *pb.Request_PartyRemove:
		remove := request.GetPartyRemove()
		err := s.partyRegistry.PartyRemove(s.ctx, uuid.FromStringOrNil(remove.Id), s.endpoint.Name(), remove.GetSessionID(), remove.GetFromNode(), remove.GetUserPresence())
		if err != nil {
			w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_Envelope{Envelope: newEnvelopeError(err)}})
			return
		}
		w(&pb.ResponseWriter{})
		return
	case *pb.Request_PartyClose:
		partyClose := request.GetPartyClose()
		err := s.partyRegistry.PartyClose(s.ctx, uuid.FromStringOrNil(partyClose.Id), s.endpoint.Name(), partyClose.GetSessionID(), partyClose.GetFromNode())
		if err != nil {
			w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_Envelope{Envelope: newEnvelopeError(err)}})
			return
		}
		w(&pb.ResponseWriter{})
		return
	case *pb.Request_PartyJoinRequestList:
		partyJoinRequestList := request.GetPartyJoinRequestList()
		list, err := s.partyRegistry.PartyJoinRequestList(s.ctx, uuid.FromStringOrNil(partyJoinRequestList.Id), s.endpoint.Name(), partyJoinRequestList.GetSessionID(), partyJoinRequestList.GetFromNode())
		if err != nil {
			w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_Envelope{Envelope: newEnvelopeError(err)}})
			return
		}
		w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_PartyJoinRequestList{
			PartyJoinRequestList: &pb.Party_JoinRequestListReply{
				UserPresence: list,
			},
		}})
		return
	case *pb.Request_PartyMatchmakerAdd:
		matchmakerAdd := request.GetPartyMatchmakerAdd()
		ticket, ids, err := s.partyRegistry.PartyMatchmakerAdd(
			s.ctx,
			uuid.FromStringOrNil(matchmakerAdd.Id),
			s.endpoint.Name(), matchmakerAdd.GetSessionID(),
			matchmakerAdd.GetFromNode(),
			matchmakerAdd.GetQuery(),
			int(matchmakerAdd.GetMinCount()),
			int(matchmakerAdd.GetMaxCount()),
			int(matchmakerAdd.GetCountMultiple()), matchmakerAdd.GetStringProperties(), matchmakerAdd.GetNumericProperties())
		if err != nil {
			w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_Envelope{Envelope: newEnvelopeError(err)}})
			return
		}

		presenceIDs := make([]*pb.PresenceID, len(ids))
		for k, v := range ids {
			presenceIDs[k] = &pb.PresenceID{Node: v.Node, SessionID: v.SessionID.String()}
		}
		w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_PartyMatchmakerAdd{
			PartyMatchmakerAdd: &pb.Party_PartyMatchmakerAddReply{
				Ticket:     ticket,
				PresenceID: presenceIDs,
			},
		}})
		return
	case *pb.Request_PartyMatchmakerRemove:
		matchmakerRemove := request.GetPartyMatchmakerRemove()
		err := s.partyRegistry.PartyMatchmakerRemove(s.ctx, uuid.FromStringOrNil(matchmakerRemove.Id), s.endpoint.Name(), matchmakerRemove.GetSessionID(), matchmakerRemove.GetFromNode(), matchmakerRemove.GetTicket())
		if err != nil {
			w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_Envelope{Envelope: newEnvelopeError(err)}})
			return
		}
		w(&pb.ResponseWriter{})
		return
	case *pb.Request_PartyDataSend:
		dataSend := request.GetPartyDataSend()
		err := s.partyRegistry.PartyDataSend(s.ctx, uuid.FromStringOrNil(dataSend.Id), s.endpoint.Name(), dataSend.GetSessionID(), dataSend.GetFromNode(), dataSend.GetOpCode(), dataSend.GetData())
		if err != nil {
			w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_Envelope{Envelope: newEnvelopeError(err)}})
			return
		}
		w(&pb.ResponseWriter{})
		return
	default:
	}
}
