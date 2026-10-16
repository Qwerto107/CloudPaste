package server

import (
	"bytes"
	"encoding/binary"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestIPLocationManualLoadAndUnload(t *testing.T) {
	f, _, creator, guest := transferPair(t)
	data, err := os.ReadFile(writeLocationFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if status := dataMap(t, f.request(t, "GET", "/api/admin/ip-location", f.admin, nil, nil, 200)); status["loaded"] != false {
		t.Fatal("首次应为未加载", status)
	}
	put := func(auth string, raw []byte, contentLength int64, want int) {
		t.Helper()
		r := httptest.NewRequest("PUT", "/api/admin/ip-location", bytes.NewReader(raw))
		r.Header.Set("Authorization", auth)
		r.Header.Set("Content-Type", "application/octet-stream")
		r.ContentLength = contentLength
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("加载返回 %d，期望 %d: %s", w.Code, want, w.Body.String())
		}
	}
	put("", data, int64(len(data)), 401)
	key, _ := f.key(t, "不能管理归属地", "", TextShare|FileShare)
	put(key, data, int64(len(data)), 403)
	put(f.admin, []byte("PK zip"), 6, 400)
	put(f.admin, []byte("x"), ipLocationMaxBytes+1, 413)
	put(f.admin, data, int64(len(data)), 200)
	for _, peer := range []transferTestPeer{creator, guest} {
		locations := peer.next("locations")["locations"].([]any)
		if len(locations) != 2 || locations[0].(map[string]any)["summary"] != "本机" || locations[1].(map[string]any)["summary"] != "本机" {
			t.Fatal("加载后未刷新已有连接", locations)
		}
	}
	if got := f.app.locateIP("8.8.8.8"); got == nil || got.Summary != "中国 · 广东" {
		t.Fatal("加载失败", got)
	}
	if got := f.app.locateIP("2408::1"); got != nil {
		t.Fatal("IPv6 不应显示位置", got)
	}
	// 已加载的选择在重启后保留；替换失败不能关闭当前有效数据库。
	restored, err := New(f.app.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.ipLocationStatus()["loaded"].(bool) {
		t.Fatal("未恢复手动加载的数据库")
	}
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
	put(f.admin, []byte("invalid replacement"), 19, 400)
	if !f.app.ipLocationStatus()["loaded"].(bool) {
		t.Fatal("无效替换影响已有数据库")
	}
	f.request(t, "DELETE", "/api/admin/ip-location", f.admin, nil, nil, 200)
	for _, peer := range []transferTestPeer{creator, guest} {
		locations := peer.next("locations")["locations"].([]any)
		if len(locations) != 2 || locations[0] != nil || locations[1] != nil {
			t.Fatal("卸载后未隐藏已有连接位置", locations)
		}
	}
	if f.app.locateIP("8.8.8.8") != nil || f.app.locateIP("127.0.0.1") != nil {
		t.Fatal("卸载后不应提供位置")
	}
	entries, err := os.ReadDir(f.app.cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if ipLocationFilename(entry.Name()) {
			t.Fatal("未回收数据库或上传临时文件", entry.Name())
		}
	}
	// 仅放置旧约定文件不等于选择加载，不扫描或自动启用用户文件。
	if err := os.WriteFile(filepath.Join(f.app.cfg.DataDir, "ip2location.bin"), data, 0600); err != nil {
		t.Fatal(err)
	}
	restored, err = New(f.app.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if restored.ipLocationStatus()["loaded"].(bool) {
		t.Fatal("不应自动加载未选择的数据库")
	}
}

// 构造只用于测试的 IPv4 DB3 文件，不依赖真实 IP 数据或在线服务。
func writeLocationFixture(t *testing.T) string {
	t.Helper()
	data := make([]byte, 96)
	data[0], data[1], data[2], data[3], data[4], data[29] = 3, 4, 26, 10, 1, 1
	put := func(offset int, value uint32) { binary.LittleEndian.PutUint32(data[offset:], value) }
	put(5, 1)
	put(9, 65)
	appendText := func(value string) uint32 {
		position := uint32(len(data))
		data = append(data, byte(len(value)))
		data = append(data, value...)
		return position
	}
	country := appendText("CN")
	appendText("China")
	region, city := appendText("Guangdong"), appendText("Guangzhou")
	for _, offset := range []int{64} {
		first := 4
		put(offset+first, country)
		put(offset+first+4, region)
		put(offset+first+8, city)
		for i := 0; i < first; i++ {
			data[offset+first+12+i] = 255
		}
	}
	put(31, uint32(len(data)))
	path := filepath.Join(t.TempDir(), "ip2location.bin")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIPLocationDB3(t *testing.T) {
	path := writeLocationFixture(t)
	db, err := openIPLocation(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.db.Close()
	a := &App{ipLocationDB: db}
	for _, ip := range []string{"8.8.8.8", "::ffff:8.8.8.8"} {
		got := a.locateIP(ip)
		if got.Summary != "中国 · 广东" || got.Region != "广东" || got.CountryCode != "CN" || got.Source != "ip2location" {
			t.Fatalf("%s IPv4 省级信息不正确: %+v", ip, got)
		}
	}
	if got := a.locateIP("2408::1"); got != nil {
		t.Fatal("IPv6 不应提供位置", got)
	}
	// 模拟运行期间文件被截断；归属地失败不得影响原有连接。
	if err := os.Truncate(path, 64); err != nil {
		t.Fatal(err)
	}
	if got := a.locateIP("8.8.8.8"); got.Summary != "未知" || got.Source != "" {
		t.Fatal("损坏数据库未安全降级", got)
	}
}

func TestIPLocationFallbackAndValidation(t *testing.T) {
	a := &App{}
	for _, ip := range []string{"127.0.0.1", "192.168.1.2", "8.8.8.8", "::1"} {
		if got := a.locateIP(ip); got != nil {
			t.Fatal("未加载数据库时不应提供位置", got)
		}
	}
	database, err := openIPLocation(writeLocationFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer database.db.Close()
	a.ipLocationDB = database
	for ip, want := range map[string]string{
		"127.0.0.1": "本机", "::ffff:127.0.0.1": "本机",
		"192.168.1.2": "局域网", "172.28.0.1": "局域网", "10.0.0.1": "局域网",
		"100.64.0.1": "局域网", "224.0.0.1": "未知", "0.0.0.0": "未知",
	} {
		if got := a.locateIP(ip); got.Summary != want || got.Source != "" {
			t.Fatalf("%s: %+v, want %s", ip, got, want)
		}
	}
	for _, ip := range []string{"::1", "fd00::1", "fe80::1", "2408::1", "host.local"} {
		if got := a.locateIP(ip); got != nil {
			t.Fatal("IPv6 或无效来源不应显示位置", ip, got)
		}
	}
	dir := t.TempDir()
	if _, err := openIPLocation(filepath.Join(dir, "missing.bin")); err == nil {
		t.Fatal("显式配置路径错误必须报告")
	}
	path := filepath.Join(dir, "invalid.bin")
	for _, data := range [][]byte{[]byte("PK invalid ZIP"), make([]byte, 64)} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := openIPLocation(path); err == nil {
			t.Fatal("无效格式应拒绝加载")
		}
	}
	for _, text := range []string{"-", "\u202e伪造省份", "广东\n北京"} {
		if locationText(text) != "" {
			t.Fatal("不应返回无效数据库字段", text)
		}
	}
}
