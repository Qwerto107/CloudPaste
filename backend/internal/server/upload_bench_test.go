package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type zeroReader struct{}

func (zeroReader) Read(data []byte) (int, error) { clear(data); return len(data), nil }

// 测量完整 HTTP 上传的分配量，包括目标目录内的临时接收、授权复核和原子发布。
func BenchmarkLocalUpload(b *testing.B) {
	for _, size := range []int64{1 << 20, 64 << 20} {
		b.Run(fmt.Sprintf("%dMiB", size>>20), func(b *testing.B) {
			app, err := New(Config{DataDir: b.TempDir(), AdminUsername: "admin", AdminPassword: testPassword}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				b.Fatal(err)
			}
			defer app.Close()
			root := b.TempDir()
			s := Storage{ID: randomID(), Name: "local", Type: "LOCAL", RootPath: root, Status: "ENABLED", UpdatedAt: nowText()}
			m := Mount{ID: randomID(), Name: "disk", Path: "/disk", RootPath: "/", StorageID: s.ID, Active: 1, UpdatedAt: nowText()}
			ctx := context.Background()
			if err = app.saveResource(ctx, "storage_configs", s.ID, "", "", s); err != nil {
				b.Fatal(err)
			}
			if err = app.saveResource(ctx, "mounts", m.ID, "", "", m); err != nil {
				b.Fatal(err)
			}
			var admin string
			if err = app.db.QueryRow("SELECT id FROM admins LIMIT 1").Scan(&admin); err != nil {
				b.Fatal(err)
			}
			token := randomID()
			if _, err = app.db.Exec("INSERT INTO sessions(token_hash,admin_id,expires_at,created_at) VALUES(?,?,?,?)", secretHash(token), admin, time.Now().Add(time.Hour).Unix(), time.Now().Unix()); err != nil {
				b.Fatal(err)
			}
			handler := app.Handler()
			b.ReportAllocs()
			b.SetBytes(size)
			b.ResetTimer()
			for b.Loop() {
				r := httptest.NewRequest("PUT", "/api/admin/share/upload", io.LimitReader(zeroReader{}, size))
				r.ContentLength = size
				r.Header.Set("Authorization", "Bearer "+token)
				for name, value := range uploadHeaders("file.bin", map[string]any{"storage_config_id": s.ID, "path": "/disk", "original_filename": true}, true) {
					r.Header.Set(name, value)
				}
				r.Header.Set("Content-Type", "application/octet-stream")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				if w.Code != 201 {
					b.Fatal(w.Code, w.Body.String())
				}
				if err = os.Remove(filepath.Join(root, "file.bin")); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
