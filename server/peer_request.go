// Copyright 2024 The Bombus Authors
//
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package server

import (
	"strings"
	"time"

	"github.com/whymidnight/nakama-common/rtapi"
	"github.com/whymidnight/nakama-kit/pb"
	"github.com/gofrs/uuid/v5"
	"go.uber.org/zap"
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
	s.logger.Debug("onRequest", zap.Any("request", request), zap.String("node", frame.Node))
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
	case *pb.Request_MatchId:
		id := request.GetMatchId()
		idComponents := strings.SplitN(id, ".", 2)
		if len(idComponents) != 2 || idComponents[1] != s.endpoint.Name() {
			w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_Envelope{Envelope: newEnvelopeError(status.Error(codes.NotFound, "Not Found"))}})
			return
		}
		match, _, err := s.matchRegistry.GetMatch(s.ctx, request.GetMatchId())
		if err != nil {
			w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_Envelope{Envelope: newEnvelopeError(err)}})
			return
		}

		w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_Match{Match: match}})
		return
	case *pb.Request_MatchJoinAttempt:
		joinAttempt := request.GetMatchJoinAttempt()
		found, allow, isNew, reason, l, ps := s.matchRegistry.JoinAttempt(s.ctx, uuid.FromStringOrNil(joinAttempt.Id), s.endpoint.Name(), uuid.FromStringOrNil(joinAttempt.UserId), uuid.FromStringOrNil(joinAttempt.SessionId), joinAttempt.Username, joinAttempt.SessionExpiry, joinAttempt.Vars, joinAttempt.ClientIP, joinAttempt.ClientPort, frame.Node, joinAttempt.Metadata)
		matchPresences := make([]*pb.MatchPresence, len(ps))
		for k, v := range ps {
			matchPresences[k] = &pb.MatchPresence{
				UserId:    v.GetUserId(),
				SessionId: v.GetSessionId(),
				Username:  v.GetUsername(),
				Node:      v.GetNodeId(),
				Reason:    uint32(v.GetReason()),
			}
		}
		w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_MathJoinAttempt{
			MathJoinAttempt: &pb.Match_JoinAttemptReply{
				Found:     found,
				Allow:     allow,
				IsNew:     isNew,
				Reason:    reason,
				Label:     l,
				Presences: matchPresences,
			},
		}})
		return

	case *pb.Request_MatchSendData:
		sendData := request.GetMatchSendData()
		s.matchRegistry.SendData(uuid.FromStringOrNil(sendData.Id), s.endpoint.Name(), uuid.FromStringOrNil(sendData.UserId), uuid.FromStringOrNil(sendData.SessionId), sendData.Username, sendData.FromNode, sendData.OpCode, sendData.Data, sendData.Reliable, sendData.ReceiveTime)
		return
	case *pb.Request_MatchSignal:
		sig := request.GetMatchSignal()
		idComponents := strings.SplitN(sig.Id, ".", 2)
		if len(idComponents) != 2 || idComponents[1] != s.endpoint.Name() {
			w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_Envelope{Envelope: newEnvelopeError(status.Error(codes.NotFound, "Not Found"))}})
			return
		}
		v, err := s.matchRegistry.Signal(s.ctx, sig.Id, sig.Data)
		if err != nil {
			w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_Envelope{Envelope: newEnvelopeError(err)}})
			return
		}

		w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_MatchSignal{MatchSignal: v}})
		return

	case *pb.Request_MatchState:
		userPresence, tick, state, err := s.matchRegistry.GetState(s.ctx, uuid.FromStringOrNil(request.GetMatchState()), s.endpoint.Name())
		if err != nil {
			w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_Envelope{Envelope: newEnvelopeError(err)}})
			return
		}

		presences := make([]*rtapi.UserPresence, len(userPresence))
		for k, v := range userPresence {
			presences[k] = &rtapi.UserPresence{
				UserId:      v.GetUserId(),
				SessionId:   v.GetSessionId(),
				Username:    v.GetUsername(),
				Persistence: v.GetPersistence(),
				Status:      v.GetStatus(),
			}
		}

		w(&pb.ResponseWriter{Payload: &pb.ResponseWriter_MatchState{
			MatchState: &pb.Match_State{
				UserPresence: presences,
				Tick:         tick,
				State:        state,
			},
		}})
		return
	default:
	}
}
