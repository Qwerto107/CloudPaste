// 采用同源反向代理，浏览器不需要知道 Docker 内部服务地址。
window.appConfig = { backendUrl: window.location.origin };
