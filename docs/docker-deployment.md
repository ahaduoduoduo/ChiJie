# Docker 部署

日期：2026-05-06

## 端口模型

Docker 部署保持控制面和代理面分离：

- Proxy API：容器 `8080`，默认映射为宿主机 `127.0.0.1:18080`。
- Admin：容器 `9090`，默认映射为宿主机 `127.0.0.1:19090`。

因此公网入口建议交给 nginx / Caddy，容器端口只监听宿主机本机地址。Admin 通过 SSH tunnel 访问：

```bash
ssh -L 9090:127.0.0.1:19090 root@your-vps
```

浏览器打开：

```text
http://127.0.0.1:9090/
```

## 首次部署

在 VPS 上安装 Docker 和 Docker Compose plugin 后，进入项目目录。

生产环境推荐拉取 Docker Hub 预构建镜像：

```bash
cp .env.example .env
cp configs/gateway.docker.yaml.example configs/gateway.yaml
cp configs/nodes.yaml.example configs/nodes.yaml
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d
```

本地开发或服务器本机构建使用：

```bash
cp .env.example .env
cp configs/gateway.docker.yaml.example configs/gateway.yaml
cp configs/nodes.yaml.example configs/nodes.yaml
docker compose up -d --build
```

生成 JWT 密钥：

```bash
openssl rand -hex 32
```

编辑 `configs/gateway.yaml`：

```yaml
server:
  listen: ":8080"
  allow_private_targets: false

admin:
  listen: ":9090"
  password: "replace-with-strong-password"
  jwt_secret: "replace-with-openssl-rand-hex-32"
  jwt_expire: "24h"
```

生产环境查看日志：

```bash
docker compose -f docker-compose.prod.yml logs -f chijie
```

本地构建模式查看日志：

```bash
docker compose logs -f chijie
```

健康检查：

```bash
curl http://127.0.0.1:18080/health
```

期望响应：

```json
{"status":"ok"}
```

## 更新

Docker Hub 镜像部署模式：

```bash
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d
```

本机构建模式在拉取或同步新代码后执行：

```bash
docker compose up -d --build
```

配置文件挂载在宿主机 `./configs`，容器重建不会覆盖。

## 端口与配置目录

宿主机端口和配置目录通过 `.env` 控制：

```env
CHIJIE_IMAGE=ahaduoduoduo/chijie:latest
CHIJIE_PROXY_HOST=127.0.0.1
CHIJIE_PROXY_PORT=18080
CHIJIE_ADMIN_HOST=127.0.0.1
CHIJIE_ADMIN_PORT=19090
CHIJIE_CONFIG_DIR=./configs
TZ=Asia/Shanghai
```

Compose 映射规则：

```text
${CHIJIE_PROXY_HOST}:${CHIJIE_PROXY_PORT} -> 容器 8080
${CHIJIE_ADMIN_HOST}:${CHIJIE_ADMIN_PORT} -> 容器 9090
${CHIJIE_CONFIG_DIR} -> 容器 /config
```

容器内端口保持固定，升级或迁移时只改 `.env`。

## 架构

Docker Hub 自动发布详见 [dockerhub-release.md](dockerhub-release.md)。

本机 ARM 开发测试时直接使用默认 Compose：

```bash
docker compose up -d --build
```

需要在 ARM 机器上构建 Linux X86 镜像时，叠加 amd64 覆盖文件：

```bash
docker compose -f docker-compose.yml -f docker-compose.amd64.yml build
```

VPS 是 X86 时，在 VPS 上直接执行默认构建即可生成 `linux/amd64` 镜像。

## Cloudflare

Cloudflare 只需要指向 VPS 的 nginx / Caddy 入口。nginx 反向代理到：

```text
http://127.0.0.1:18080
```

生产环境用 nginx / Caddy 监听 `443`，外部 URL 不需要带端口。

## 文件说明

- `Dockerfile`：多阶段构建，先构建前端，再构建 Go 二进制。
- `docker-compose.yml`：默认部署配置，Proxy API 暴露到宿主机，Admin 只绑定宿主机本机地址。
- `docker-compose.prod.yml`：生产部署配置，只拉取 Docker Hub 镜像，不在 VPS 上构建。
- `docker-compose.amd64.yml`：ARM 机器上构建 X86 镜像时使用的覆盖文件。
- `.github/workflows/dockerhub.yml`：GitHub Actions 自动构建并推送 Docker Hub 镜像。
- `.env.example`：宿主机端口、配置目录和时区模板。
- `.dockerignore`：排除本地二进制、构建产物和 `configs/*.yaml`，避免把本地密钥打进镜像。
- `configs/gateway.docker.yaml.example`：Docker 场景的主配置模板。
