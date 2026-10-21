package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func (a *App) leaveTransfer(w http.ResponseWriter, r *http.Request) error {
	if !a.transferOrigin(r, true) {
		return forbidden()
	}
	h := a.transfers
	h.mu.Lock()
	allowed := h.allowIPLocked("leave", a.clientIP(r), a.runtimeLimits().RateLimits.TransferSocketPerIP)
	h.mu.Unlock()
	if !allowed {
		return transferBusy()
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := transferDecode(w, r, &body); err != nil {
		return err
	}
	if len(body.Token) != 43 {
		return badRequest("连接信息无效")
	}
	// 仅凭请求体中的短期成员票据退出；重复通知不暴露票据是否仍然有效。
	hash := secretHash(body.Token)
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, room := range h.rooms {
		for _, member := range room.members {
			if member != nil && member.token == hash {
				h.removeLocked(room)
				w.WriteHeader(http.StatusNoContent)
				return nil
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *transferHub) reconnectExpiredLocked(room *transferRoom, now time.Time) bool {
	for _, member := range room.members {
		if member != nil && !member.disconnectedAt.IsZero() && !now.Before(member.disconnectedAt.Add(transferReconnect)) {
			return true
		}
	}
	return false
}

func (h *transferHub) peerOfflineLocked(room *transferRoom) map[string]any {
	deadline := room.expires
	for _, member := range room.members {
		if member != nil && !member.disconnectedAt.IsZero() && member.disconnectedAt.Add(transferReconnect).Before(deadline) {
			deadline = member.disconnectedAt.Add(transferReconnect)
		}
	}
	return map[string]any{"type": "peer-offline", "reconnect_until": deadline, "generation": room.generation}
}

func (h *transferHub) disconnectMemberLocked(member *transferMember, socket *websocket.Conn) {
	room := member.room
	// 被新连接替代的旧读写循环不能结束新会话，也不能延长既有离线期限。
	if member.socket != socket || !member.connected || h.rooms[room.id] != room {
		return
	}
	member.connected = false
	member.socket = nil
	if member.socketCancel != nil {
		member.socketCancel()
	}
	if !room.paired || room.ctx.Err() != nil || h.reconnectExpiredLocked(room, time.Now()) {
		h.removeLocked(room)
		return
	}
	member.disconnectedAt = time.Now()
	room.generation++
	// 仅保留有界的配对和任务结果，立刻撤销票据、管道、预留及实际中继额度。
	for _, folder := range room.folders {
		h.finishFolderLocked(room, folder, "failed")
	}
	for _, file := range room.files {
		h.finishLocked(file, "failed")
	}
	h.broadcastLocked(room, h.peerOfflineLocked(room))
	h.dispatchFolderQueuesLocked()
}

func (h *transferHub) pairedLocked(room *transferRoom) {
	servers, first := []string{}, ""
	if h.stun != "" {
		servers = strings.Split(h.stun, "\n")
		first = servers[0]
	}
	h.broadcastLocked(room, map[string]any{
		"type": "paired", "names": []string{room.members[0].name, room.members[1].name},
		"ips":        []string{room.members[0].ip, room.members[1].ip},
		"devices":    []*transferDevice{room.members[0].device, room.members[1].device},
		"locations":  []*ipLocation{room.members[0].location, room.members[1].location},
		"relay_only": room.relayOnly, "stun": first, "stun_servers": servers, "audit_enabled": h.app.audit.allows("transfer"),
		"generation": room.generation,
	})
}

func (h *transferHub) syncTransferLocked(member *transferMember) {
	files := []map[string]any{}
	for _, file := range member.room.files {
		if file.folder == nil {
			files = append(files, map[string]any{"type": "state", "id": file.id, "state": file.state, "mode": file.mode, "bytes": file.bytes})
		}
	}
	folders := []map[string]any{}
	for _, folder := range member.room.folders {
		folders = append(folders, map[string]any{"type": "folder-state", "id": folder.id, "state": folder.state, "mode": folder.mode, "bytes": folder.bytes, "completed": folder.completed, "concurrency": folder.concurrency})
	}
	h.sendLocked(member, map[string]any{"type": "sync", "files": files, "folders": folders})
}
