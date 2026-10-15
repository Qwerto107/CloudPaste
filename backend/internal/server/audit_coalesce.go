package server

import (
	"strconv"
	"time"
)

const (
	auditCoalesceWindow = time.Minute
	auditCoalesceKeys   = 256
)

type auditCoalesceKey struct {
	ip, module, action, endpoint, actorType, actorID, result, target string
	status                                                           int
}

type auditBurst struct {
	event  auditEvent
	until  time.Time
	last   int64
	merged uint64
}

// 合并器只在日志写入 goroutine 中使用，不增加业务请求上的锁或数据库操作。
type auditCoalescer struct {
	bursts   map[auditCoalesceKey]*auditBurst
	overflow map[string]*auditBurst
}

func newAuditCoalescer() *auditCoalescer {
	return &auditCoalescer{
		bursts:   make(map[auditCoalesceKey]*auditBurst),
		overflow: make(map[string]*auditBurst),
	}
}

func auditCoalesceKeyFor(event auditEvent) (auditCoalesceKey, bool) {
	// 失败请求可能扫描不同后缀，合并键故意不包含目标路径。
	if event.Result != "failure" {
		if event.Method != "GET" || (event.Module != "text" && event.Module != "file") {
			return auditCoalesceKey{}, false
		}
	}
	key := auditCoalesceKey{
		ip: event.IP, module: event.Module, action: event.Action,
		endpoint: event.Endpoint, actorType: event.ActorType, actorID: event.ActorID,
		result: event.Result, status: event.Status,
	}
	if event.Result != "failure" {
		key.target = event.Target
	}
	return key, true
}

func auditMergedEvent(burst *auditBurst) auditEvent {
	event := burst.event
	event.Time = burst.last
	event.Target = ""
	event.DurationMS = 0
	event.Details = `{"merged_requests":` + strconv.FormatUint(burst.merged, 10) + `}`
	return event
}

func (c *auditCoalescer) add(event auditEvent, now time.Time) []auditEvent {
	key, eligible := auditCoalesceKeyFor(event)
	if !eligible {
		return []auditEvent{event}
	}
	var output []auditEvent
	if burst := c.bursts[key]; burst != nil {
		if now.Before(burst.until) {
			burst.merged++
			burst.last = event.Time
			return nil
		}
		if burst.merged > 0 {
			output = append(output, auditMergedEvent(burst))
		}
		delete(c.bursts, key)
	}
	if len(c.bursts) >= auditCoalesceKeys {
		// 来源过多时按模块和结果汇总，内存和落库量保持有界。
		overflowKey := event.Module + "\x00" + event.Result
		burst := c.overflow[overflowKey]
		if burst == nil || !now.Before(burst.until) {
			if burst != nil {
				output = append(output, auditMergedEvent(burst))
			}
			summary := auditEvent{
				Time: event.Time, Module: event.Module, Action: "高频请求已合并",
				ActorType: "unknown", ActorName: "多个来源", IP: "多个 IP",
				Result: event.Result,
			}
			burst = &auditBurst{event: summary, until: now.Add(auditCoalesceWindow)}
			c.overflow[overflowKey] = burst
		}
		burst.merged++
		burst.last = event.Time
		return output
	}
	c.bursts[key] = &auditBurst{event: event, until: now.Add(auditCoalesceWindow), last: event.Time}
	return append(output, event)
}

func (c *auditCoalescer) expire(now time.Time, all bool) []auditEvent {
	var output []auditEvent
	for key, burst := range c.bursts {
		if !all && now.Before(burst.until) {
			continue
		}
		if burst.merged > 0 {
			output = append(output, auditMergedEvent(burst))
		}
		delete(c.bursts, key)
	}
	for key, burst := range c.overflow {
		if !all && now.Before(burst.until) {
			continue
		}
		output = append(output, auditMergedEvent(burst))
		delete(c.overflow, key)
	}
	return output
}
