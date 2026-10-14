"""在新建的 CI 容器中验证前后端同源部署及核心访问控制。"""

import base64
import json
import os
import re
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
    # 健康检查与当前前端一致的上传流程，测试只运行在独立 CI 容器。
    assert api("GET", "/api/health")["status"] == "ok"
    if os.environ.get("SMOKE_BACKEND_ONLY") != "1":
        content, _ = request("GET", "/")
        assert b"<html" in content
        content, _ = request("GET", "/config.js")
        assert b"window.location.origin" in content or re.search(rb'backendUrl\s*:\s*([\"\'])\1', content)
        content, _ = request("GET", "/admin/login")
        assert b"<html" in content
    login = api("POST", "/api/admin/login", {"username": "admin", "password": os.environ["ADMIN_PASSWORD"]})
    auth = "Bearer " + login["token"]
    storage = api("POST", "/api/storage", {
        "name": "CI local", "storage_type": "LOCAL", "root_path": os.environ.get("SMOKE_STORAGE_ROOT", "/files"), "is_public": True,
    }, auth, 201)

    def upload(name, body, fields):
        options = base64.b64encode(json.dumps({"storage_config_id": storage["id"], **fields}, ensure_ascii=False).encode()).decode()
        content, _ = request("PUT", "/api/share/upload", body, auth, 201, {
            "X-Share-Filename": name, "X-Share-Options": options, "Content-Type": "application/octet-stream",
        })
        return json.loads(content)["data"]

    # 超过旧代理 2 MiB 限制的真实上传，重名冲突不会覆盖原文件。
    uploaded = upload("uploaded.bin", b"A" * (3 << 20), {"original_filename": True, "remark": "中文备注"})
    assert uploaded["remark"] == "中文备注" and "storage_key" not in uploaded
    options = base64.b64encode(json.dumps({"storage_config_id": storage["id"], "original_filename": True}).encode()).decode()
    request("PUT", "/api/share/upload", b"overwrite", auth, 409, {"X-Share-Filename": "uploaded.bin", "X-Share-Options": options})
    view = api("GET", "/api/share/get/" + uploaded["slug"])
    content, headers = request("GET", view["contentUrl"], status=206, headers={"Range": "bytes=0-3"})
    assert content == b"AAAA" and headers["Content-Range"] == f"bytes 0-3/{3 << 20}"

    protected = upload("protected.txt", b"share upload", {"password": "上传密码", "max_views": 1})
    request("GET", "/api/s/" + protected["slug"], status=403)
    verified = api("POST", "/api/share/verify/" + protected["slug"], {"password": "上传密码"})
    assert request("GET", verified["contentUrl"])[0] == b"share upload"
    request("GET", "/api/share/get/" + protected["slug"], status=404)

    paste = api("POST", "/api/paste", {"content": "CI Markdown", "max_views": 1}, auth, 201)
    assert api("GET", "/api/paste/" + paste["slug"])["content"] == "CI Markdown"
    request("GET", "/api/paste/" + paste["slug"], status=404)
    request("POST", "/api/share/upload", auth=auth, status=404)
    request("POST", "/api/fs/multipart/init", auth=auth, status=404)
    request("GET", "/api/fs/list", auth=auth, status=404)
    api("POST", "/api/admin/logout", auth=auth)
    request("GET", "/api/storage", auth=auth, status=401)
    print("原始上传、中文元信息、分享、Range 与访问控制验证通过")


if __name__ == "__main__":
    main()
