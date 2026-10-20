package server

import (
	"strings"
	"time"
	"unicode"
)

const (
	transferFolderMaxConcurrency     = 128
	transferFolderDefaultConcurrency = 16
)

// 文件夹只保存活动条目和待发额度，并发完成后立即释放子任务。
type transferFolder struct {
	id, clientID, name, state, mode                         string
	sender, count, offered, completed, concurrency, pending int
	size, offeredBytes, bytes                               int64
	created                                                 time.Time
	progress                                                time.Time
	current                                                 map[string]bool
	adjusting                                               bool
	nextConcurrency                                         int
}

type transferProgress struct {
	ID    string `json:"id"`
	Bytes int64  `json:"bytes"`
}

type transferFolderEntry struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Directory bool   `json:"directory,omitempty"`
}

// 只有已配对、已接收的文件夹内部操作使用独立额度；未知编号和普通消息不能借此绕过限频。
func (h *transferHub) isFolderMessageLocked(member *transferMember, msg transferMessage) bool {
	room := member.room
	if !room.paired || room.ctx.Err() != nil {
		return false
	}
	if msg.Type == "offer" && msg.Folder != nil {
		folder := room.folders[msg.Folder.ID]
		return folder != nil && folder.state == "active" && folder.sender == member.index && folder.pending > 0
	}
	if msg.Type == "folder-progress" {
		if len(msg.Progress) == 0 || len(msg.Progress) > transferFolderMaxConcurrency {
			return false
		}
		for _, entry := range msg.Progress {
			file := room.files[entry.ID]
			if file == nil || file.folder == nil || file.state != "active" || file.sender == member.index {
				return false
			}
		}
		return true
	}
	file := room.files[msg.ID]
	if file == nil || file.folder == nil || file.folder.state != "active" {
		return false
	}
	switch msg.Type {
	case "accept":
		return file.state == "offered" && file.sender != member.index
	case "progress", "complete":
		return file.state == "active" && file.sender != member.index
	case "cancel", "fail":
		return true
	}
	return false
}

func validTransferPath(path string, root bool) bool {
	if path == "" {
		return root
	}
	if len(path) > 1024 {
		return false
	}
	parts := strings.Split(path, "/")
	if len(parts) > 16 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || len(part) > 180 || strings.ContainsAny(part, "\\:<>\"|?*") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || strings.ContainsFunc(part, func(r rune) bool { return unicode.IsControl(r) || r == '\u202d' || r == '\u202e' }) {
			return false
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || (len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
			return false
		}
	}
	return true
}

func (h *transferHub) folderStateLocked(room *transferRoom, folder *transferFolder) {
	h.broadcastLocked(room, map[string]any{"type": "folder-state", "id": folder.id, "state": folder.state, "mode": folder.mode, "bytes": folder.bytes, "completed": folder.completed, "concurrency": folder.concurrency})
}

func (h *transferHub) finishFolderLocked(room *transferRoom, folder *transferFolder, state string) {
	if folder.state != "offered" && folder.state != "active" {
		return
	}
	folder.state = state
	if folder.mode == "relay" {
		h.folderReservations -= folder.pending
	}
	folder.pending = 0
	for id := range folder.current {
		if file := room.files[id]; file != nil {
			h.finishLocked(file, "cancelled")
		}
	}
	h.folderStateLocked(room, folder)
}

// 全站额度先为待发条目预留；活跃文件夹共享可用份额，防止一个长队列饿死其他连接。
func (h *transferHub) dispatchFolderQueuesLocked() {
	activeRelays := 0
	for _, room := range h.rooms {
		if room.ctx.Err() == nil {
			for _, folder := range room.folders {
				if folder.state == "active" && folder.mode == "relay" {
					activeRelays++
				}
			}
		}
	}
	share := h.maxRelays
	if activeRelays > 0 {
		share = max(1, h.maxRelays/activeRelays)
	}
	for _, room := range h.rooms {
		if room.ctx.Err() != nil {
			continue
		}
		for _, folder := range room.folders {
			if folder.state != "active" || folder.adjusting {
				continue
			}
			desired := folder.concurrency
			// 0 仅取消全站总并发限制，文件夹自身的并发窗口仍然生效。
			if folder.mode == "relay" && h.maxRelays > 0 {
				desired = min(desired, share)
			}
			credits := min(desired-len(folder.current)-folder.pending, folder.count-folder.offered-folder.pending)
			if folder.mode == "relay" && h.maxRelays > 0 {
				credits = min(credits, h.maxRelays-h.relays-h.folderReservations)
			}
			if credits <= 0 {
				continue
			}
			folder.pending += credits
			if folder.mode == "relay" {
				h.folderReservations += credits
			}
			h.sendLocked(room.members[folder.sender], map[string]any{"type": "folder-next", "id": folder.id, "count": credits})
		}
	}
}

func (h *transferHub) folderMessageLocked(member *transferMember, msg transferMessage) error {
	room := member.room
	if msg.Type == "folder-progress" {
		if len(msg.Progress) == 0 || len(msg.Progress) > transferFolderMaxConcurrency {
			return badRequest("文件夹进度数量无效")
		}
		seen := map[string]bool{}
		// 先整批校验再更新，不能用混合合法、非法条目修改其他人的状态。
		for _, progress := range msg.Progress {
			file := room.files[progress.ID]
			if seen[progress.ID] || file == nil || file.folder == nil || file.sender == member.index || file.mode != "p2p" || file.state != "active" || progress.Bytes < file.bytes || progress.Bytes > file.size {
				return forbidden()
			}
			seen[progress.ID] = true
		}
		for _, progress := range msg.Progress {
			file := room.files[progress.ID]
			file.bytes = progress.Bytes
			file.progress = time.Now()
		}
		h.broadcastLocked(room, map[string]any{"type": "folder-progress", "progress": msg.Progress})
		return nil
	}
	if msg.Type == "folder-offer" {
		if !validTransferPath(msg.Name, false) || strings.Contains(msg.Name, "/") || msg.ClientID == "" || len(msg.ClientID) > 64 || msg.Count < 1 || int64(msg.Count) > 1<<53-1 || msg.Size < 0 || msg.Size > room.maxFile*4-room.offeredBytes || ordinaryTransferTasks(room)+len(room.folders) >= transferFiles {
			return badRequest("文件夹名称、大小或任务额度无效")
		}
		if room.folders == nil {
			room.folders = map[string]*transferFolder{}
		}
		folder := &transferFolder{id: randomID(), clientID: msg.ClientID, name: msg.Name, count: msg.Count, size: msg.Size, sender: member.index, state: "offered", concurrency: h.folderConcurrency, created: time.Now(), current: map[string]bool{}}
		room.folders[folder.id] = folder
		room.offeredBytes += folder.size
		h.broadcastLocked(room, map[string]any{"type": "folder", "id": folder.id, "client_id": folder.clientID, "name": folder.name, "count": folder.count, "size": folder.size, "from": folder.sender, "state": "offered", "sent_at": folder.created.UTC().Format(time.RFC3339Nano), "concurrency": folder.concurrency})
		return nil
	}
	folder := room.folders[msg.ID]
	if folder == nil {
		return forbidden()
	}
	if msg.Type == "folder-concurrency" {
		if folder.sender == member.index || folder.state != "active" || folder.adjusting {
			return forbidden()
		}
		if msg.Concurrency < 1 || msg.Concurrency > transferFolderMaxConcurrency {
			return badRequest("文件夹并发数应为 1–128")
		}
		// 暂停发放额度；等发送端确认停止旧队列后，再安全归还未使用的预留。
		folder.adjusting, folder.nextConcurrency = true, msg.Concurrency
		h.sendLocked(room.members[folder.sender], map[string]any{"type": "folder-reset", "id": folder.id})
		return nil
	}
	if msg.Type == "folder-ready" {
		if folder.sender != member.index {
			return forbidden()
		}
		// 最后一个文件可能在调整期间完成，迟到的确认不改变终态。
		if folder.state != "active" {
			return nil
		}
		if !folder.adjusting {
			return forbidden()
		}
		// 同一发送端的 WebSocket 有序，确认之前在途的 offer 已全部处理。
		if folder.mode == "relay" {
			h.folderReservations -= folder.pending
		}
		folder.pending = 0
		folder.concurrency, folder.adjusting = folder.nextConcurrency, false
		h.folderStateLocked(room, folder)
		h.sendLocked(room.members[1-folder.sender], map[string]any{"type": "folder-concurrency", "id": folder.id, "concurrency": folder.concurrency})
		h.dispatchFolderQueuesLocked()
		return nil
	}
	if msg.Type == "cancel-folder" {
		h.finishFolderLocked(room, folder, "cancelled")
		h.dispatchFolderQueuesLocked()
		return nil
	}
	if msg.Type != "accept-folder" || folder.sender == member.index || folder.state != "offered" || (msg.Mode != "relay" && (msg.Mode != "p2p" || room.relayOnly)) {
		return forbidden()
	}
	concurrency := msg.Concurrency
	if concurrency == 0 {
		concurrency = h.folderConcurrency
	}
	if concurrency < 1 || concurrency > transferFolderMaxConcurrency {
		return badRequest("文件夹并发数应为 1–128")
	}
	for _, other := range room.folders {
		if other.state == "active" {
			return badRequest("请先完成当前文件夹传输")
		}
	}
	for _, file := range room.files {
		if file.state == "active" {
			return badRequest("请先完成当前文件传输")
		}
	}
	folder.state, folder.mode, folder.concurrency = "active", msg.Mode, concurrency
	folder.progress = time.Now()
	h.folderStateLocked(room, folder)
	h.dispatchFolderQueuesLocked()
	return nil
}

func ordinaryTransferTasks(room *transferRoom) int {
	count := 0
	for _, file := range room.files {
		if file.folder == nil {
			count++
		}
	}
	return count
}

func (h *transferHub) finishFolderEntryLocked(file *transferFile, state string) {
	folder := file.folder
	if folder == nil {
		return
	}
	delete(file.room.files, file.id)
	delete(folder.current, file.id)
	if folder.state != "active" {
		return
	}
	if state != "complete" {
		h.finishFolderLocked(file.room, folder, state)
		return
	}
	folder.completed++
	folder.progress = time.Now()
	folder.bytes += file.size
	if folder.completed == folder.count {
		if folder.bytes != folder.size {
			h.finishFolderLocked(file.room, folder, "failed")
			return
		}
		h.finishFolderLocked(file.room, folder, "complete")
		return
	}
	h.folderStateLocked(file.room, folder)
}
