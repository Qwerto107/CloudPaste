package server

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func mergedRequests(t *testing.T, event auditEvent) uint64 {
	t.Helper()
	var details struct {
		Count uint64 `json:"merged_requests"`
	}
	if err := json.Unmarshal([]byte(event.Details), &details); err != nil {
		t.Fatal(err)
	}
	return details.Count
}

// 路径扫描器不断更换无效后缀时，只保留首条和计数汇总。
func TestAuditCoalescesScannedPaths(t *testing.T) {
	coalescer := newAuditCoalescer()
	now := time.Now()
	for i := 0; i < 1000; i++ {
		event := auditEvent{
			Time: now.UnixMilli() + int64(i), Module: "text", Action: "查看文本",
			ActorType: "guest", IP: "198.51.100.1", Target: fmt.Sprintf("missing-%d", i),
			Method: "GET", Endpoint: "/api/paste/{slug}", Status: 404, Result: "failure",
		}
		output := coalescer.add(event, now.Add(10*time.Second))
		if i == 0 {
			if len(output) != 1 || output[0].Target != "missing-0" {
				t.Fatalf("首条扫描请求未保留: %+v", output)
			}
		} else if len(output) != 0 {
			t.Fatalf("第 %d 个扫描请求被单独写入: %+v", i, output)
		}
	}
	summaries := coalescer.expire(now.Add(2*time.Minute), false)
	if len(summaries) != 1 || mergedRequests(t, summaries[0]) != 999 || summaries[0].Target != "" {
		t.Fatalf("扫描汇总不正确: %+v", summaries)
	}
}

func TestAuditHTTPPathScanDoesNotFillDatabase(t *testing.T) {
	f := newFixture(t)
	f.app.audit.enabled.Store(true)
	for i := 0; i < 100; i++ {
		f.request(t, "GET", fmt.Sprintf("/api/paste/scan-missing-%d", i), "", nil, nil, 404)
	}
	waitAudit(t, f, "module='text' AND action='查看文本'", 1)
	time.Sleep(300 * time.Millisecond)
	var count int
	if err := f.app.audit.db.QueryRow("SELECT count(*) FROM operation_logs WHERE module='text' AND action='查看文本'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("扫描路径逐条写入日志: count=%d, error=%v", count, err)
	}
}

func TestAuditCoalescerBoundsManySources(t *testing.T) {
	coalescer := newAuditCoalescer()
	now := time.Now()
	kept := 0
	for i := 0; i < auditCoalesceKeys+50; i++ {
		event := auditEvent{
			Time: now.UnixMilli(), Module: "text", Action: "查看文本",
			IP: fmt.Sprintf("198.51.%d.%d", i/256, i%256), Method: "GET",
			Endpoint: "/api/paste/{slug}", Status: 404, Result: "failure",
		}
		kept += len(coalescer.add(event, now))
	}
	if kept != auditCoalesceKeys || len(coalescer.bursts) != auditCoalesceKeys {
		t.Fatalf("合并键未保持有界: kept=%d, keys=%d", kept, len(coalescer.bursts))
	}
	summaries := coalescer.expire(now.Add(2*time.Minute), false)
	if len(summaries) != 1 || summaries[0].Action != "高频请求已合并" || mergedRequests(t, summaries[0]) != 50 {
		t.Fatalf("多来源汇总不正确: %+v", summaries)
	}
}

func TestAuditCoalescerPreservesWrites(t *testing.T) {
	coalescer := newAuditCoalescer()
	now := time.Now()
	event := auditEvent{Module: "text", Action: "创建文本", Method: "POST", Result: "success", IP: "198.51.100.1"}
	if len(coalescer.add(event, now)) != 1 || len(coalescer.add(event, now)) != 1 {
		t.Fatal("成功的写操作不应合并")
	}
	read := auditEvent{Module: "text", Action: "查看文本", Method: "GET", Result: "success", IP: "198.51.100.1", Target: "one"}
	if len(coalescer.add(read, now)) != 1 || len(coalescer.add(read, now)) != 0 {
		t.Fatal("重复读取同一分享未合并")
	}
	read.Target = "two"
	if len(coalescer.add(read, now)) != 1 {
		t.Fatal("不同成功分享应分别保留首条记录")
	}
}
