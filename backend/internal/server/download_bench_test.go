package server

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type discardResponse struct {
	header http.Header
	size   int64
}

func (w *discardResponse) Header() http.Header    { return w.header }
func (w *discardResponse) WriteHeader(status int) {}
func (w *discardResponse) Write(data []byte) (int, error) {
	w.size += int64(len(data))
	return len(data), nil
}

// 对比不同文件大小的分配量，验证下载内存不会随着文件体积线性增长。
func BenchmarkLocalDownload(b *testing.B) {
	for _, size := range []int64{1 << 20, 64 << 20} {
		b.Run(fmt.Sprintf("%dMiB", size>>20), func(b *testing.B) {
			root := b.TempDir()
			filename := filepath.Join(root, "file.bin")
			f, err := os.Create(filename)
			if err != nil {
				b.Fatal(err)
			}
			if err = f.Truncate(size); err != nil {
				b.Fatal(err)
			}
			_ = f.Close()
			d, err := newLocalDriver(Storage{RootPath: root})
			if err != nil {
				b.Fatal(err)
			}
			defer d.Close()
			app := &App{downloads: make(chan struct{}, 8), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			info := FileInfo{Name: "file.bin", Size: &size, MimeType: "application/octet-stream"}
			b.ReportAllocs()
			b.SetBytes(size)
			b.ResetTimer()
			for b.Loop() {
				r := httptest.NewRequest("GET", "/file", nil)
				w := &discardResponse{header: make(http.Header)}
				if err = app.deliver(w, r, d, "file.bin", info, false); err != nil {
					b.Fatal(err)
				}
				if w.size != size {
					b.Fatalf("downloaded %d bytes, want %d", w.size, size)
				}
			}
		})
	}
}
