"""在新建的 CI 容器中验证前后端同源部署及核心访问控制。"""

import base64
import json
import os
import urllib.error
import urllib.request

BASE = os.environ.get("SMOKE_BASE_URL", "http://127.0.0.1:8081")


def request(method, path, body=None, auth=None, status=200, headers=None):
    # 管理员凭据只能发到独立前缀，API Key 和分享请求使用普通路径。
    if auth and auth.startswith("Bearer "):
        if path.startswith("/api/") and not path.startswith("/api/admin/"):
            path = "/api/admin" + path[4:]
        elif path == "/dav" or path.startswith("/dav/"):
            path = "/api/admin" + path
    data = body if isinstance(body, bytes) else None if body is None else json.dumps(body).encode("utf-8")
    values = {"Content-Type": "application/json", **(headers or {})}
    if auth:
        values["Authorization"] = auth
    req = urllib.request.Request(BASE + path, data=data, headers=values, method=method)
    try:
        response = urllib.request.urlopen(req, timeout=15)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        content = response.read()
        assert response.status == status, (method, path.split("?")[0], response.status, content)
        return content, response.headers


def api(method, path, body=None, auth=None, status=200):
    content, _ = request(method, path, body, auth, status)
    result = json.loads(content)
    assert result["success"]
    return result["data"]


def main():
    # 确认连接的是新后端，避免将测试配置写入旧 Node 服务。
    assert api("GET", "/api/version")["name"] == "cloudpaste"
    if os.environ.get("SMOKE_BACKEND_ONLY") != "1":
        content, _ = request("GET", "/")
        assert b"<html" in content
        content, _ = request("GET", "/config.js")
        assert b"window.location.origin" in content
    login = api("POST", "/api/admin/login", {"username": "admin", "password": os.environ["ADMIN_PASSWORD"]})
    auth = "Bearer " + login["token"]

    storage = api("POST", "/api/storage", {
        "name": "CI local", "storage_type": "LOCAL", "root_path": os.environ.get("SMOKE_STORAGE_ROOT", "/files"), "is_public": True,
    }, auth, 201)
    api("POST", "/api/mount/create", {
        "name": "CI mount", "storage_config_id": storage["id"], "mount_path": "/smoke",
    }, auth, 201)
    listing = api("GET", "/api/fs/list?path=/smoke", auth=auth)
    assert any(item["name"] == "demo.txt" for item in listing["items"])
    link = api("GET", "/api/fs/file-link?path=/smoke/demo.txt", auth=auth)["url"]
    content, headers = request("GET", link, status=206, headers={"Range": "bytes=0-3"})
    assert content == b"0123" and headers["Content-Range"] == "bytes 0-3/10"
    request("GET", "/api/p/smoke/demo.txt", status=403)
    request("PROPFIND", "/dav/smoke", auth=auth, status=404, headers={"Depth": "1"})
    request("PUT", "/dav/smoke/new.txt", auth=auth, status=404)
    request("POST", "/api/share/upload", auth=auth, status=400)

    # 超过旧代理 2 MiB 限制的真实流式上传，冲突不会改变原文件。
    upload_headers = {"X-FS-Filename": "uploaded.bin", "Content-Type": "application/octet-stream"}
    request("PUT", "/api/fs/upload?path=/smoke", b"A" * (3 << 20), auth, 201, upload_headers)
    request("PUT", "/api/fs/upload?path=/smoke", b"overwrite", auth, 409, upload_headers)
    uploaded_link = api("GET", "/api/fs/file-link?path=/smoke/uploaded.bin", auth=auth)["url"]
    content, _ = request("GET", uploaded_link, status=206, headers={"Range": "bytes=0-3"})
    assert content == b"AAAA"

    # 使用前端相同的按需签名、分片清单和完成协议。
    session = api("POST", "/api/fs/multipart/init", {
        "path": "/smoke", "fileName": "multipart.bin", "fileSize": (5 << 20) + 3,
        "partSize": 5 << 20, "partCount": 2,
    }, auth)
    signed = api("POST", "/api/fs/multipart/sign-parts", {
        "uploadId": session["uploadId"], "partNumbers": [1, 2],
    }, auth)["presignedUrls"]
    for index, item in enumerate(signed):
        request("PUT", item["url"], b"B" * (5 << 20) if index == 0 else b"end", headers={"Content-Type": "application/octet-stream"})
    parts = api("POST", "/api/fs/multipart/list-parts", {"uploadId": session["uploadId"]}, auth)["parts"]
    assert len(parts) == 2
    api("POST", "/api/fs/multipart/complete", {"uploadId": session["uploadId"], "parts": parts}, auth)
    multipart_link = api("GET", "/api/fs/file-link?path=/smoke/multipart.bin", auth=auth)["url"]
    content, _ = request("GET", multipart_link, status=206, headers={"Range": f"bytes={5 << 20}-"})
    assert content == b"end"

    # 文件位于表单第一项，后续中文备注与分享参数仍需完整解析。
    boundary = "cloudpaste-go-smoke-boundary"
    form = f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="form.txt"\r\nContent-Type: text/plain\r\n\r\nform upload\r\n'
    for name, value in {"storage_config_id": storage["id"], "path": "/smoke", "remark": "中文备注"}.items():
        form += f'--{boundary}\r\nContent-Disposition: form-data; name="{name}"\r\n\r\n{value}\r\n'
    form += f'--{boundary}--\r\n'
    content, _ = request("POST", "/api/share/upload", form.encode("utf-8"), auth, 201, {"Content-Type": f"multipart/form-data; boundary={boundary}"})
    form_share = json.loads(content)["data"]
    assert form_share["remark"] == "中文备注" and "storage_key" not in form_share
    assert api("GET", "/api/share/get/" + form_share["slug"])["filename"] == "form.txt"

    options = base64.b64encode(json.dumps({"storage_config_id": storage["id"], "path": "/smoke", "password": "上传密码"}, ensure_ascii=False).encode()).decode()
    content, _ = request("PUT", "/api/share/upload", b"share upload", auth, 201, {
        "X-Share-Filename": "share.txt", "X-Share-Options": options, "Content-Type": "text/plain",
    })
    uploaded_share = json.loads(content)["data"]
    request("GET", "/api/s/" + uploaded_share["slug"], status=403)
    verified_upload = api("POST", "/api/share/verify/" + uploaded_share["slug"], {"password": "上传密码"})
    assert request("GET", verified_upload["previewUrl"])[0] == b"share upload"

    share = api("POST", "/api/fs/create-share", {
        "path": "/smoke/demo.txt", "password": "CI-file-password", "max_views": 1,
    }, auth, 201)
    request("GET", "/api/s/" + share["slug"], status=403)
    verified = api("POST", "/api/share/verify/" + share["slug"], {"password": "CI-file-password"})
    content, _ = request("GET", verified["previewUrl"])
    assert content == b"0123456789"
    request("POST", "/api/share/verify/" + share["slug"], {"password": "CI-file-password"}, status=410)

    paste = api("POST", "/api/paste", {"content": "CI Markdown", "max_views": 1}, auth, 201)
    assert api("GET", "/api/paste/" + paste["slug"])["content"] == "CI Markdown"
    request("GET", "/api/paste/" + paste["slug"], status=410)
    api("POST", "/api/admin/logout", auth=auth)
    request("GET", link, status=403)
    print("上传、分片、中文表单、分享与访问控制验证通过")


if __name__ == "__main__":
    main()
