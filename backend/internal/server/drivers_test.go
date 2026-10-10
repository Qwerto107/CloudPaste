package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestS3ListingSignedRequestsAndRange(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Error("S3 request not signed")
			w.WriteHeader(403)
			return
		}
		if r.URL.Query().Get("list-type") == "2" {
			w.Header().Set("Content-Type", "application/xml")
			io.WriteString(w, `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>test-bucket</Name><Prefix></Prefix><KeyCount>1</KeyCount><MaxKeys>50</MaxKeys><IsTruncated>false</IsTruncated><Contents><Key>file.txt</Key><LastModified>2023-11-14T22:13:20.000Z</LastModified><ETag>"etag"</ETag><Size>10</Size></Contents></ListBucketResult>`)
			return
		}
		if r.URL.Path != "/test-bucket/file.txt" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("ETag", `"etag"`)
		w.Header().Set("Content-Type", "text/plain")
		if r.Method == "HEAD" {
			w.Header().Set("Content-Length", "10")
			return
		}
		if r.Header.Get("Range") == "bytes=2-4" {
			w.Header().Set("Content-Range", "bytes 2-4/10")
			w.Header().Set("Content-Length", "3")
			w.WriteHeader(206)
			io.WriteString(w, "234")
			return
		}
		w.Header().Set("Content-Length", "10")
		io.WriteString(w, "0123456789")
	}))
	defer upstream.Close()
	d, err := newS3Driver(Storage{Type: "S3", Endpoint: upstream.URL, Bucket: "test-bucket", AccessKey: "access", SecretKey: "secret", Region: "us-east-1", PathStyle: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	listing, err := d.List(context.Background(), "", "", 50)
	if err != nil || len(listing.Items) != 1 || listing.Items[0].Name != "file.txt" {
		t.Fatal("S3 listing", listing, err)
	}
	info, err := d.Stat(context.Background(), "file.txt")
	if err != nil || info.Size == nil || *info.Size != 10 {
		t.Fatal("S3 stat", info, err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Range", "bytes=2-4")
	stream, err := d.Open(context.Background(), "file.txt", r)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	content, err := io.ReadAll(stream.Body)
	if err != nil || stream.Status != 206 || string(content) != "234" {
		t.Fatal("S3 range", stream.Status, string(content), err)
	}
}

func TestLocalPaginationHasNoMissingFiles(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 225; i++ {
		writeFile(t, root, fmt.Sprintf("file-%03d.txt", i), "test")
	}
	d, err := newLocalDriver(Storage{RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	names := map[string]bool{}
	cursor := ""
	for pages := 0; pages < 20; pages++ {
		listing, err := d.List(context.Background(), "", cursor, 23)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range listing.Items {
			if names[item.Name] {
				t.Fatal("duplicate file", item.Name)
			}
			names[item.Name] = true
		}
		if !listing.HasMore {
			break
		}
		cursor = listing.NextCursor
	}
	if len(names) != 225 {
		t.Fatalf("pagination returned %d files, want 225", len(names))
	}
}

func TestLocalNamedPipeIsRejectedWithoutOpening(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("此测试在 Linux CI 中验证 POSIX 命名管道")
	}
	root := t.TempDir()
	filename := filepath.Join(root, "pipe")
	if output, err := exec.Command("mkfifo", filename).CombinedOutput(); err != nil {
		t.Fatalf("create test FIFO: %v %s", err, output)
	}
	d, err := newLocalDriver(Storage{RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err = d.Stat(context.Background(), "pipe"); err == nil {
		t.Fatal("named pipe reported as regular file")
	}
	if _, err = d.Open(context.Background(), "pipe", httptest.NewRequest("GET", "/", nil)); err == nil {
		t.Fatal("named pipe opened")
	}
}
